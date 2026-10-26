package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/run"
)

// A whole-server restore copies the source's Coolify database, and with it
// the address of the source's panel (instance_settings.fqdn) and its public
// IPs. Coolify builds the proxy route of its own panel from that address on
// every start (app:init -> setupDynamicProxyConfiguration), so the restored
// panel answered only on the old server's domain: "This domain is not
// registered" on its own. These values belong to the machine, not to the
// data, so a full restore keeps this server's own.

// Panel is the identity of a Coolify panel: its address and public IPs.
type Panel struct {
	URL  string // instance_settings.fqdn ("" = no domain, http://IP:8000)
	IPv4 string
	IPv6 string
}

// Panel URL choices of a full restore (FullOptions.PanelURL).
const (
	PanelKeep   = "keep"   // this server's address from before the restore (default)
	PanelSource = "source" // the source server's address
	PanelNone   = "none"   // no domain: open the panel at http://IP:8000
)

// FullOptions are the choices of a whole-server restore.
type FullOptions struct {
	// PanelURL is PanelKeep (default), PanelSource, PanelNone or an address.
	PanelURL string
}

// ReadPanel reads the panel identity from Coolify's database.
func ReadPanel(ctx context.Context, in *coolify.Instance) (Panel, error) {
	var rows []struct {
		URL  *string `json:"fqdn"`
		IPv4 *string `json:"public_ipv4"`
		IPv6 *string `json:"public_ipv6"`
	}
	if err := in.Query(ctx, `SELECT fqdn, public_ipv4, public_ipv6 FROM instance_settings ORDER BY id LIMIT 1`, &rows); err != nil {
		return Panel{}, err
	}
	var p Panel
	if len(rows) == 1 {
		p.URL, p.IPv4, p.IPv6 = strings.TrimSpace(deref(rows[0].URL)), strings.TrimSpace(deref(rows[0].IPv4)), strings.TrimSpace(deref(rows[0].IPv6))
	}
	return p, nil
}

// NormalizePanelURL checks an address typed for the panel and adds http://
// when the scheme is missing.
func NormalizePanelURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not a panel address (example: https://coolify.example.com)", s)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
}

// ResolvePanelURL turns a FullOptions.PanelURL choice into an address.
func ResolvePanelURL(choice string, before, source Panel) (string, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", PanelKeep:
		return before.URL, nil
	case PanelSource:
		return source.URL, nil
	case PanelNone, "-":
		return "", nil
	}
	return NormalizePanelURL(choice)
}

// WritePanel stores the panel identity in Coolify's database. The wildcard
// domain of this server (server_settings) follows when it was built from the
// old public IP (the default http://IP.sslip.io).
func WritePanel(ctx context.Context, in *coolify.Instance, p, old Panel) error {
	nullable := func(s string) string {
		if s == "" {
			return "NULL"
		}
		return coolify.SQLString(s)
	}
	script := fmt.Sprintf(`UPDATE instance_settings SET fqdn = %s, public_ipv4 = %s, public_ipv6 = %s;`,
		nullable(p.URL), nullable(p.IPv4), nullable(p.IPv6))
	if old.IPv4 != "" && p.IPv4 != "" && old.IPv4 != p.IPv4 {
		script += fmt.Sprintf("\nUPDATE server_settings SET wildcard_domain = replace(wildcard_domain, %s, %s) WHERE server_id = %d AND wildcard_domain LIKE %s;",
			coolify.SQLString(old.IPv4), coolify.SQLString(p.IPv4), coolify.LocalServerID, coolify.SQLString("%"+old.IPv4+"%"))
	}
	return in.ExecSQL(ctx, script, true)
}

// RebuildPanelProxy asks Coolify to write the proxy route of its panel
// again, with its own code (the same call as saving Settings in the panel).
func RebuildPanelProxy(ctx context.Context, in *coolify.Instance) error {
	var out struct {
		OK bool `json:"ok"`
	}
	if err := in.PHP(ctx, "panel_proxy", nil, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("Coolify could not write the proxy configuration of its panel")
	}
	return nil
}

// CheckPanel waits until the panel answers on port 8000 and, with an
// address, through the proxy on that address. It returns what does not work.
func CheckPanel(ctx context.Context, panelURL string, wait time.Duration) []string {
	var problems []string
	if err := pollHealth(ctx, "http://127.0.0.1:8000/api/health", "", wait); err != nil {
		problems = append(problems, "the Coolify panel does not answer on port 8000: "+err.Error())
		return problems
	}
	if panelURL == "" {
		return nil
	}
	u, err := url.Parse(panelURL)
	if err != nil {
		return []string{"the panel address " + panelURL + " is not valid"}
	}
	port := "80"
	if u.Scheme == "https" {
		port = "443"
	}
	if err := pollHealth(ctx, u.Scheme+"://127.0.0.1:"+port+"/api/health", u.Hostname(), wait); err != nil {
		problems = append(problems, "the Coolify panel does not answer on "+panelURL+" through the proxy: "+err.Error())
	}
	return problems
}

// pollHealth GETs u (with Host/SNI host when set) until it returns "OK".
func pollHealth(ctx context.Context, u, host string, wait time.Duration) error {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host}, // a fresh certificate may not be issued yet
		DialContext:     (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	}
	cl := &http.Client{Transport: tr, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(wait)
	var last error
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if host != "" {
			req.Host = host
		}
		resp, err := cl.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "OK" {
				return nil
			}
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
			if resp.StatusCode/100 == 3 {
				last = fmt.Errorf("HTTP %d to %s", resp.StatusCode, resp.Header.Get("Location"))
			}
		} else {
			last = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(3 * time.Second)
	}
}

// RepairPanel gives this server's Coolify its own panel identity back (after
// a whole-server restore made with an older version) and rebuilds the proxy
// route of the panel. url: "" keeps the current address, PanelNone clears it.
func RepairPanel(ctx context.Context, in *coolify.Instance, panelURL string) (Panel, []string, error) {
	cur, err := ReadPanel(ctx, in)
	if err != nil {
		return Panel{}, nil, err
	}
	next := cur
	switch {
	case panelURL == "" || strings.EqualFold(panelURL, PanelKeep):
	case strings.EqualFold(panelURL, PanelNone) || panelURL == "-":
		next.URL = ""
	default:
		if next.URL, err = NormalizePanelURL(panelURL); err != nil {
			return Panel{}, nil, err
		}
	}
	if v4 := detectPublicIPv4(ctx); v4 != "" {
		next.IPv4 = v4
	}
	if err := WritePanel(ctx, in, next, cur); err != nil {
		return Panel{}, nil, fmt.Errorf("save the panel address: %w", err)
	}
	if err := RebuildPanelProxy(ctx, in); err != nil {
		return Panel{}, nil, fmt.Errorf("rebuild the proxy route of the panel: %w", err)
	}
	run.Logf("panel repaired: %+v (was %+v)", next, cur)
	return next, CheckPanel(ctx, next.URL, 90*time.Second), nil
}

// detectPublicIPv4 finds this machine's public IPv4 like Coolify's installer
// (ifconfig.io), falling back to the address of the outgoing interface.
func detectPublicIPv4(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	for _, u := range []string{"https://ifconfig.io", "https://api.ipify.org"} {
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", "curl/8")
		resp, err := (&http.Client{Transport: &http.Transport{DialContext: (&net.Dialer{}).DialContext,
			ForceAttemptHTTP2: false}}).Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if ip := net.ParseIP(strings.TrimSpace(string(b))); ip != nil && ip.To4() != nil {
			return ip.String()
		}
	}
	return coolify.OutboundIP()
}
