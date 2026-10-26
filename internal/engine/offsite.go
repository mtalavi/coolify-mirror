package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/archive"
	"github.com/mtalavi/coolify-mirror/internal/run"
)

// Off-site backups: a backup is copied to a destination - a folder (another
// disk, a mount) or any rclone remote (Google Drive, S3, SFTP, Dropbox…) -
// checked, and only then removed here. It is encrypted to the recovery key,
// so after losing the server it opens with that key on any new one.

// ConfigPath holds the recovery public keys and the notification settings.
var ConfigPath = filepath.Join(HomeDir, "config.json")

// Config is coolify-mirror's own settings on this server.
type Config struct {
	// RecoveryKeys are age X25519 public keys (age1…): every new backup is
	// also encrypted to them. The secret keys are never stored here.
	RecoveryKeys []string     `json:"recovery_keys,omitempty"`
	Notify       NotifyConfig `json:"notify"`
}

// LoadConfig reads the settings (empty settings when there are none).
func LoadConfig() (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigPath, err)
	}
	return c, nil
}

// Save writes the settings (readable by root only: they hold SMTP/bot secrets).
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigPath)
}

// NewRecoveryKey creates a recovery key and adds its public half to the
// settings. The secret half is returned once and never stored.
func NewRecoveryKey() (secret, public string, err error) {
	c, err := LoadConfig()
	if err != nil {
		return "", "", err
	}
	if secret, public, err = archive.NewIdentityKey(); err != nil {
		return "", "", err
	}
	c.RecoveryKeys = append(c.RecoveryKeys, public)
	return secret, public, c.Save()
}

// AddRecoveryKey adds an existing public key (one recovery key for several
// servers) or the public half of a pasted secret key.
func AddRecoveryKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, archive.IdentityPrefix) {
		pub, err := archive.PublicKeyOf(key)
		if err != nil {
			return "", fmt.Errorf("not a recovery key: %w", err)
		}
		key = pub
	}
	if err := archive.ValidPublicKey(key); err != nil {
		return "", fmt.Errorf("not a recovery public key (age1…): %w", err)
	}
	c, err := LoadConfig()
	if err != nil {
		return "", err
	}
	for _, k := range c.RecoveryKeys {
		if k == key {
			return key, nil
		}
	}
	c.RecoveryKeys = append(c.RecoveryKeys, key)
	return key, c.Save()
}

// RemoveRecoveryKeys forgets every recovery key (old backups keep opening
// with them; new ones get a random key again).
func RemoveRecoveryKeys() error {
	c, err := LoadConfig()
	if err != nil {
		return err
	}
	c.RecoveryKeys = nil
	return c.Save()
}

// BackupFileName names a backup: host, mode, the Coolify version it comes
// from and the time - "coolify-shop1-full-coolify4.5.0-20261027-033000.cmb".
// The time stays last (saved-file lists read it from there).
func BackupFileName(host, mode, coolifyVersion string, t time.Time) string {
	v := safeName(strings.ReplaceAll(coolifyVersion, ".", "-"))
	v = strings.ReplaceAll(v, "-", ".")
	if v == "" {
		v = "unknown"
	}
	return fmt.Sprintf("coolify-%s-%s-coolify%s-%s.cmb", safeName(host), mode, v, t.Format("20060102-150405"))
}

// BackupPrefix is what the file names of one server's backups of one mode
// start with, so cleaning a shared folder never touches other servers'.
func BackupPrefix(host, mode string) string {
	return fmt.Sprintf("coolify-%s-%s-", safeName(host), mode)
}

// ---------------------------------------------------------------------------
// Destinations

// IsRemote reports whether dest is an rclone remote ("name:path"), not a
// folder on this server.
func IsRemote(dest string) bool {
	dest = strings.TrimSpace(dest)
	if strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, ".") || strings.HasPrefix(dest, "[") ||
		strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
		return false
	}
	i := strings.Index(dest, ":")
	if i <= 0 || strings.Contains(dest[:i], "/") {
		return false
	}
	// HOST:PORT/code is a share code, not a remote.
	return !hostPortCode.MatchString(dest[i+1:])
}

var (
	hostPortCode = regexp.MustCompile(`^[0-9]+/`)
	shareCode    = regexp.MustCompile(`^(\[[0-9a-fA-F:]+\]|[^/\s:]+)(:[0-9]+)?/[a-z0-9]{4}(-[a-z0-9]{2,4}){3,}$`)
)

// RcloneInstalled reports whether rclone is on this server.
func RcloneInstalled() bool { return run.Exists("rclone") }

