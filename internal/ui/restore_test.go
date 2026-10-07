package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestHangWrap(t *testing.T) {
	for name, c := range map[string]struct {
		s           string
		first, rest int
	}{
		"words":          {"note: scheduled tasks/backups were restored and are active here too - disable them on the old server once you switch", 60, 54},
		"one long word":  {strings.Repeat("x", 130), 40, 36},
		"wide runes":     {strings.Repeat("宽", 70), 40, 36},
		"wide and words": {"/srv/" + strings.Repeat("数据", 30) + " exists: moved aside", 30, 26},
		"fits":           {"short", 40, 36},
	} {
		lines := hangWrap(c.s, c.first, c.rest)
		joined := strings.ReplaceAll(strings.Join(lines, ""), " ", "")
		if joined != strings.ReplaceAll(c.s, " ", "") {
			t.Errorf("%s: text changed: %q", name, lines)
		}
		for i, l := range lines {
			w := c.rest
			if i == 0 {
				w = c.first
			}
			if lipgloss.Width(l) > w {
				t.Errorf("%s: line %d is %d cells, max %d: %q", name, i, lipgloss.Width(l), w, l)
			}
		}
	}
}
