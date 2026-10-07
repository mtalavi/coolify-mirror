package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
)

func TestVersionBlocker(t *testing.T) {
	man := &Manifest{Source: SourceInfo{CoolifyVersion: "4.3.23"}}
	if b := versionBlocker(&coolify.Instance{Version: "4.3.23"}, man); b != "" {
		t.Fatalf("same version blocked: %s", b)
	}
	if b := versionBlocker(&coolify.Instance{Version: "v4.3.23"}, man); b != "" {
		t.Fatalf("v prefix blocked: %s", b)
	}
	for _, v := range []string{"4.3.24", "4.3.22", "4.4.0-rc.1", ""} {
		if b := versionBlocker(&coolify.Instance{Version: v}, man); b == "" {
			t.Fatalf("version %q not blocked", v)
		}
	}
	if b := versionBlocker(&coolify.Instance{Version: "4.3.23"}, &Manifest{}); !strings.Contains(b, "does not say") {
		t.Fatalf("missing source version not blocked: %q", b)
	}
}

func TestHostRequirementBlockers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bounded-build-v1.sh")
	if err := os.WriteFile(p, []byte("policy-v1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	man := &Manifest{HostRequirements: []HostRequirement{{
		Path: p, SHA256: sum, Reason: "test build wrapper",
	}}}
	if b := hostRequirementBlockers(man); len(b) != 0 {
		t.Fatalf("matching host prerequisite blocked: %v", b)
	}

	if err := os.WriteFile(p, []byte("policy-v2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if b := hostRequirementBlockers(man); len(b) != 1 || !strings.Contains(b[0], "SHA-256") {
		t.Fatalf("changed host prerequisite not blocked clearly: %v", b)
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if b := hostRequirementBlockers(man); len(b) != 1 || !strings.Contains(b[0], "missing or unreadable") {
		t.Fatalf("missing host prerequisite not blocked clearly: %v", b)
	}
}

func TestRelevantWarnings(t *testing.T) {
	w := []string{"Team-scoped GitHub Apps were exported with credentials. Update each GitHub App webhook URL.", "S3 storage credentials were exported."}
	if got := relevantWarnings(w, &dbx.Export{Tables: map[string][]coolify.Row{}}); len(got) != 1 || !strings.HasPrefix(got[0], "S3") {
		t.Errorf("without a GitHub App: %v", got)
	}
	ex := &dbx.Export{Tables: map[string][]coolify.Row{"github_apps": {{"id": 3}}}}
	if got := relevantWarnings(w, ex); len(got) != 2 {
		t.Errorf("with a GitHub App: %v", got)
	}
}
