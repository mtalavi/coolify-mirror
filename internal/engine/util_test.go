package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeniedHostPath(t *testing.T) {
	denied := []string{"/", "/etc", "/etc/nginx", "/data", "/data/coolify", "/data/coolify/source/.env",
		"/var", "/var/lib", "/var/lib/docker/volumes/x", "/root", "/root/.ssh", "/home", "/home/bob",
		"/home/bob/.ssh/authorized_keys", "/data/coolify-mirror/backups", "relative/path"}
	allowed := []string{"/data/coolify/applications/abc", "/data/coolify/proxy", "/data/coolify/ssh",
		"/srv/uploads", "/mnt/data", "/opt/app/data", "/home/bob/app-data", "/root/app-data", "/data/uploads"}
	for _, p := range denied {
		if !deniedHostPath(p) {
			t.Errorf("%s should be denied", p)
		}
	}
	for _, p := range allowed {
		if deniedHostPath(p) {
			t.Errorf("%s should be allowed", p)
		}
	}
}

func TestCompareHelpers(t *testing.T) {
	if HumanBytes(1500) != "1.5 kB" || HumanBytes(999) != "999 B" {
		t.Fatal(HumanBytes(1500), HumanBytes(999))
	}
	if !isAnonymousVolume("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") || isAnonymousVolume("postgres-data-x") {
		t.Fatal("anonymous volume detection")
	}
}

func TestImageTitle(t *testing.T) {
	for _, c := range []struct {
		refs []string
		want string
	}{
		{[]string{"o60obsi6npnbrtpsseqthzav:4c57d5980fc9268546530080cce45aaf4a7471d2", "o60obsi6npnbrtpsseqthzav:latest"}, "Images o60obsi6npnbrtpsseqthzav:4c57d5980fc9  +1 more"},
		{[]string{"postgres:16-alpine"}, "Image  postgres:16-alpine"},
		{[]string{"localhost:5000/app"}, "Image  localhost:5000/app"},
		{nil, "Image"},
	} {
		if got := imageTitle(c.refs); got != c.want {
			t.Errorf("%v: %q, want %q", c.refs, got, c.want)
		}
	}
}

func TestMarkerOffset(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 3<<20)
	body := "-- dump\nINSERT '" + long + "-- coolify-mirror: accounts';\n"
	p := filepath.Join(dir, "d.sql")
	content := body + "\n" + mysqlAccountsMarker + "\nCREATE USER IF NOT EXISTS 'a'@'%';\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	off, ok := markerOffset(p, mysqlAccountsMarker)
	if !ok || off != int64(len(body)+1) {
		t.Fatalf("markerOffset = %d %v, want %d", off, ok, len(body)+1)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := markerOffset(p, mysqlAccountsMarker); ok {
		t.Fatal("found a marker in a dump without accounts")
	}
}
