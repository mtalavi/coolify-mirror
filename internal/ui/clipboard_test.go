package ui

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52(t *testing.T) {
	cmd := "sudo coolify-mirror restore 203.0.113.10/hi4i-2dzx"
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(cmd)) + "\a"
	if got := osc52(cmd); got != want {
		t.Fatalf("plain terminal: %q, want %q", got, want)
	}
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	got := osc52(cmd)
	if !strings.HasPrefix(got, "\x1bPtmux;\x1b\x1b]52;c;") || !strings.HasSuffix(got, "\a\x1b\\") {
		t.Fatalf("tmux passthrough: %q", got)
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "screen.xterm-256color")
	if got := osc52(cmd); !strings.HasPrefix(got, "\x1bP\x1b]52;c;") || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("screen passthrough: %q", got)
	}
}
