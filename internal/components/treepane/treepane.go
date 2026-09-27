// Package treepane is the left sidebar listing containers, queues, or
// tables with their item counts.
package treepane

import (
	"strconv"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Item is one sidebar entry.
type Item struct {
	Name  string
	Count int
	// ShowCount distinguishes "zero items" from "count unknown".
	ShowCount bool
	Dim       bool
	Badge     string
}

// Model is the sidebar.
type Model struct {
	Focused bool

	items         []Item
	cursor        int
	offset        int
	width, height int
	theme         theme.Theme
	title         string
	empty         string
}

// New builds a sidebar.
func New(t theme.Theme, title string) *Model {
	return &Model{theme: t, title: title, empty: "none"}
}

// SetSize resizes the pane.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.clamp() }

// SetEmptyText replaces the placeholder shown when the list is empty.
func (m *Model) SetEmptyText(s string) { m.empty = s }

// SetItems replaces the contents, holding the cursor on the same name
// so a background refresh does not move the user's selection.
func (m *Model) SetItems(items []Item) {
	var focused string
	if m.cursor < len(m.items) {
		focused = m.items[m.cursor].Name
	}
	m.items = items
	m.cursor = 0
	if focused != "" {
		for i, it := range items {
			if it.Name == focused {
				m.cursor = i
				break
			}
		}
	}
	m.clamp()
}

// Items returns the current entries.
func (m *Model) Items() []Item { return m.items }

// Len returns the entry count.
func (m *Model) Len() int { return len(m.items) }

// Current returns the selected entry.
func (m *Model) Current() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return Item{}, false
	}
	return m.items[m.cursor], true
}

// CurrentName returns the selected entry's name, or "".
func (m *Model) CurrentName() string {
	if it, ok := m.Current(); ok {
		return it.Name
	}
	return ""
}

// Select moves the cursor to a named entry.
func (m *Model) Select(name string) bool {
	for i, it := range m.items {
		if it.Name == name {
			m.cursor = i
			m.clamp()
			return true
		}
	}
	return false
}

// Move applies a navigation motion.
func (m *Model) Move(n ui.Nav) {
	switch n {
	case ui.NavUp:
		m.cursor--
	case ui.NavDown:
		m.cursor++
	case ui.NavTop:
		m.cursor = 0
	case ui.NavBottom:
		m.cursor = len(m.items) - 1
	case ui.NavHalfUp:
		m.cursor -= max(m.visible()/2, 1)
	case ui.NavHalfDown:
		m.cursor += max(m.visible()/2, 1)
	default:
		return
	}
	m.clamp()
}

func (m *Model) visible() int { return max(m.height, 0) }

func (m *Model) clamp() {
	m.cursor = ui.Clamp(m.cursor, 0, max(len(m.items)-1, 0))
	v := m.visible()
	if v <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+v {
		m.offset = m.cursor - v + 1
	}
	m.offset = ui.Clamp(m.offset, 0, max(len(m.items)-v, 0))
}

// Title returns the pane heading, with the entry count appended.
func (m *Model) Title() string {
	if len(m.items) == 0 {
		return m.title
	}
	return m.title + " (" + strconv.Itoa(len(m.items)) + ")"
}

// View renders the sidebar.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if len(m.items) == 0 {
		return ui.Fit(m.theme.Muted.Render(ui.Pad(" "+m.empty, m.width)), m.width, m.height)
	}

	var lines []string
	end := min(m.offset+m.visible(), len(m.items))
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.renderItem(i))
	}
	return ui.Fit(strings.Join(lines, "\n"), m.width, m.height)
}

func (m *Model) renderItem(i int) string {
	it := m.items[i]

	// The count is right-aligned in its own gutter so names of varying
	// length still leave the numbers in one column.
	countCell := ""
	if it.ShowCount {
		countCell = strconv.Itoa(it.Count)
	}
	if it.Badge != "" {
		countCell = it.Badge
	}
	countWidth := min(len(countCell), max(m.width-4, 0))

	marker := "  "
	if i == m.cursor {
		marker = "> "
	}
	nameWidth := max(m.width-len(marker)-countWidth-1, 0)
	line := marker + ui.Pad(ui.Truncate(it.Name, nameWidth), nameWidth) + " " + ui.PadLeft(countCell, countWidth)
	line = ui.Pad(line, m.width)

	switch {
	case i == m.cursor && m.Focused:
		return m.theme.Selected.Render(line)
	case i == m.cursor:
		return m.theme.Title.Render(line)
	case it.Dim:
		return m.theme.Muted.Render(line)
	default:
		return line
	}
}
