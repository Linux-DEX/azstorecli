// Package theme holds the lipgloss style set used across every screen.
// Screens should pull colors and styles from a Theme rather than
// hardcoding lipgloss.Color values, so switching internal/theme/themes/*
// actually re-skins the whole app.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme is the full style set one screen needs to render consistently
// with the rest of the app.
type Theme struct {
	Name string

	Background lipgloss.Color
	Foreground lipgloss.Color
	Muted      lipgloss.Color
	Accent     lipgloss.Color
	Border     lipgloss.Color
	Danger     lipgloss.Color

	Title    lipgloss.Style
	HelpBar  lipgloss.Style
	StatusOK lipgloss.Style
	StatusErr lipgloss.Style
	Selected lipgloss.Style
}

// Dark is the built-in default theme. Additional themes (light, nord, ...)
// live under internal/theme/themes/ as embedded YAML once the loader in
// load.go (M7) reads user-selectable themes; this Go value is the
// hardcoded fallback so the app never boots with zero styling.
func Dark() Theme {
	t := Theme{
		Name:       "dark",
		Background: lipgloss.Color("#0d0d0d"),
		Foreground: lipgloss.Color("#e6e6e6"),
		Muted:      lipgloss.Color("#8a8a8a"),
		Accent:     lipgloss.Color("#5fb3ff"),
		Border:     lipgloss.Color("#3a3a3a"),
		Danger:     lipgloss.Color("#ff6b6b"),
	}

	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.HelpBar = lipgloss.NewStyle().Foreground(t.Muted)
	t.StatusOK = lipgloss.NewStyle().Foreground(lipgloss.Color("#7ec699"))
	t.StatusErr = lipgloss.NewStyle().Foreground(t.Danger)
	t.Selected = lipgloss.NewStyle().Bold(true).Foreground(t.Background).Background(t.Accent)

	return t
}
