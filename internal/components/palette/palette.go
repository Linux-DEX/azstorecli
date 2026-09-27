// Package palette is the ':' command palette.
package palette

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Model is the palette overlay.
type Model struct {
	input    textinput.Model
	items    []keymap.Action
	filtered []keymap.Action
	cursor   int
	focused  bool
	theme    theme.Theme
	keys     *keymap.KeyMap
	width    int
	scope    string
}

// New builds a closed palette.
func New(t theme.Theme, keys *keymap.KeyMap) *Model {
	ti := textinput.New()
	ti.Placeholder = "type an action…"
	ti.CharLimit = 80
	ti.Width = 40
	return &Model{input: ti, theme: t, keys: keys}
}

// Focused reports whether the palette is capturing input.
func (m *Model) Focused() bool { return m.focused }

// Open shows the palette for a screen scope.
func (m *Model) Open(scope string) {
	m.scope = scope
	m.items = keymap.ActionsFor(scope)
	m.focused = true
	m.input.SetValue("")
	m.input.Focus()
	m.filter()
}

// Close hides the palette.
func (m *Model) Close() {
	m.focused = false
	m.input.Blur()
	m.input.SetValue("")
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg_ tea.Msg) (tea.Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	if key, ok := msg_.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.Close()
			return m, nil
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				id := m.filtered[m.cursor].ID
				m.Close()
				return m, func() tea.Msg { return msg.Action{ID: id} }
			}
			m.Close()
			return m, nil
		case "up", "ctrl+p":
			m.cursor--
			m.clamp()
			return m, nil
		case "down", "ctrl+n":
			m.cursor++
			m.clamp()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg_)
	m.filter()
	return m, cmd
}

func (m *Model) filter() {
	q := strings.TrimSpace(m.input.Value())
	m.filtered = m.filtered[:0]
	for _, a := range m.items {
		if q == "" || match(q, a.ID) || match(q, a.Title) || match(q, a.Desc) {
			m.filtered = append(m.filtered, a)
		}
	}
	m.clamp()
}

func match(q, target string) bool {
	q, target = strings.ToLower(q), strings.ToLower(target)
	if strings.Contains(target, q) {
		return true
	}
	i := 0
	for _, r := range target {
		if i < len(q) && r == rune(q[i]) {
			i++
		}
	}
	return i == len(q)
}

func (m *Model) clamp() {
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = ui.Clamp(m.cursor, 0, len(m.filtered)-1)
}

func (m *Model) View() string {
	if !m.focused {
		return ""
	}
	w := min(m.width-4, 68)
	if w < 40 {
		w = 40
	}

	var b strings.Builder
	b.WriteString(": " + m.input.View() + "\n")
	b.WriteString(strings.Repeat("─", w) + "\n")

	limit := min(10, len(m.filtered))
	if limit == 0 {
		b.WriteString(m.theme.Muted.Render("  no matching actions"))
	}
	for i := 0; i < limit; i++ {
		a := m.filtered[i]
		shortcut := ""
		if m.keys != nil {
			shortcut = strings.Join(m.keys.KeysFor(a.ID), ", ")
		}
		line := ui.Pad(a.Title, w-len(shortcut)-4) + "  " + shortcut
		if i == m.cursor {
			b.WriteString(m.theme.Selected.Render(ui.Pad("> "+line, w)))
		} else {
			b.WriteString("  " + ui.Truncate(line, w-2))
		}
		if i < limit-1 {
			b.WriteString("\n")
		}
	}
	return m.theme.Modal.Width(w).Render(b.String())
}

// SetWidth sets the drawing width.
func (m *Model) SetWidth(w int) { m.width = w }
