package table

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
	"github.com/Linux-DEX/azstorecli/internal/components/treepane"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/storage"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

type Deps struct {
	Theme theme.Theme
	Keys  *keymap.KeyMap
	Stack *stack.Stack
}

const (
	paneSide = iota
	paneList
)

type Model struct {
	deps      Deps
	w, h      int
	focus     int
	side      *treepane.Model
	table     *datatable.Model
	filter    textinput.Model
	filtering bool
	name      string
	page      storage.EntityPage
	history   []string
	histIdx   int
	hidden    map[string]bool
	detail    string

	// cached layout, computed once in Resize() and reused by View() so the
	// sizes handed to child components always match the sizes used to
	// compose the final frame.
	sideW, detailH int
}

func New(deps Deps) *Model {
	side := treepane.New(deps.Theme, "Tables")
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "PartitionKey", Flex: true, MinWidth: 10},
		{Title: "RowKey", Width: 12},
		{Title: "Timestamp", Width: 12},
	})
	ti := textinput.New()
	ti.Placeholder = "PartitionKey eq '…'"
	ti.CharLimit = 400
	ti.Width = 60
	return &Model{deps: deps, side: side, table: tbl, filter: ti, hidden: map[string]bool{}}
}

func (m *Model) Init() tea.Cmd { return m.loadTables() }

// frameSize returns how many columns/rows box() adds on top of a child's
// raw body: the pane style's border+padding, plus one row for the
// "title\n" header line that box() always prepends. Deriving this from
// the style itself (instead of hardcoded constants) keeps the layout
// correct even if the theme's border/padding ever changes.
func (m *Model) frameSize() (h, v int) {
	h = m.deps.Theme.Pane.GetHorizontalFrameSize()
	v = m.deps.Theme.Pane.GetVerticalFrameSize() + 1 // +1 for the header line
	return
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	if w <= 0 || h <= 0 {
		return
	}
	hFrame, vFrame := m.frameSize()

	m.sideW = ui.SideWidth(w)
	m.side.SetSize(max(m.sideW-hFrame, 1), max(h-vFrame, 1))

	// The right-hand box stacks THREE things inside one border: the
	// "Query …" header line, the table body, and the detail block below
	// it. All three must fit inside (h - vFrame), so we split that budget
	// explicitly instead of giving table.View() the whole height and then
	// silently overflowing the box with the query line + detail on top.
	const queryLineRows = 1
	m.detailH = clamp(h/4, 3, 8)

	rightW := w - hFrame
	if ui.Wide(w) {
		rightW = w - m.sideW - hFrame
	}
	rightInnerH := max(h-vFrame, 1)
	tableRows := clamp(rightInnerH-queryLineRows-m.detailH, 1, rightInnerH-queryLineRows-1)

	m.table.SetSize(max(rightW, 10), tableRows)
}