// RcloneRemotes lists the remotes configured in rclone ("gdrive:", "s3:").
func RcloneRemotes(ctx context.Context) ([]string, error) {
	if !RcloneInstalled() {
		return nil, errNoRclone
	}
	out, err := run.Text(ctx, "rclone", "listremotes")
	if err != nil {
		return nil, err
	}
	var rs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			rs = append(rs, l)
		}
	}
	return rs, nil
}

var errNoRclone = errors.New("rclone is not installed - install it (https://rclone.org/install/: curl https://rclone.org/install.sh | sudo bash) and add a destination with: rclone config")

// checkRemote fails early when dest names an rclone remote that does not exist.
func checkRemote(ctx context.Context, dest string) error {
	if !IsRemote(dest) {
		return nil
	}
	remotes, err := RcloneRemotes(ctx)
	if err != nil {
		return err
	}
	name := dest[:strings.Index(dest, ":")+1]
	for _, r := range remotes {
		if r == name {
			return nil
		}
	}
	return fmt.Errorf("rclone has no remote %q (it has: %s) - add it with: rclone config", strings.TrimSuffix(name, ":"), strings.Join(remotes, " "))
}

func joinDest(dest, name string) string {
	if IsRemote(dest) {
		if strings.HasSuffix(dest, ":") || strings.HasSuffix(dest, "/") {
			return dest + name
		}
		return dest + "/" + name
	}
	return filepath.Join(dest, name)
}

// Upload copies file to the destination folder and checks the copy: the
// same size and checksum (rclone check where the remote supports hashes).
func Upload(ctx context.Context, file, dest string, progress func(done, total int64)) error {
	fi, err := os.Stat(file)
	if err != nil {
		return err
	}
	name := filepath.Base(file)
	if !IsRemote(dest) {
		return copyLocalChecked(file, filepath.Join(dest, name), progress)
	}
	if err := checkRemote(ctx, dest); err != nil {
		return err
	}
	target := joinDest(dest, name)
	stop := watchProgress(ctx, fi.Size(), func() int64 { return remoteSize(ctx, target) }, progress, 5*time.Second)
	_, err = run.Do(ctx, run.Spec{Name: "rclone", Args: []string{"copyto", file, target, "--retries", "5", "--low-level-retries", "20"}})
	stop()
	if err != nil {
		return fmt.Errorf("upload to %s: %w", target, err)
	}
	// Size and, when both sides have a common hash, the checksum.
	if _, err := run.Do(ctx, run.Spec{Name: "rclone", Args: []string{"check", filepath.Dir(file), parentOf(target),
		"--one-way", "--include", "/" + name}}); err != nil {
		return fmt.Errorf("the uploaded copy %s does not match this file: %w", target, err)
	}
	if progress != nil {
		progress(fi.Size(), fi.Size())
	}
	return nil
}

func parentOf(remotePath string) string {
	i := strings.LastIndex(remotePath, "/")
	if i < 0 {
		return remotePath[:strings.Index(remotePath, ":")+1]
	}
	return remotePath[:i]
}

func remoteSize(ctx context.Context, target string) int64 {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := run.Output(cctx, "rclone", "size", "--json", target)
	if err != nil {
		return -1
	}
	var s struct {
		Bytes int64 `json:"bytes"`
	}
	if json.Unmarshal(out, &s) != nil {
		return -1
	}
	return s.Bytes
}

// watchProgress polls size() until stopped and reports it.
func watchProgress(ctx context.Context, total int64, size func() int64, progress func(done, total int64), every time.Duration) func() {
	if progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if n := size(); n >= 0 {
					progress(n, total)
				}
			}
		}
	}()
	return func() { close(done) }
}

func copyLocalChecked(src, dst string, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, _ := in.Stat()
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), &countingReader{r: in, fn: func(n int64) {
		if progress != nil {
			progress(n, fi.Size())
		}
	}})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	want := hex.EncodeToString(h.Sum(nil))
	got, err := fileSHA256(dst)
	if err != nil || got != want {
		return fmt.Errorf("the copy %s does not match this file", dst)
	}
	return nil
}

type countingReader struct {
	r  io.Reader
	n  int64
	fn func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.fn != nil {
		c.fn(c.n)
	}
	return n, err
}

// StoredBackupAt is one backup file in a destination.
type StoredBackupAt struct {
	Name    string
	Size    int64
	ModTime time.Time
	Path    string // full path or remote path, to download it
}

