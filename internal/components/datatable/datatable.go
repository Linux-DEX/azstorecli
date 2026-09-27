// Package datatable is a virtualised table: it renders only the rows
// that fit on screen, so a container with 100,000 blobs costs the same
// to draw as one with ten.
package datatable

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Column describes one table column.
type Column struct {
	Title string
	// Width is the fixed cell width. A Flex column ignores it and
	// absorbs whatever space the fixed columns leave.
	Width int
	Flex  bool
	Right bool
	// MinWidth is the floor a Flex column will not shrink below; the
	// column is dropped entirely rather than rendered narrower.
	MinWidth int
}

// Row is one table row.
type Row struct {
	ID     string // stable identity, used for multi-select
	Cells  []string
	Dim    bool
	Danger bool
	Accent bool
}

// SortSpec is the active sort.
type SortSpec struct {
	Column int
	Desc   bool
}

// Model is the table widget.
type Model struct {
	Columns []Column
	Focused bool
	// Multi enables space-to-select and the leading selection gutter.
	Multi bool

	rows     []Row
	cursor   int
	offset   int
	width    int
	height   int
	selected map[string]bool
	theme    theme.Theme
	sortBy   *SortSpec
	empty    string
}

// New builds a table.
func New(t theme.Theme, columns []Column) *Model {
	return &Model{
		Columns:  columns,
		theme:    t,
		selected: map[string]bool{},
		empty:    "nothing here",
	}
}

// SetEmptyText replaces the placeholder shown when there are no rows.
func (m *Model) SetEmptyText(s string) { m.empty = s }

// SetSize sets the widget's drawing area, including its header row.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetRows replaces the contents, keeping the cursor on the same row ID
// when it still exists. Without that, a background refresh would yank
// the cursor to the top mid-keystroke.
func (m *Model) SetRows(rows []Row) {
	var focusedID string
	if m.cursor < len(m.rows) {
		focusedID = m.rows[m.cursor].ID
	}
	m.rows = rows
	m.applySort()

	m.cursor = 0
	if focusedID != "" {
		for i, r := range m.rows {
			if r.ID == focusedID {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()

	// Drop selections for rows that no longer exist, so a delete-all
	// cannot act on a stale ID.
	if len(m.selected) > 0 {
		live := make(map[string]bool, len(rows))
		for _, r := range rows {
			live[r.ID] = true
		}
		for id := range m.selected {
			if !live[id] {
				delete(m.selected, id)
			}
		}
	}
}

// Rows returns the current rows.
func (m *Model) Rows() []Row { return m.rows }

// Len returns the row count.
func (m *Model) Len() int { return len(m.rows) }

// Cursor returns the cursor index.
func (m *Model) Cursor() int { return m.cursor }

// Current returns the row under the cursor.
func (m *Model) Current() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// Selected returns the IDs of multi-selected rows, falling back to the
// cursor row when nothing is explicitly selected. Every bulk action
// uses this, so "delete" with no selection means "delete this one".
func (m *Model) Selected() []string {
	if len(m.selected) == 0 {
		if r, ok := m.Current(); ok {
			return []string{r.ID}
		}
		return nil
	}
	out := make([]string, 0, len(m.selected))
	for _, r := range m.rows {
		if m.selected[r.ID] {
			out = append(out, r.ID)
		}
	}
	return out
}

// SelectionCount returns the number of explicitly selected rows.
func (m *Model) SelectionCount() int { return len(m.selected) }

// ToggleSelect flips the cursor row's selection and advances, so a run
// of rows can be selected by holding space.
func (m *Model) ToggleSelect() {
	r, ok := m.Current()
	if !ok || !m.Multi {
		return
	}
	if m.selected[r.ID] {
		delete(m.selected, r.ID)
	} else {
		m.selected[r.ID] = true
	}
	m.Move(ui.NavDown)
}

// SelectAll selects every row.
func (m *Model) SelectAll() {
	if !m.Multi {
		return
	}
	for _, r := range m.rows {
		m.selected[r.ID] = true
	}
}

// ClearSelection drops every selection.
func (m *Model) ClearSelection() { m.selected = map[string]bool{} }

// SetSort sorts by a column. Selecting the active column flips the
// direction, which is what a second press of `o` should do.
func (m *Model) SetSort(column int) {
	if m.sortBy != nil && m.sortBy.Column == column {
		m.sortBy.Desc = !m.sortBy.Desc
	} else {
		m.sortBy = &SortSpec{Column: column}
	}
	m.applySort()
}

// Sort returns the active sort, if any.
func (m *Model) Sort() *SortSpec { return m.sortBy }

func (m *Model) applySort() {
	if m.sortBy == nil || m.sortBy.Column >= len(m.Columns) {
		return
	}
	col := m.sortBy.Column
	// Stable so rows comparing equal keep their server order, which for
	// blobs means they stay alphabetical within a size group.
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := cellAt(m.rows[i], col), cellAt(m.rows[j], col)
		if m.sortBy.Desc {
			return a > b
		}
		return a < b
	})
}

func cellAt(r Row, i int) string {
	if i < len(r.Cells) {
		return r.Cells[i]
	}
	return ""
}

// Move applies a navigation motion.
func (m *Model) Move(n ui.Nav) {
	page := m.visibleRows() / 2
	if page < 1 {
		page = 1
	}
	switch n {
	case ui.NavUp:
		m.cursor--
	case ui.NavDown:
		m.cursor++
	case ui.NavTop:
		m.cursor = 0
	case ui.NavBottom:
		m.cursor = len(m.rows) - 1
	case ui.NavHalfUp:
		m.cursor -= page
	case ui.NavHalfDown:
		m.cursor += page
	default:
		return
	}
	m.clampCursor()
}

// SetCursorByID moves the cursor to a row by ID, for deep links.
func (m *Model) SetCursorByID(id string) bool {
	for i, r := range m.rows {
		if r.ID == id {
			m.cursor = i
			m.clampCursor()
			return true
		}
	}
	return false
}

func (m *Model) clampCursor() {
	m.cursor = ui.Clamp(m.cursor, 0, max(len(m.rows)-1, 0))

	visible := m.visibleRows()
	if visible <= 0 {
		m.offset = 0
		return
	}
	// Keep the cursor inside the window, scrolling by the minimum
	// needed so the view does not jump when stepping one row.
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = ui.Clamp(m.offset, 0, max(len(m.rows)-visible, 0))
}

// visibleRows is the height minus the header line.
func (m *Model) visibleRows() int { return max(m.height-1, 0) }

// View renders the table.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	widths := m.layout()

	var b strings.Builder
	b.WriteString(m.theme.Header.Render(m.headerLine(widths)))

	visible := m.visibleRows()
	if len(m.rows) == 0 {
		b.WriteString("\n" + m.theme.Muted.Render(ui.Pad("  "+m.empty, m.width)))
		return ui.Fit(b.String(), m.width, m.height)
	}

	end := min(m.offset+visible, len(m.rows))
	for i := m.offset; i < end; i++ {
		b.WriteString("\n" + m.renderRow(i, widths))
	}
	return ui.Fit(b.String(), m.width, m.height)
}

