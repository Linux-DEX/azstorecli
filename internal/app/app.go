// Package app holds the Bubble Tea root model — the only package that
// knows about every screen. It owns global keybindings, the help bar,
// and dispatching messages to whichever screen is focused via router.go.
package app

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/screens/dashboard"
)

// RootModel is the top-level tea.Model. It is intentionally thin: it
// holds global chrome (help bar, status line) and delegates everything
// else to the focused screen via the router.
type RootModel struct {
	deps   Deps
	router *router
	help   help.Model

	width, height int
	statusErr     error
}

// New builds the root model with every M1 screen registered. Later
// milestones add more screens.register(...) calls here as they land —
// this is the single place that assembles the app.
func New(cfg config.Config) RootModel {
	deps := NewDeps(cfg)

	r := newRouter()
	r.register(ScreenDashboard, dashboard.New(dashboard.Deps{
		Theme: deps.Theme,
		Keys:  deps.Keys,
	}))
	// r.register(ScreenBlob, blob.New(...))            // M3
	// r.register(ScreenQueue, queuescreen.New(...))    // M4
	// r.register(ScreenTable, tablescreen.New(...))    // M4
	// r.register(ScreenFunctions, functions.New(...))  // M5
	// r.register(ScreenLogs, logs.New(...))             // M2
	// r.register(ScreenSnapshots, snapshots.New(...))  // M6
	// r.register(ScreenProfiles, profiles.New(...))    // M7
	// r.register(ScreenSettings, settings.New(...))    // M7

	r.focus(ScreenDashboard)

	return RootModel{
		deps:   deps,
		router: r,
		help:   help.New(),
	}
}

func (m RootModel) Init() tea.Cmd {
	return nil
}

func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.deps.Keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.deps.Keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		case key.Matches(msg, m.deps.Keys.NextScreen):
			m.router.next()
			return m, nil
		case key.Matches(msg, m.deps.Keys.PrevScreen):
			m.router.prev()
			return m, nil
			// CommandPalette (":") arrives in M7 with the palette component.
		}

	case SwitchScreenMsg:
		m.router.focus(msg.Target)
		return m, nil

	case ErrorMsg:
		m.statusErr = msg.Err
		return m, nil
	}

	cmd := m.router.updateCurrent(msg)
	return m, cmd
}

func (m RootModel) View() string {
	body := ""
	if current := m.router.current(); current != nil {
		body = current.View()
	}

	helpView := m.deps.Theme.HelpBar.Render(m.help.View(m.deps.Keys))

	status := ""
	if m.statusErr != nil {
		status = m.deps.Theme.StatusErr.Render(m.statusErr.Error())
	}

	return lipgloss.JoinVertical(lipgloss.Left, body, status, helpView)
}
