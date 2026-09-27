// Package theme holds the lipgloss style set used across every screen.
// Screens pull colours and styles from a Theme rather than hardcoding
// lipgloss.Color values, so switching themes actually re-skins the app.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the colour half of a theme — the part a YAML file defines.
type Palette struct {
	Name       string `yaml:"name"`
	Background string `yaml:"background"`
	Foreground string `yaml:"foreground"`
	Muted      string `yaml:"muted"`
	Accent     string `yaml:"accent"`
	Border     string `yaml:"border"`
	Danger     string `yaml:"danger"`
	Warning    string `yaml:"warning"`
	Success    string `yaml:"success"`
	Highlight  string `yaml:"highlight"`
}

// Theme is the full style set one screen needs to render consistently
// with the rest of the app.
type Theme struct {
	Palette

	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Muted      lipgloss.Style
	Body       lipgloss.Style
	HelpBar    lipgloss.Style
	StatusBar  lipgloss.Style
	StatusOK   lipgloss.Style
	StatusWarn lipgloss.Style
	StatusErr  lipgloss.Style
	Selected   lipgloss.Style
	Header     lipgloss.Style
	Pane       lipgloss.Style
	PaneActive lipgloss.Style
	Modal      lipgloss.Style
	Danger     lipgloss.Style
	Badge      lipgloss.Style
	Key        lipgloss.Style
}

// Build derives every style from a palette. Every theme goes through
// here, so a new style is defined once and every theme gets it.
func Build(p Palette) Theme {
	fill := func(value, fallback string) lipgloss.Color {
		if value == "" {
			return lipgloss.Color(fallback)
		}
		return lipgloss.Color(value)
	}

	accent := fill(p.Accent, "#5fb3ff")
	muted := fill(p.Muted, "#8a8a8a")
	border := fill(p.Border, "#3a3a3a")
	danger := fill(p.Danger, "#ff6b6b")
	warning := fill(p.Warning, "#e2b93d")
	success := fill(p.Success, "#7ec699")
	fg := fill(p.Foreground, "#e6e6e6")
	bg := fill(p.Background, "#0d0d0d")

	t := Theme{Palette: p}
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(accent)
	t.Subtitle = lipgloss.NewStyle().Foreground(fg).Bold(true)
	t.Muted = lipgloss.NewStyle().Foreground(muted)
	t.Body = lipgloss.NewStyle().Foreground(fg)
	t.HelpBar = lipgloss.NewStyle().Foreground(muted)
	t.StatusBar = lipgloss.NewStyle().Foreground(fg)
	t.StatusOK = lipgloss.NewStyle().Foreground(success)
	t.StatusWarn = lipgloss.NewStyle().Foreground(warning)
	t.StatusErr = lipgloss.NewStyle().Foreground(danger)
	t.Danger = lipgloss.NewStyle().Foreground(danger).Bold(true)
	t.Selected = lipgloss.NewStyle().Bold(true).Foreground(bg).Background(accent)
	t.Header = lipgloss.NewStyle().Bold(true).Foreground(muted)
	t.Pane = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border)
	t.PaneActive = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)
	t.Modal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2)
	t.Badge = lipgloss.NewStyle().Foreground(bg).Background(muted).Padding(0, 1)
	t.Key = lipgloss.NewStyle().Bold(true).Foreground(accent)
	return t
}

// Dark is the hardcoded fallback, so the app never boots unstyled even
// if the embedded themes fail to parse.
func Dark() Theme {
	return Build(Palette{
		Name:       "dark",
		Background: "#0d0d0d",
		Foreground: "#e6e6e6",
		Muted:      "#8a8a8a",
		Accent:     "#5fb3ff",
		Border:     "#3a3a3a",
		Danger:     "#ff6b6b",
		Warning:    "#e2b93d",
		Success:    "#7ec699",
		Highlight:  "#2a3f5f",
	})
}

// StateColor maps a service state name to its indicator style, so the
// dashboard and the status bar can never disagree about what green means.
func (t Theme) StateColor(state string) lipgloss.Style {
	switch state {
	case "healthy":
		return t.StatusOK
	case "starting":
		return t.StatusWarn
	case "unhealthy", "crashed":
		return t.StatusErr
	default:
		return t.Muted
	}
}

// StateGlyph is the dot shown next to a service name.
func StateGlyph(state string) string {
	switch state {
	case "healthy":
		return "●"
	case "starting":
		return "◐"
	case "unhealthy", "crashed":
		return "✖"
	default:
		return "⬡"
	}
}