func (m *Model) Scope() string { return keymap.ScopeTable }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("table.filter", "table.new", "table.edit", "table.delete", "table.export")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case tablesMsg:
		items := make([]treepane.Item, 0, len(v))
		for _, t := range v {
			items = append(items, treepane.Item{Name: t.Name})
		}
		m.side.SetItems(items)
		if m.name == "" && len(v) > 0 {
			m.name = v[0].Name
			return m, m.query("")
		}
		return m, nil
	case pageMsg:
		m.page = storage.EntityPage(v)
		m.fill()
		m.detailCurrent()
		return m, nil
	case msg.FocusTable:
		m.name = v.Table
		m.side.Select(v.Table)
		return m, m.query("")
	case msg.ProfileChanged, msg.Refresh:
		return m, m.loadTables()
	case msg.ModalResult:
		return m, m.onModal(v)
	case msg.Edited:
		return m, m.onEdited(v)
	case tea.KeyMsg:
		if m.filtering {
			return m, m.handleFilter(v)
		}
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleFilter(k tea.KeyMsg) tea.Cmd {
	if k.String() == "esc" {
		m.filtering = false
		m.filter.Blur()
		return nil
	}
	if k.String() == "enter" {
		m.filtering = false
		m.filter.Blur()
		f := m.filter.Value()
		if err := storage.ValidateFilter(f); err != nil {
			return func() tea.Msg { return ui.Fail(err) }
		}
		return m.query(f)
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	return cmd
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	if km.Matches(k, "app.next_pane") || km.Matches(k, "app.prev_pane") {
		m.focus = 1 - m.focus
		m.sync()
		return nil
	}
	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		switch m.focus {
		case paneSide:
			m.side.Move(n)
			if it, ok := m.side.Current(); ok && it.Name != m.name {
				m.name, m.history, m.histIdx = it.Name, nil, 0
				return m.query(m.filter.Value())
			}
			return nil
		case paneList:
			m.table.Move(n)
			m.detailCurrent()
			return nil
		}
		return nil
	}
	switch {
	case km.Matches(k, "table.filter"), km.Matches(k, "app.filter"):
		m.filtering = true
		m.filter.Focus()
		return nil
	case km.Matches(k, "table.saved_queries"):
		return m.savedQueries()
	case km.Matches(k, "table.new"):
		return func() tea.Msg {
			body, err := util.EditTemp(".json", "{\n  \"PartitionKey\": \"\",\n  \"RowKey\": \"\"\n}\n")
			return msg.Edited{Kind: "table.new", Content: body, Err: err}
		}
	case km.Matches(k, "table.edit"):
		return m.edit()
	case km.Matches(k, "table.delete"):
		e, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalConfirm, Title: "Delete entity " + e.PartitionKey + "/" + e.RowKey + "?", Destructive: true, Action: "delete"}
		}
	case km.Matches(k, "table.columns"):
		return m.ask("columns", "Hide column", "column name")
	case km.Matches(k, "table.sort"):
		m.table.SetSort(0)
		return nil
	case km.Matches(k, "table.export"):
		return m.ask("export", "Export path", "file.csv / file.json / file.ndjson")
	case km.Matches(k, "table.import"):
		return m.ask("import", "Import path", "file.csv or file.json")
	case km.Matches(k, "table.next_page"):
		if m.page.Token == "" {
			return nil
		}
		m.history = append(m.history[:m.histIdx], m.page.Token)
		m.histIdx++
		return m.queryToken(m.filter.Value(), m.page.Token)
	case km.Matches(k, "table.prev_page"):
		if m.histIdx == 0 {
			return m.query(m.filter.Value())
		}
		m.histIdx--
		tok := ""
		if m.histIdx > 0 {
			tok = m.history[m.histIdx-1]
		}
		return m.queryToken(m.filter.Value(), tok)
	case km.Matches(k, "table.new_table"):
		return m.ask("new_table", "New table", "name")
	case km.Matches(k, "app.yank"):
		if e, ok := m.current(); ok {
			b, _ := json.MarshalIndent(e.Properties, "", "  ")
			_ = util.Yank(string(b))
			return func() tea.Msg { return msg.Status{Text: "yanked entity JSON"} }
		}
	}
	return nil
}

func (m *Model) sync() {
	m.side.Focused = m.focus == paneSide
	m.table.Focused = m.focus == paneList
}

func (m *Model) ask(action, title, prompt string) tea.Cmd {
	return func() tea.Msg {
		return msg.OpenModal{Kind: msg.ModalInput, Title: title, Prompt: prompt, Action: action}
	}
}

func (m *Model) fill() {
	cols := []datatable.Column{
		{Title: "PartitionKey", Flex: true, MinWidth: 10},
		{Title: "RowKey", Width: 12},
	}
	extra := make([]string, 0, len(m.page.Columns))
	for _, c := range m.page.Columns {
		if c == "PartitionKey" || c == "RowKey" || c == "Timestamp" || m.hidden[c] {
			continue
		}
		extra = append(extra, c)
		if len(extra) == 4 {
			break
		}
	}
	for _, c := range extra {
		cols = append(cols, datatable.Column{Title: c, Width: 12})
	}
	cols = append(cols, datatable.Column{Title: "Timestamp", Width: 12})
	m.table.Columns = cols

	rows := make([]datatable.Row, 0, len(m.page.Entities))
	for _, e := range m.page.Entities {
		cells := []string{e.PartitionKey, e.RowKey}
		for _, c := range extra {
			cells = append(cells, storage.FormatValue(e.Properties[c]))
		}
		cells = append(cells, util.RelTime(e.Timestamp))
		rows = append(rows, datatable.Row{ID: e.PartitionKey + "\t" + e.RowKey, Cells: cells})
	}
	m.table.SetRows(rows)
}