// ListBackupsAt lists the backup files (.cmb) of a destination, newest first.
func ListBackupsAt(ctx context.Context, dest string) ([]StoredBackupAt, error) {
	var out []StoredBackupAt
	if !IsRemote(dest) {
		es, err := os.ReadDir(dest)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".cmb") {
				fi, err := e.Info()
				if err != nil {
					continue
				}
				out = append(out, StoredBackupAt{Name: e.Name(), Size: fi.Size(), ModTime: stampTime(strings.TrimSuffix(e.Name(), ".cmb"), fi.ModTime()), Path: filepath.Join(dest, e.Name())})
			}
		}
	} else {
		if err := checkRemote(ctx, dest); err != nil {
			return nil, err
		}
		b, err := run.Output(ctx, "rclone", "lsjson", "--files-only", "--no-mimetype", dest)
		if err != nil {
			return nil, err
		}
		var items []struct {
			Name    string    `json:"Name"`
			Size    int64     `json:"Size"`
			ModTime time.Time `json:"ModTime"`
		}
		if err := json.Unmarshal(b, &items); err != nil {
			return nil, err
		}
		for _, it := range items {
			if strings.HasSuffix(it.Name, ".cmb") {
				out = append(out, StoredBackupAt{Name: it.Name, Size: it.Size, ModTime: stampTime(strings.TrimSuffix(it.Name, ".cmb"), it.ModTime), Path: joinDest(dest, it.Name)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// PruneBackupsAt keeps the newest keep backups whose names start with prefix
// in dest and deletes older ones (keep <= 0: keeps everything).
func PruneBackupsAt(ctx context.Context, dest, prefix string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	all, err := ListBackupsAt(ctx, dest)
	if err != nil {
		return nil, err
	}
	var deleted []string
	n := 0
	for _, b := range all {
		if !strings.HasPrefix(b.Name, prefix) {
			continue
		}
		n++
		if n <= keep {
			continue
		}
		if IsRemote(dest) {
			_, err = run.Do(ctx, run.Spec{Name: "rclone", Args: []string{"deletefile", b.Path}})
		} else {
			err = os.Remove(b.Path)
			_ = os.Remove(b.Path + ".key")
		}
		if err != nil {
			return deleted, fmt.Errorf("delete %s: %w", b.Name, err)
		}
		deleted = append(deleted, b.Name)
	}
	return deleted, nil
}

// ---------------------------------------------------------------------------
// Restore sources

// IsShareSource reports whether s is a share code or a share link (the
// backup key travels with it).
func IsShareSource(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return strings.Contains(s, "#key=") || strings.Contains(s, "/cm/")
	}
	return shareCode.MatchString(s)
}

// IsOffsiteSource reports whether s is a backup to fetch first: an rclone
// path (file or folder), a plain https:// address, or a local folder.
func IsOffsiteSource(s string) bool {
	s = strings.TrimSpace(s)
	if IsShareSource(s) {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || IsRemote(s) {
		return true
	}
	fi, err := os.Stat(s)
	return err == nil && fi.IsDir()
}

// FetchOffsite brings a backup from a folder, an rclone path or an https://
// address to this server and returns the local file. A folder (or an rclone
// folder) gives its newest backup.
func FetchOffsite(ctx context.Context, src string, progress func(done, total int64)) (string, error) {
	src = strings.TrimSpace(src)
	if err := os.MkdirAll(IncomingDir, 0o700); err != nil {
		return "", err
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return downloadURL(ctx, src, progress)
	}
	if !IsRemote(src) {
		fi, err := os.Stat(src)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() {
			return src, nil
		}
		list, err := ListBackupsAt(ctx, src)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "", fmt.Errorf("no backup (.cmb) in %s", src)
		}
		return list[0].Path, nil
	}
	if err := checkRemote(ctx, src); err != nil {
		return "", err
	}
	file := src
	var size int64 = -1
	if !strings.HasSuffix(src, ".cmb") {
		list, err := ListBackupsAt(ctx, src)
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "", fmt.Errorf("no backup (.cmb) in %s", src)
		}
		file, size = list[0].Path, list[0].Size
	}
	if size < 0 {
		size = remoteSize(ctx, file)
	}
	local := filepath.Join(IncomingDir, path.Base(strings.SplitN(file, ":", 2)[1]))
	if path.Base(strings.SplitN(file, ":", 2)[1]) == "." || path.Base(strings.SplitN(file, ":", 2)[1]) == "/" {
		local = filepath.Join(IncomingDir, "offsite.cmb")
	}
	stop := watchProgress(ctx, size, func() int64 {
		if fi, err := os.Stat(local); err == nil {
			return fi.Size()
		}
		return 0
	}, progress, 2*time.Second)
	_, err := run.Do(ctx, run.Spec{Name: "rclone", Args: []string{"copyto", file, local, "--retries", "5", "--low-level-retries", "20"}})
	stop()
	if err != nil {
		_ = os.Remove(local)
		return "", fmt.Errorf("download %s: %w", file, err)
	}
	return local, nil
}

func downloadURL(ctx context.Context, u string, progress func(done, total int64)) (string, error) {
	pu, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	name := path.Base(pu.Path)
	if !strings.HasSuffix(name, ".cmb") {
		name = "download.cmb"
	}
	local := filepath.Join(IncomingDir, name)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", u, resp.StatusCode)
	}
	out, err := os.OpenFile(local, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, &countingReader{r: resp.Body, fn: func(n int64) {
		if progress != nil {
			progress(n, resp.ContentLength)
		}
	}})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(local)
		return "", err
	}
	return local, nil
}

// ---------------------------------------------------------------------------
// One off-site run (the scheduled backup)

// OffsiteResult is what an off-site run did, kept for `schedule status`.
type OffsiteResult struct {
	Name     string    `json:"name,omitempty"` // schedule name
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	File     string    `json:"file,omitempty"`
	Dest     string    `json:"dest"`
	Size     int64     `json:"size"`
	Started  time.Time `json:"started"`
	Duration string    `json:"duration"`
	Deleted  []string  `json:"deleted,omitempty"` // older backups removed at the destination
	Kept     string    `json:"kept,omitempty"`    // what stays on this server
}

// uploadedList names the local backups whose upload was checked: only those
// are ever removed here (a backup made by hand is never touched).
var uploadedList = filepath.Join(HomeDir, "schedules", "uploaded.txt")

func readUploaded() map[string]bool {
	m := map[string]bool{}
	b, _ := os.ReadFile(uploadedList)
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			m[l] = true
		}
	}
	return m
}

