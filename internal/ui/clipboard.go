package ui

import (
	"encoding/base64"
	"os"
	"strings"
)

// osc52 is the escape sequence that asks the terminal to put text on the
// clipboard of the computer the person sits at (OSC 52). It travels through
// SSH, sudo's pty and tmux; Windows Terminal, iTerm2, WezTerm, kitty, Ghostty,
// Alacritty and foot honour it. Other terminals ignore it silently.
func osc52(text string) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if os.Getenv("TMUX") != "" {
		// tmux passes it on only inside its passthrough wrapper.
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		seq = "\x1bP" + seq + "\x1b\\"
	}
	return seq
}

// copyToClipboard sends text to the person's clipboard (see osc52).
func copyToClipboard(text string) {
	_, _ = os.Stdout.WriteString(osc52(text))
}