func (m *Model) current() (storage.Entity, bool) {
	row, ok := m.table.Current()
	if !ok {
		return storage.Entity{}, false
	}
	pk, rk, _ := strings.Cut(row.ID, "\t")
	for _, e := range m.page.Entities {
		if e.PartitionKey == pk && e.RowKey == rk {
			return e, true
		}
	}
	return storage.Entity{}, false
}

func (m *Model) detailCurrent() {
	e, ok := m.current()
	if !ok {
		m.detail = ""
		return
	}
	var b strings.Builder
	for _, k := range sortedKeys(e.Properties) {
		b.WriteString(k + "  " + storage.TypeName(e.Properties[k]) + "  " + storage.FormatValue(e.Properties[k]) + "\n")
	}
	m.detail = b.String()
}

func (m *Model) loadTables() tea.Cmd {
	c := m.deps.Stack.Clients
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		list, err := c.ListTables(ctx)
		if err != nil {
			return ui.Fail(err)
		}
		return tablesMsg(list)
	}
}

func (m *Model) query(filter string) tea.Cmd { return m.queryToken(filter, "") }

func (m *Model) queryToken(filter, token string) tea.Cmd {
	c, name, size := m.deps.Stack.Clients, m.name, int32(m.deps.Stack.Cfg.UI.PageSize)
	return func() tea.Msg {
		if name == "" {
			return pageMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		page, err := c.QueryEntities(ctx, name, filter, token, size)
		if err != nil {
			return ui.Fail(err)
		}
		return pageMsg(page)
	}
}

func (m *Model) edit() tea.Cmd {
	e, ok := m.current()
	if !ok {
		return nil
	}
	body, _ := json.MarshalIndent(e.Properties, "", "  ")
	return func() tea.Msg {
		edited, err := util.EditTemp(".json", string(body))
		return msg.Edited{Kind: "table.edit", Content: edited, Err: err}
	}
}

func (m *Model) onEdited(e msg.Edited) tea.Cmd {
	if e.Err != nil {
		return func() tea.Msg { return ui.Fail(e.Err) }
	}
	var props map[string]any
	if err := json.Unmarshal([]byte(e.Content), &props); err != nil {
		return func() tea.Msg { return ui.Fail(err) }
	}
	c, name := m.deps.Stack.Clients, m.name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.UpsertEntity(ctx, name, props); err != nil {
			return ui.Fail(err)
		}
		return msg.Refresh{}
	}
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	c, name := m.deps.Stack.Clients, m.name
	switch r.Action {
	case "delete":
		e, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.DeleteEntity(ctx, name, e.PartitionKey, e.RowKey); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "new_table":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.CreateTable(ctx, r.Value); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "columns":
		m.hidden[r.Value] = !m.hidden[r.Value]
		m.fill()
		return nil
	case "export":
		return m.doExport(r.Value)
	case "import":
		return m.doImport(r.Value)
	case "saved":
		m.filter.SetValue(r.Value)
		return m.query(r.Value)
	}
	return nil
}

func (m *Model) doExport(path string) tea.Cmd {
	page := m.page
	return func() tea.Msg {
		if err := exportEntities(path, page); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "exported " + path}
	}
}