func writeUploaded(m map[string]bool) {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	_ = os.MkdirAll(filepath.Dir(uploadedList), 0o700)
	_ = os.WriteFile(uploadedList, []byte(strings.Join(names, "\n")+"\n"), 0o600)
}

// AfterUpload records that file was uploaded and checked, then keeps the
// newest keepLocal of the uploaded backups with this prefix here and removes
// the others (0 = none stays).
func AfterUpload(file, prefix string, keepLocal int) ([]string, error) {
	dir := filepath.Dir(file)
	up := readUploaded()
	up[filepath.Base(file)] = true
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	var mine []StoredBackupAt
	for _, e := range es {
		present[e.Name()] = true
		if e.Type().IsRegular() && up[e.Name()] && strings.HasPrefix(e.Name(), prefix) {
			fi, _ := e.Info()
			mine = append(mine, StoredBackupAt{Name: e.Name(), Path: filepath.Join(dir, e.Name()), ModTime: stampTime(strings.TrimSuffix(e.Name(), ".cmb"), fi.ModTime())})
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].ModTime.After(mine[j].ModTime) })
	var removed []string
	for i, b := range mine {
		if i < keepLocal {
			continue
		}
		if err := os.Remove(b.Path); err != nil {
			writeUploaded(up)
			return removed, err
		}
		_ = os.Remove(b.Path + ".key")
		removed = append(removed, b.Name)
		present[b.Name] = false
	}
	for n := range up {
		if !present[n] {
			delete(up, n)
		}
	}
	writeUploaded(up)
	return removed, nil
}

// StatusDir keeps the last result of each schedule.
var StatusDir = filepath.Join(HomeDir, "schedules")

// SaveResult records an off-site run.
func SaveResult(r OffsiteResult) {
	if r.Name == "" {
		r.Name = "manual"
	}
	_ = os.MkdirAll(StatusDir, 0o700)
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(filepath.Join(StatusDir, safeName(r.Name)+".json"), b, 0o600)
}

// LastResult reads the last run of a schedule.
func LastResult(name string) (*OffsiteResult, error) {
	b, err := os.ReadFile(filepath.Join(StatusDir, safeName(name)+".json"))
	if err != nil {
		return nil, err
	}
	r := &OffsiteResult{}
	return r, json.Unmarshal(b, r)
}

// RcloneVersion returns rclone's version line ("" when missing).
func RcloneVersion(ctx context.Context) string {
	if !RcloneInstalled() {
		return ""
	}
	out, err := exec.CommandContext(ctx, "rclone", "version").Output()
	if err != nil {
		return ""
	}
	return strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
}
