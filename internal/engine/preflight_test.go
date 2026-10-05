package engine

import (
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
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
