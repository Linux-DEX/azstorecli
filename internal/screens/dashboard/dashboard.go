// Package dashboard is the first screen the TUI shows. In M1 it is a
// placeholder that proves screen routing, theming, and the help bar work
// end to end. M2 replaces the body with live supervisor status tiles for
// Azurite and the Functions host.
package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

// Deps is the slice of app.Deps this screen actually needs. Screens
// declare their own narrow Deps type rather than importing app.Deps
// directly, keeping internal/app the only package that assembles the
// full dependency graph.
type Deps struct {
	Theme theme.Theme
	Keys  keymap.Global
}

// Model is the dashboard's tea.Model.
type Model struct {
	deps Deps
}

// New builds the dashboard screen.
func New(deps Deps) Model {
	return Model{deps: deps}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// No screen-local key handling yet — M2 adds "s" to start the stack
	// and per-service focus (see docs/KEYBINDINGS.md, Screen 1).
	return m, nil
}

func (m Model) View() string {
	title := m.deps.Theme.Title.Render("azstorecli — Dashboard")
	body := "Supervisor not wired up yet (arrives in M2).\n" +
		"This screen currently only proves that routing, theming,\n" +
		"and the help bar work end to end."
	return title + "\n\n" + body + "\n"
}