// layout distributes the available width across the columns, dropping
// trailing columns that cannot meet their minimum rather than squeezing
// every column into illegibility.
func (m *Model) layout() []int {
	gutter := 0
	if m.Multi {
		gutter = 2
	}
	avail := m.width - gutter - (len(m.Columns) - 1) // one space between columns
	if avail < 0 {
		avail = 0
	}

	widths := make([]int, len(m.Columns))
	fixed, flexCount := 0, 0
	for i, c := range m.Columns {
		if c.Flex {
			flexCount++
			continue
		}
		widths[i] = c.Width
		fixed += c.Width
	}

	remaining := avail - fixed
	if flexCount > 0 {
		per := remaining / flexCount
		extra := remaining % flexCount
		for i, c := range m.Columns {
			if !c.Flex {
				continue
			}
			widths[i] = per
			if extra > 0 {
				widths[i]++
				extra--
			}
		}
	}

	// Trim from the right until everything fits.
	total := gutter + len(m.Columns) - 1
	for _, w := range widths {
		total += w
	}
	for i := len(widths) - 1; i >= 0 && total > m.width; i-- {
		floor := m.Columns[i].MinWidth
		shrink := min(widths[i]-floor, total-m.width)
		if shrink > 0 {
			widths[i] -= shrink
			total -= shrink
		}
	}
	for i := range widths {
		widths[i] = max(widths[i], 0)
	}
	return widths
}

func (m *Model) headerLine(widths []int) string {
	var parts []string
	if m.Multi {
		parts = append(parts, "  ")
	}
	for i, c := range m.Columns {
		if widths[i] == 0 {
			continue
		}
		title := c.Title
		if m.sortBy != nil && m.sortBy.Column == i {
			arrow := " ↑"
			if m.sortBy.Desc {
				arrow = " ↓"
			}
			title += arrow
		}
		if c.Right {
			parts = append(parts, ui.PadLeft(title, widths[i]))
			continue
		}
		parts = append(parts, ui.Pad(title, widths[i]))
	}
	return ui.Pad(strings.Join(parts, " "), m.width)
}

func (m *Model) renderRow(idx int, widths []int) string {
	row := m.rows[idx]

	var parts []string
	if m.Multi {
		mark := "  "
		if m.selected[row.ID] {
			mark = "▸ "
		}
		parts = append(parts, mark)
	}
	for i, c := range m.Columns {
		if widths[i] == 0 {
			continue
		}
		cell := cellAt(row, i)
		if c.Right {
			parts = append(parts, ui.PadLeft(cell, widths[i]))
			continue
		}
		// Names are more identifiable by their tail, so a long blob
		// name loses its middle rather than its extension.
		if c.Flex {
			parts = append(parts, ui.Pad(ui.TruncateLeft(cell, widths[i]), widths[i]))
			continue
		}
		parts = append(parts, ui.Pad(cell, widths[i]))
	}
	line := ui.Pad(strings.Join(parts, " "), m.width)

	switch {
	case idx == m.cursor && m.Focused:
		return m.theme.Selected.Render(line)
	case idx == m.cursor:
		return lipgloss.NewStyle().Bold(true).Render(line)
	case row.Danger:
		return m.theme.StatusErr.Render(line)
	case row.Accent:
		return m.theme.Title.Render(line)
	case row.Dim:
		return m.theme.Muted.Render(line)
	default:
		return line
	}
}

// ScrollInfo renders "12/128" for a pane title.
func (m *Model) ScrollInfo() string {
	if len(m.rows) == 0 {
		return ""
	}
	return strconv.Itoa(m.cursor+1) + "/" + strconv.Itoa(len(m.rows))
}
