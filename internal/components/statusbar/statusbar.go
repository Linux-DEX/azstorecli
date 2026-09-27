// Package statusbar is the top chrome: service dots, project, ports.
package statusbar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Model is the status bar.
type Model struct {
	theme    theme.Theme
	project  string
	profile  config.Profile
	statuses []supervisor.Status
	toast    string
	warn     bool
	ports    string
	width    int
}

// New builds a status bar.
func New(t theme.Theme, project string) Model {
	return Model{theme: t, project: project}
}

// SetSize sets the bar width.
func (m *Model) SetSize(w int) { m.width = w }

// SetServices replaces the service list.
func (m *Model) SetServices(ss []supervisor.Status) { m.statuses = ss }

// SetService updates one service's state.
func (m *Model) SetService(name string, state supervisor.State, pid int) {
	for i := range m.statuses {
		if m.statuses[i].Name == name {
			m.statuses[i].State = state.String()
			m.statuses[i].PID = pid
			return
		}
	}
}

// SetProfile updates the active profile indicator.
func (m *Model) SetProfile(p config.Profile) { m.profile = p }

// SetPorts sets the right-hand port summary.
func (m *Model) SetPorts(s string) { m.ports = s }

// SetToast shows a transient message.
func (m *Model) SetToast(text string, warn bool) { m.toast, m.warn = text, warn }

// ClearToast drops the transient message.
func (m *Model) ClearToast() { m.toast = "" }

// View renders the bar.
func (m Model) View() string {
	var left strings.Builder
	left.WriteString(m.theme.Title.Render(" azstorecli "))
	if m.project != "" {
		left.WriteString(m.theme.Muted.Render("─ " + m.project + " "))
	}

	var mid strings.Builder
	for i, st := range m.statuses {
		if i > 0 {
			mid.WriteString("  ")
		}
		glyph := theme.StateGlyph(st.State)
		style := m.theme.StateColor(st.State)
		mid.WriteString(style.Render(glyph + " " + title(st.Name)))
	}
	if m.profile.IsReadOnly() {
		mid.WriteString("  " + m.theme.StatusWarn.Render("RO "+m.profile.Name))
	} else if m.profile.Name != "" && !m.profile.IsEmulator() {
		mid.WriteString("  " + m.theme.StatusWarn.Render(m.profile.Name))
	}

	right := m.theme.Muted.Render(m.ports)
	if m.toast != "" {
		style := m.theme.StatusOK
		if m.warn {
			style = m.theme.StatusErr
		}
		right = style.Render(m.toast)
	}

	gap := m.width - uiWidth(left.String()) - uiWidth(mid.String()) - uiWidth(right) - 4
	if gap < 1 {
		gap = 1
	}
	line := left.String() + mid.String() + strings.Repeat(" ", gap) + right
	if m.profile.IsReadOnly() && !m.profile.IsEmulator() {
		return m.theme.StatusWarn.Render(ui.Pad(strip(line), m.width))
	}
	return ui.Pad(line, m.width)
}

func title(name string) string {
	switch name {
	case "azurite":
		return "Azurite"
	case "functions":
		return "Functions"
	default:
		return name
	}
}

func uiWidth(s string) int { return lipgloss.Width(s) }

func strip(s string) string { return s }
