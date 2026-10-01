// Package datatable is a virtualised table: it renders only the rows
// that fit on screen, so a container with 100,000 blobs costs the same
// to draw as one with ten.
package datatable

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// defaultMaxWidth is the widest a non-name column grows to fit its text.
// Past this the cell wraps. The name column uses half the pane instead.
const defaultMaxWidth = 40

// Column describes one table column.
type Column struct {
	Title string
	// Width is a minimum. The column grows to fit its cells up to MaxWidth.
	Width int
	// MaxWidth caps the column. Zero uses half the pane for Flex (the name)
	// and defaultMaxWidth for the rest. Longer text wraps.
	MaxWidth int
	// Flex is the name column. It may grow to half the pane, and it shrinks
	// first when the other columns need the rest. It does not absorb leftover
	// space, so a short name stays short.
	Flex  bool
	Right bool
	// MinWidth is the floor a Flex column will not shrink below.
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
	if visible <= 0 || len(m.rows) == 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	// Wrapped rows are taller than one line, so scroll until the cursor
	// row actually fits in the window.
	widths := m.layout()
	for m.offset < m.cursor && m.span(m.offset, m.cursor, widths) > visible {
		m.offset++
	}
	m.offset = ui.Clamp(m.offset, 0, max(len(m.rows)-1, 0))
}

func (m *Model) span(from, to int, widths []int) int {
	n := 0
	for i := from; i <= to && i < len(m.rows); i++ {
		n += m.rowHeight(i, widths)
	}
	return n
}

func (m *Model) rowHeight(i int, widths []int) int {
	h := 1
	for col := range m.Columns {
		if widths[col] == 0 {
			continue
		}
		if n := len(wrapText(cellAt(m.rows[i], col), widths[col])); n > h {
			h = n
		}
	}
	return h
}

// visibleRows is the height minus the header line.
func (m *Model) visibleRows() int { return max(m.height-1, 0) }

// View renders the table.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	widths := m.layout()

	lines := []string{m.theme.Header.Render(m.headerLine(widths))}
	if len(m.rows) == 0 {
		lines = append(lines, m.theme.Muted.Render(ui.Pad("  "+m.empty, m.width)))
		return ui.Fit(strings.Join(lines, "\n"), m.width, m.height)
	}

	for i := m.offset; i < len(m.rows); i++ {
		rowLines := m.renderRow(i, widths)
		if len(lines)+len(rowLines) > m.height && len(lines) > 1 {
			break
		}
		lines = append(lines, rowLines...)
	}
	return ui.Fit(strings.Join(lines, "\n"), m.width, m.height)
}

// layout sizes each column to its header and cells, capped at MaxWidth.
// Leftover pane width stays empty on the right. When the columns still
// do not fit, Flex columns shrink first and their text wraps.
func (m *Model) layout() []int {
	n := len(m.Columns)
	widths := make([]int, n)
	for i, c := range m.Columns {
		maxW := m.columnCap(c)
		w := runewidth.StringWidth(c.Title)
		if c.Width > w {
			w = c.Width
		}
		for _, row := range m.rows {
			if cw := runewidth.StringWidth(cellAt(row, i)); cw > w {
				w = cw
			}
		}
		if w > maxW {
			w = maxW
		}
		if w < 1 {
			w = 1
		}
		widths[i] = w
	}

	gaps := 0
	if m.Multi {
		gaps += 2
	}
	if n > 1 {
		gaps += n - 1
	}
	total := gaps
	for _, w := range widths {
		total += w
	}
	for total > m.width && m.width > 0 {
		idx := shrinkIndex(m.Columns, widths)
		if idx < 0 {
			break
		}
		widths[idx]--
		total--
	}
	return widths
}

// columnCap is how wide a column may grow before its text wraps. The name
// column may use half the right-hand pane; the others stay compact.
func (m *Model) columnCap(c Column) int {
	if c.MaxWidth > 0 {
		return c.MaxWidth
	}
	if c.Flex && m.width > 1 {
		return m.width / 2
	}
	return defaultMaxWidth
}

// shrinkIndex picks the column that should give up one cell. Flex
// columns go first, down to MinWidth, then any column down to 4.
func shrinkIndex(cols []Column, widths []int) int {
	best, idx := -1, -1
	for i, w := range widths {
		floor := 4
		if cols[i].Flex && cols[i].MinWidth > floor {
			floor = cols[i].MinWidth
		}
		if w <= floor {
			continue
		}
		score := w
		if cols[i].Flex {
			score += 1000
		}
		if score > best {
			best, idx = score, i
		}
	}
	if idx >= 0 {
		return idx
	}
	for i, w := range widths {
		if w > 4 && (idx < 0 || w > widths[idx]) {
			idx = i
		}
	}
	return idx
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

// renderRow wraps each cell to its column width and returns one styled
// line per wrapped row.
func (m *Model) renderRow(idx int, widths []int) []string {
	row := m.rows[idx]
	cols := make([][]string, len(m.Columns))
	height := 1
	for i := range m.Columns {
		if widths[i] == 0 {
			continue
		}
		cols[i] = wrapText(cellAt(row, i), widths[i])
		if len(cols[i]) > height {
			height = len(cols[i])
		}
	}

	lines := make([]string, 0, height)
	for line := 0; line < height; line++ {
		var parts []string
		if m.Multi {
			mark := "  "
			if line == 0 && m.selected[row.ID] {
				mark = "▸ "
			}
			parts = append(parts, mark)
		}
		for i, c := range m.Columns {
			if widths[i] == 0 {
				continue
			}
			cell := ""
			if line < len(cols[i]) {
				cell = cols[i][line]
			}
			if c.Right && line == 0 {
				parts = append(parts, ui.PadLeft(cell, widths[i]))
				continue
			}
			parts = append(parts, ui.Pad(cell, widths[i]))
		}
		lines = append(lines, m.styleLine(idx, ui.Pad(strings.Join(parts, " "), m.width)))
	}
	return lines
}

func (m *Model) styleLine(idx int, line string) string {
	row := m.rows[idx]
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

// wrapText breaks s into lines of at most width cells. Breaks land on a
// space when there is one; a single long token is split hard.
func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	if s == "" || runewidth.StringWidth(s) <= width {
		if s == "" {
			return []string{""}
		}
		return []string{s}
	}
	var lines []string
	rest := s
	for rest != "" {
		if runewidth.StringWidth(rest) <= width {
			lines = append(lines, rest)
			break
		}
		cut := fitPrefix(rest, width)
		if sp := strings.LastIndexByte(rest[:cut], ' '); sp > 0 {
			lines = append(lines, rest[:sp])
			rest = strings.TrimLeft(rest[sp+1:], " ")
			continue
		}
		lines = append(lines, rest[:cut])
		rest = rest[cut:]
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func fitPrefix(s string, width int) int {
	w := 0
	end := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw < 1 {
			rw = 1
		}
		if w+rw > width {
			if end == 0 {
				_, size := utf8.DecodeRuneInString(s)
				return size
			}
			return end
		}
		w += rw
		end = i + utf8.RuneLen(r)
	}
	return len(s)
}

// ScrollInfo renders "12/128" for a pane title.
func (m *Model) ScrollInfo() string {
	if len(m.rows) == 0 {
		return ""
	}
	return strconv.Itoa(m.cursor+1) + "/" + strconv.Itoa(len(m.rows))
}
