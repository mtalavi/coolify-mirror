package engine

import "testing"

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
