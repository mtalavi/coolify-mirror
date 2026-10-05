package engine

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/docker"
	"github.com/mtalavi/coolify-mirror/internal/run"
	"github.com/mtalavi/coolify-mirror/internal/transfer"
)

// Share modes.
const (
	ShareDirect = "direct" // this process listens on a TCP port
	ShareProxy  = "proxy"  // a helper container behind Coolify's Traefik on port 443 (TLS passthrough)
)

// DefaultPort is used for direct sharing.
const DefaultPort = 8123

// KeyEnv can carry the backup key instead of --key.
const KeyEnv = "COOLIFY_MIRROR_KEY"

// ShareOptions configure Share.
type ShareOptions struct {
	Mode         string
	Port         int
	Host         string // public address to put in the link (default: detected)
	OpenFirewall bool   // add a temporary ufw rule when ufw is active
	Token        string // reuse a token (keeps the link the same); random when empty
}

// Share is an active share of a backup file.
type Share struct {
	Link      string
	ToolSHA   string // sha256 of the served tool binary
	Pin       string // certificate pin (also in the link)
	hostPort  string
	Mode      string
	Token     string
	Events    chan transfer.Event
	server    *transfer.Server
	container string
	certDir   string
	ufwPort   int
	cancel    context.CancelFunc
}

// UFWBlocks reports whether ufw is active and does not allow port/tcp yet.
func UFWBlocks(ctx context.Context, port int) bool {
	if !run.Exists("ufw") {
		return false
	}
	out, err := run.Text(ctx, "ufw", "status")
	if err != nil || !strings.Contains(out, "Status: active") {
		return false
	}
	p := strconv.Itoa(port)
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[0] == p || f[0] == p+"/tcp") && strings.Contains(l, "ALLOW") {
			return false
		}
	}
	return true
}

// ProxyAvailable reports whether Coolify's Traefik proxy is running here.
func ProxyAvailable(ctx context.Context) bool {
	st, _ := coolify.ContainerState(ctx, coolify.ProxyContainer)
	if st != "running" {
		return false
	}
	img, _ := docker.Image(ctx, coolify.ProxyContainer)
	return strings.Contains(img, "traefik")
}

