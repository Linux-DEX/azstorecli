// Package modal is the confirm / input / notice overlay.
//
// Destructive confirms never land the cursor on the destructive option.
package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Model is one modal.
type Model struct {
	kind        msg.ModalKind
	title       string
	body        string
	prompt      string
	action      string
	destructive bool
	onCancel    bool // true = Cancel focused (the safe default)
	input       textinput.Model
	theme       theme.Theme
	width       int
}

// New builds a modal from an OpenModal request.
func New(t theme.Theme, req msg.OpenModal) *Model {
	ti := textinput.New()
	ti.Placeholder = req.Placeholder
	ti.SetValue(req.Value)
	ti.CharLimit = 256
	ti.Width = 48
	if req.Kind == msg.ModalInput {
		ti.Focus()
	}
	return &Model{
		kind:        req.Kind,
		title:       req.Title,
		body:        req.Body,
		prompt:      req.Prompt,
		action:      req.Action,
		destructive: req.Destructive,
		onCancel:    true,
		input:       ti,
		theme:       t,
	}
}

// Action is the callback ID this modal will report.
func (m *Model) Action() string { return m.action }

func (m *Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) Update(msg_ tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg_.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return m, result(m.action, false, "")
		case "y":
			if m.kind == msg.ModalConfirm {
				return m, result(m.action, true, "")
			}
		case "n":
			if m.kind == msg.ModalConfirm {
				return m, result(m.action, false, "")
			}
		case "left", "h", "right", "l", "tab", "shift+tab":
			if m.kind == msg.ModalConfirm {
				m.onCancel = !m.onCancel
				return m, nil
			}
		case "enter":
			switch m.kind {
			case msg.ModalConfirm:
				return m, result(m.action, !m.onCancel, "")
			case msg.ModalInput:
				return m, result(m.action, true, strings.TrimSpace(m.input.Value()))
			default:
				return m, result(m.action, true, "")
			}
		}
	}
	if m.kind == msg.ModalInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg_)
		return m, cmd
	}
	return m, nil
}

func result(action string, ok bool, value string) tea.Cmd {
	return func() tea.Msg {
		return msg.ModalResult{Action: action, OK: ok, Value: value}
	}
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render(m.title))
	b.WriteString("\n\n")
	if m.body != "" {
		b.WriteString(m.body)
		b.WriteString("\n\n")
	}
	switch m.kind {
	case msg.ModalInput:
		if m.prompt != "" {
			b.WriteString(m.theme.Muted.Render(m.prompt) + "\n")
		}
		b.WriteString(m.input.View())
	case msg.ModalConfirm:
		cancel := "[ Cancel ]"
		ok := "[ Confirm ]"
		if m.destructive {
			ok = "[ Delete ]"
		}
		if m.onCancel {
			cancel = m.theme.Selected.Render(cancel)
			ok = m.theme.Muted.Render(ok)
		} else {
			style := m.theme.Selected
			if m.destructive {
				style = m.theme.Danger
			}
			cancel = m.theme.Muted.Render(cancel)
			ok = style.Render(ok)
		}
		b.WriteString("       " + cancel + "    " + ok)
	}

	box := m.theme.Modal.Width(min(m.width-4, 62)).Render(b.String())
	return box
}

// Place centers the modal in a w×h frame.
func Place(frame string, w, h int, t theme.Theme) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, frame)
}

// Overlay draws the modal on top of the current view.
func Overlay(base, modalView string, w, h int) string {
	if w <= 0 || h <= 0 {
		return modalView
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, modalView,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// Dim is unused — kept so callers can pass a theme if they later want
// a dimmed backdrop. The overlay itself already covers the frame.
func Dim(_ theme.Theme, s string) string { return ui.Truncate(s, len(s)) }
