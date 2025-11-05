package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/pkg/storage"
)

// --- Model ---

type model struct {
	viewport viewport.Model
	logs     string
	width    int
	height   int
	quitting bool
	logChan  <-chan string
}

type tickMsg time.Time
type logMsg string

// --- Program entrypoint ---

func RunApp() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run UI: %w", err)
	}
	return nil
}

// --- Initialization ---
func initialModel() model {
	vp := viewport.New(100, 30)
	vp.MouseWheelEnabled = true // Enable mouse wheel scrolling
	vp.SetContent("Starting Azurite...\n")
	logChan, _ := storage.StartAzurite()
	return model{viewport: vp, logChan: logChan}
}

// --- Cmd to read from log channel ---

func waitForLogLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil // channel closed
		}
		return logMsg(line)
	}
}

// --- Bubble Tea lifecycle ---

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		waitForLogLine(m.logChan),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			storage.StopAzurite()
			return m, tea.Quit
		case "r":
			m.logs = "Re-attaching Azurite logs...\n"
			m.viewport.SetContent(m.logs)
			id := storage.GetAzuriteContainerID()
			newChan, _ := storage.AttachLogs(id)
			m.logChan = newChan
			return m, waitForLogLine(m.logChan)
		case "up", "k":
			m.viewport.ScrollUp(1)
			return m, nil
		case "down", "j":
			m.viewport.ScrollDown(1)
			return m, nil
		case "pgup":
			m.viewport.PageUp()
			return m, nil
		case "pgdown":
			m.viewport.PageDown()
			return m, nil
		case "ctrl+d":
			m.viewport.HalfPageDown()
			return m, nil
		case "ctrl+u":
			m.viewport.HalfPageUp()
			return m, nil
		}

	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })

	case logMsg:
		m.logs += string(msg)
		m.viewport.SetContent(m.logs)
		// Auto-scroll to bottom on new logs:
		m.viewport.GotoBottom()
		return m, waitForLogLine(m.logChan)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 3
	}
	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return "Exiting..."
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("AZURE STORAGE LOCAL (Azurite Logs)")
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("[Q] Quit | [R] Reattach Logs")
	content := m.viewport.View()
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}
