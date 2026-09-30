// Package helpoverlay is the ? keybinding dialog drawn over the screen.
package helpoverlay

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Model is the keybinding dialog.
type Model struct {
	open   bool
	offset int
	theme  theme.Theme
	keys   *keymap.KeyMap
}

// New builds a closed dialog.
func New(t theme.Theme, keys *keymap.KeyMap) *Model {
	return &Model{theme: t, keys: keys}
}

// Opened reports whether the dialog is capturing input.
func (m *Model) Opened() bool { return m.open }

// Toggle opens or closes the dialog.
func (m *Model) Toggle() {
	m.open = !m.open
	m.offset = 0
}

// Close hides the dialog.
func (m *Model) Close() { m.open = false }

// Update scrolls or closes. The caller only forwards keys while Opened.
func (m *Model) Update(k tea.KeyMsg) {
	if !m.open {
		return
	}
	switch {
	case k.String() == "esc" || m.keys.Matches(k, "app.help"):
		m.Close()
	case m.keys.Matches(k, "nav.down"):
		m.offset++
	case m.keys.Matches(k, "nav.up"):
		m.offset--
	case m.keys.Matches(k, "nav.half_down"):
		m.offset += 8
	case m.keys.Matches(k, "nav.half_up"):
		m.offset -= 8
	case m.keys.Matches(k, "nav.top"):
		m.offset = 0
	case m.keys.Matches(k, "nav.bottom"):
		m.offset = 1 << 20
	}
}

// View renders the dialog for the focused screen scope.
func (m *Model) View(scope string, termW, termH int) string {
	if !m.open || termW < 1 || termH < 1 {
		return ""
	}
	style := m.theme.Modal
	fx, fy := style.GetFrameSize()

	boxW := termW - 4
	if boxW > 140 {
		boxW = 140
	}
	if boxW < fx+20 {
		boxW = termW
	}
	contentW := boxW - fx
	if contentW < 16 {
		contentW = 16
	}

	boxH := termH - 4
	if boxH > 42 {
		boxH = 42
	}
	if boxH < fy+6 {
		boxH = termH
	}
	contentH := boxH - fy
	window := contentH - 4
	if window < 1 {
		window = 1
	}

	lines := m.lines(scope, contentW)
	maxOff := len(lines) - window
	if maxOff < 0 {
		maxOff = 0
	}
	if m.offset > maxOff {
		m.offset = maxOff
	}
	if m.offset < 0 {
		m.offset = 0
	}
	visible := lines[m.offset:min(m.offset+window, len(lines))]

	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Keys"))
	b.WriteString("\n\n")
	for i, line := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	hint := m.theme.Key.Render("esc") + " " + m.theme.Muted.Render("close")
	if maxOff > 0 {
		hint = m.theme.Muted.Render("j/k scroll") + "  " + hint
	}
	b.WriteString("\n\n" + hint)
	return style.Width(contentW).Render(b.String())
}

func (m *Model) lines(scope string, contentW int) []string {
	local, global := split(m.keys, scope)
	sep := ""
	colW := contentW
	keyW := maxKeyWidth(m.keys, append(append([]keymap.Action{}, local...), global...))
	if contentW >= 64 {
		sep = " " + m.theme.Muted.Render("│") + " "
		colW = (contentW - lipgloss.Width(sep)) / 2
	}
	var out []string
	if len(local) > 0 {
		label := scope
		if label == "" {
			label = "this screen"
		}
		out = append(out, m.theme.Header.Render(label))
		out = append(out, formatEntries(m.theme, m.keys, local, keyW, colW, sep)...)
		out = append(out, "")
	}
	out = append(out, m.theme.Header.Render("global"))
	out = append(out, formatEntries(m.theme, m.keys, global, keyW, colW, sep)...)
	return out
}

func split(km *keymap.KeyMap, scope string) (local, global []keymap.Action) {
	for _, a := range keymap.ActionsFor(scope) {
		if len(km.KeysFor(a.ID)) == 0 {
			continue
		}
		if a.Scope == keymap.ScopeGlobal {
			global = append(global, a)
		} else {
			local = append(local, a)
		}
	}
	return local, global
}

func maxKeyWidth(km *keymap.KeyMap, actions []keymap.Action) int {
	w := 1
	for _, a := range actions {
		if n := lipgloss.Width(km.Get(a.ID).Help().Key); n > w {
			w = n
		}
	}
	if w > 14 {
		return 14
	}
	return w
}

func formatEntries(t theme.Theme, km *keymap.KeyMap, actions []keymap.Action, keyW, colW int, sep string) []string {
	const gap = 2
	descW := colW - keyW - gap
	if descW < 1 {
		descW = 1
	}
	entries := make([]string, 0, len(actions))
	for _, a := range actions {
		k := km.Get(a.ID).Help().Key
		if k == "" {
			continue
		}
		cell := t.Key.Render(ui.Pad(ui.Truncate(k, keyW), keyW)) + "  " + t.Muted.Render(ui.Truncate(a.Desc, descW))
		if d := colW - lipgloss.Width(cell); d > 0 {
			cell += strings.Repeat(" ", d)
		}
		entries = append(entries, cell)
	}
	if sep == "" {
		return entries
	}
	out := make([]string, 0, (len(entries)+1)/2)
	for i := 0; i < len(entries); i += 2 {
		if i+1 >= len(entries) {
			out = append(out, entries[i])
			break
		}
		out = append(out, entries[i]+sep+entries[i+1])
	}
	return out
}

// Place draws fg centered on top of bg inside a w×h frame.
func Place(bg, fg string, w, h int) string {
	if w <= 0 || h <= 0 {
		return fg
	}
	bgLines := fitLines(bg, w, h)
	if fg == "" {
		return strings.Join(bgLines, "\n")
	}
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		if n := lipgloss.Width(l); n > fgW {
			fgW = n
		}
	}
	if fgW > w {
		fgW = w
	}
	for i, l := range fgLines {
		switch n := lipgloss.Width(l); {
		case n > fgW:
			fgLines[i] = ansi.Cut(l, 0, fgW)
		case n < fgW:
			fgLines[i] = l + strings.Repeat(" ", fgW-n)
		}
	}
	if len(fgLines) > h {
		fgLines = fgLines[:h]
	}
	fgH := len(fgLines)
	x := (w - fgW) / 2
	y := (h - fgH) / 2

	var b strings.Builder
	for i := 0; i < h; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := bgLines[i]
		if i < y || i >= y+fgH {
			b.WriteString(line)
			continue
		}
		left := ansi.Cut(line, 0, x)
		if d := x - lipgloss.Width(left); d > 0 {
			left += strings.Repeat(" ", d)
		}
		right := ""
		if lipgloss.Width(line) > x+fgW {
			right = ansi.Cut(line, x+fgW, lipgloss.Width(line))
		}
		row := left + fgLines[i-y] + right
		if d := w - lipgloss.Width(row); d > 0 {
			row += strings.Repeat(" ", d)
		}
		b.WriteString(row)
	}
	return b.String()
}

func fitLines(s string, w, h int) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, h)
	blank := strings.Repeat(" ", w)
	for i := 0; i < h; i++ {
		if i >= len(raw) {
			out[i] = blank
			continue
		}
		line := raw[i]
		switch n := lipgloss.Width(line); {
		case n > w:
			out[i] = ansi.Cut(line, 0, w)
		case n < w:
			out[i] = line + strings.Repeat(" ", w-n)
		default:
			out[i] = line
		}
	}
	return out
}
