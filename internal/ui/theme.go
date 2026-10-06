package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5B3CC4", Dark: "#A78BFA"}
	colOK     = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#4ADE80"}
	colErr    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F87171"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#FBBF24"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	colText   = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}

	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sBold   = lipgloss.NewStyle().Bold(true).Foreground(colText)
	sMuted  = lipgloss.NewStyle().Foreground(colMuted)
	sOK     = lipgloss.NewStyle().Foreground(colOK)
	sErr    = lipgloss.NewStyle().Foreground(colErr)
	sWarn   = lipgloss.NewStyle().Foreground(colWarn)
	sAccent = lipgloss.NewStyle().Foreground(colAccent)
	sBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(0, 1)
	sErrBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colErr).Padding(0, 1)
	sLink   = lipgloss.NewStyle().Bold(true).Foreground(colOK)
)

// boxed renders s in a bordered box that never exceeds the terminal width
// (long lines wrap inside the box instead of breaking the border).
func boxed(st lipgloss.Style, s string) string {
	w := termWidth() - 1
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if lw := lipgloss.Width(line); lw > widest {
			widest = lw
		}
	}
	if widest+4 > w {
		st = st.Width(w - 2)
	}
	return st.Render(s)
}

// keys: esc goes back (ends the form like ctrl+c), as the screens say.
func keys() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "back"))
	km.Select.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	km.Select.Next = key.NewBinding(key.WithKeys("enter", "tab"), key.WithHelp("enter", "choose"))
	// The single-choice lists are short; no search there, so esc never
	// has to mean "leave the search".
	km.Select.Filter = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"), key.WithDisabled())
	return km
}

// listKeys: the keys of a multi-select list, with the help line naming space
// (what the descriptions say) and enter as continue.
func listKeys() *huh.KeyMap {
	km := keys()
	km.MultiSelect.Toggle = key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "tick"))
	km.MultiSelect.Submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue"))
	km.MultiSelect.Next = key.NewBinding(key.WithKeys("enter", "tab"), key.WithHelp("enter", "continue"))
	km.MultiSelect.Filter = key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search"))
	return km
}

func theme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Title = t.Focused.Title.Foreground(colAccent)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colAccent)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(colAccent)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(colOK)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colOK)
	return t
}