// StartShare starts sharing file (encrypted with key).
func StartShare(ctx context.Context, in *coolify.Instance, file, key string, opt ShareOptions) (*Share, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, err
	}
	host := opt.Host
	if host == "" {
		v4, v6 := in.PublicIPs(ctx)
		host = v4
		if host == "" && v6 != "" {
			host = v6
		}
	}
	if host == "" {
		host = coolify.OutboundIP()
	}
	// IPv6 literals need brackets in URLs.
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	token := opt.Token
	if token == "" {
		token = transfer.NewToken()
	}
	self, _ := os.Executable()
	sctx, cancel := context.WithCancel(context.Background())
	s := &Share{Mode: opt.Mode, Token: token, Events: make(chan transfer.Event, 64), cancel: cancel}
	if sum, err := fileSHA256(self); err == nil {
		s.ToolSHA = sum
	}
	cert, err := transfer.NewCert(token)
	if err != nil {
		cancel()
		return nil, err
	}
	s.Pin = cert.Pin

	switch opt.Mode {
	case ShareProxy:
		if !ProxyAvailable(ctx) {
			cancel()
			return nil, errors.New("Coolify's Traefik proxy is not running on this server")
		}
		name := "coolify-mirror-share-" + token[:8]
		image, _ := docker.Image(ctx, "coolify-realtime")
		if image == "" {
			image, _ = docker.Image(ctx, coolify.ProxyContainer)
		}
		// Traefik passes the TLS connection through by its SNI name, so TLS
		// ends in the helper with our pinned certificate.
		s.certDir = filepath.Join(HomeDir, "share-"+token[:8])
		if err := os.MkdirAll(s.certDir, 0o700); err != nil {
			cancel()
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(s.certDir, "cert.pem"), cert.CertPEM, 0o600); err != nil {
			cancel()
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(s.certDir, "key.pem"), cert.KeyPEM, 0o600); err != nil {
			cancel()
			return nil, err
		}
		router := "coolify-mirror-" + token[:8]
		args := []string{"run", "-d", "--name", name, "--network", "coolify", "--restart", "no", "--user", "0:0",
			"--label", "coolify-mirror.share=true",
			"--label", "traefik.enable=true",
			"--label", "traefik.docker.network=coolify",
			"--label", "traefik.tcp.routers." + router + ".rule=HostSNI(`" + transfer.SNIName(token) + "`)",
			"--label", "traefik.tcp.routers." + router + ".entrypoints=https",
			"--label", "traefik.tcp.routers." + router + ".tls.passthrough=true",
			"--label", "traefik.tcp.routers." + router + ".service=" + router,
			"--label", "traefik.tcp.services." + router + ".loadbalancer.server.port=8443",
			"-v", file + ":/share/" + transfer.BackupName + ":ro",
			"-v", self + ":/share/coolify-mirror:ro",
			"-v", s.certDir + ":/share/tls:ro",
			"--entrypoint", "/share/coolify-mirror", image,
			"serve-internal", "--file", "/share/" + transfer.BackupName, "--binary", "/share/coolify-mirror",
			"--token", token, "--listen", ":8443", "--ttl", "24h", "--tls-dir", "/share/tls"}
		if _, err := run.Output(ctx, "docker", args...); err != nil {
			cancel()
			_ = os.RemoveAll(s.certDir)
			return nil, fmt.Errorf("start sharing container: %w", err)
		}
		s.container = name
		s.hostPort = net.JoinHostPort(strings.Trim(host, "[]"), "443")
		s.Link = transfer.Link(strings.TrimSuffix(s.hostPort, ":443"), token, key, cert.Pin)
		go followContainerEvents(sctx, name, s.Events)

	default:
		s.Mode = ShareDirect
		port := opt.Port
		if port == 0 {
			port = DefaultPort
		}
		srv := &transfer.Server{File: file, Token: token, Binary: self, Cert: cert, OnEvent: func(e transfer.Event) {
			select {
			case s.Events <- e:
			default:
			}
		}}
		addr, err := srv.Listen(fmt.Sprintf(":%d", port))
		// The default port may be taken; try the next few.
		for try := 1; err != nil && port == DefaultPort && try < 10 && strings.Contains(err.Error(), "address already in use"); try++ {
			addr, err = srv.Listen(fmt.Sprintf(":%d", port+try))
		}
		if err != nil {
			cancel()
			return nil, fmt.Errorf("listen on port %d: %w", port, err)
		}
		s.server = srv
		actual := addr.(*net.TCPAddr).Port
		if opt.OpenFirewall && UFWBlocks(ctx, actual) {
			if _, err := run.Output(ctx, "ufw", "allow", fmt.Sprintf("%d/tcp", actual), "comment", "coolify-mirror"); err == nil {
				s.ufwPort = actual
			}
		}
		s.hostPort = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(actual))
		s.Link = transfer.Link(s.hostPort, token, key, cert.Pin)
	}
	return s, nil
}

func followContainerEvents(ctx context.Context, name string, out chan<- transfer.Event) {
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", name)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var e transfer.Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Total > 0 {
			select {
			case out <- e:
			default:
			}
		}
	}
	_ = cmd.Wait()
}

func (s *Share) closeFirewall() {
	if s.ufwPort > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = run.Output(ctx, "ufw", "delete", "allow", fmt.Sprintf("%d/tcp", s.ufwPort))
		s.ufwPort = 0
	}
}

// Stop ends the share.
func (s *Share) Stop() {
	s.cancel()
	if s.server != nil {
		s.server.Close()
	}
	if s.container != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = docker.Remove(ctx, s.container)
	}
	if s.certDir != "" {
		_ = os.RemoveAll(s.certDir)
	}
	s.closeFirewall()
}

// Detach keeps sharing in a background process for ttl and returns its PID.
// The background process is this binary running `serve`.
func Detach(file, key string, opt ShareOptions, ttl time.Duration) (int, string, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(LogsDir, 0o700); err != nil {
		return 0, "", err
	}
	logPath := filepath.Join(LogsDir, "share-"+time.Now().Format("20060102-150405")+".log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, "", err
	}
	defer lf.Close()
	// The key goes through the environment, not argv (argv is visible in `ps`).
	args := []string{"serve", file, "--mode", opt.Mode, "--port", strconv.Itoa(opt.Port),
		"--ttl", ttl.String(), "--foreground"}
	if opt.Host != "" {
		args = append(args, "--host", opt.Host)
	}
	if opt.Token != "" {
		args = append(args, "--token", opt.Token)
	}
	if opt.OpenFirewall {
		args = append(args, "--open-firewall")
	}
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), KeyEnv+"="+key)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return 0, "", err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, logPath, nil
}

// ToolCommand is the one-liner that downloads this tool on the other server
// over the pinned HTTPS connection, checks its SHA-256 and opens the menu. The
// backup link is pasted into the menu, so its key stays out of shell history.
func (s *Share) ToolCommand() string {
	return transfer.ToolCommand(s.hostPort, s.Token, s.Pin, s.ToolSHA)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
