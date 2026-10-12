package engine

import (
	"context"
	"strings"
	"testing"
)

// The compatibility test runs with COOLIFY_MIRROR_COMPAT_URL=off, so a
// result published earlier can never fail the new test.
func TestLookupCompatOff(t *testing.T) {
	t.Setenv("COOLIFY_MIRROR_COMPAT_URL", "off")
	saved := CompatURL
	defer func() { CompatURL = saved }()
	CompatURL = "http://127.0.0.1:1/must-not-be-used"
	if _, ok := lookupCompat(context.Background(), "4.4.6"); ok {
		t.Fatal("lookup was not skipped")
	}
}

// Regression: 1.9.0 (tested with 4.4.2) said "✓ tested" and, below it,
// "coolify-mirror 1.8.1 did not work with Coolify 4.4.2".
func TestApplyCompatEntry(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "1.9.0"
	run := func(builtIn bool, e CompatEntry) *Compat {
		c := &Compat{CoolifyVersion: "4.4.2", Status: "untested"}
		if builtIn {
			c.Status = "tested"
		}
		applyCompatEntry(c, e, "update")
		return c
	}
	if c := run(true, CompatEntry{Status: "fail", Tool: "1.8.1"}); c.Status != "tested" || len(c.Warnings)+len(c.Blockers) != 0 {
		t.Errorf("tested by this release, older tool failed: %+v", c)
	}
	if c := run(false, CompatEntry{Status: "fail", Tool: "1.8.1"}); c.Status != "untested" || len(c.Warnings) != 1 {
		t.Errorf("untested, older tool failed: %+v", c)
	}
	// A published failure refuses restores only; backups keep working.
	if c := run(true, CompatEntry{Status: "fail", Tool: "1.9.0", Detail: "boom\r source did not start "}); c.Status != "failed" ||
		len(c.Blockers) != 0 || len(c.RestoreBlockers()) != 1 || strings.Contains(c.Failed, "\r") || !strings.Contains(c.Failed, "(boom source did not start)") {
		t.Errorf("this release failed the published test: %+v", c)
	}
	if c := run(false, CompatEntry{Status: "ok", Tool: "1.9.0"}); c.Status != "tested" {
		t.Errorf("published ok: %+v", c)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"4.3.9", "4.3.23", -1}, {"v4.3.23", "4.3.23", 0}, {"1.10.0", "1.9.9", 1},
		{"4.0.0-beta.400", "4.0.0-beta.401", -1}, {"1.5", "1.5.0", 0},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