func (m *Model) doImport(path string) tea.Cmd {
	c, name := m.deps.Stack.Clients, m.name
	return func() tea.Msg {
		ents, err := importEntities(path)
		if err != nil {
			return ui.Fail(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, e := range ents {
			if err := c.UpsertEntity(ctx, name, e); err != nil {
				return ui.Fail(err)
			}
		}
		return msg.Refresh{}
	}
}

func (m *Model) savedQueries() tea.Cmd {
	path := config.ProjectConfigPath(m.deps.Stack.Cfg.ProjectDir())
	path = strings.TrimSuffix(path, "config.yaml") + "queries.yaml"
	return func() tea.Msg {
		raw, err := os.ReadFile(path)
		if err != nil {
			return ui.Fail(err)
		}
		var doc struct {
			Queries []struct{ Name, Table, Filter string }
		}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return ui.Fail(err)
		}
		var b strings.Builder
		for _, q := range doc.Queries {
			if m.name != "" && q.Table != "" && q.Table != m.name {
				continue
			}
			b.WriteString(q.Name + "  " + q.Filter + "\n")
		}
		first := ""
		if len(doc.Queries) > 0 {
			first = doc.Queries[0].Filter
		}
		return msg.OpenModal{Kind: msg.ModalInput, Title: "Saved query", Body: b.String(), Prompt: "paste filter", Value: first, Action: "saved"}
	}
}

func (m *Model) View() string {
	if m.w <= 0 {
		return ""
	}
	q := m.filter.Value()
	if q == "" {
		q = "(all)"
	}
	header := "Query  " + q
	if m.filtering {
		header = "Query  " + m.filter.View()
	}

	// Reuse the layout computed once in Resize(); never recompute sideW
	// here, or it can drift out of sync with what m.side/m.table were
	// actually sized to.
	if !ui.Wide(m.w) {
		if m.focus == paneSide {
			body := m.box(m.side.Title(), m.side.View(), true)
			return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
		}
		body := m.box(m.name+"  "+m.table.ScrollInfo(), header+"\n"+m.table.View()+"\n"+m.detail, true)
		return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
	}

	left := m.box(m.side.Title(), m.side.View(), m.focus == paneSide)
	right := m.box(m.name+"  "+m.table.ScrollInfo(), header+"\n"+m.table.View()+"\n"+m.detail, m.focus == paneList)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.sideW).Height(m.h).Render(left),
		lipgloss.NewStyle().Width(m.w-m.sideW).Height(m.h).Render(right),
	)
}

func (m *Model) box(title, body string, active bool) string {
	s := m.deps.Theme.Pane
	if active {
		s = m.deps.Theme.PaneActive
	}
	// Height is the inside of the border. Stretching it keeps the pane
	// full height when the detail block is shorter than the space reserved
	// for it; clipping keeps a long detail from pushing the border off.
	contentH := max(m.h-s.GetVerticalFrameSize(), 1)
	text := clipHeight(m.deps.Theme.Header.Render(title)+"\n"+body, contentH)
	return s.Height(contentH).Render(text)
}

func clipHeight(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func exportEntities(path string, page storage.EntityPage) error {
	switch {
	case strings.HasSuffix(path, ".csv"):
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		cols := append([]string{"PartitionKey", "RowKey"}, page.Columns...)
		_ = w.Write(cols)
		for _, e := range page.Entities {
			row := make([]string, len(cols))
			for i, c := range cols {
				row[i] = storage.FormatValue(e.Properties[c])
			}
			_ = w.Write(row)
		}
		w.Flush()
		return w.Error()
	case strings.HasSuffix(path, ".ndjson"):
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		for _, e := range page.Entities {
			if err := enc.Encode(e.Properties); err != nil {
				return err
			}
		}
		return nil
	default:
		var rows []map[string]any
		for _, e := range page.Entities {
			rows = append(rows, e.Properties)
		}
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
}

func importEntities(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".csv") {
		r := csv.NewReader(strings.NewReader(string(data)))
		rows, err := r.ReadAll()
		if err != nil || len(rows) < 2 {
			return nil, err
		}
		var out []map[string]any
		head := rows[0]
		for _, row := range rows[1:] {
			m := map[string]any{}
			for i, h := range head {
				if i < len(row) {
					m[h] = row[i]
				}
			}
			out = append(out, m)
		}
		return out, nil
	}
	var out []map[string]any
	if err := json.Unmarshal(data, &out); err == nil {
		return out, nil
	}
	var one map[string]any
	if err := json.Unmarshal(data, &one); err != nil {
		return nil, err
	}
	return []map[string]any{one}, nil
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for _, p := range []string{"PartitionKey", "RowKey"} {
		if _, ok := m[p]; ok {
			out = append(out, p)
		}
	}
	var rest []string
	for k := range m {
		if k != "PartitionKey" && k != "RowKey" {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

type tablesMsg []storage.TableInfo
type pageMsg storage.EntityPage
