package functions

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
	"github.com/Linux-DEX/azstorecli/internal/components/preview"
	"github.com/Linux-DEX/azstorecli/internal/funcs"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/stack"
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
	paneList = iota
	panePrev
)

type Model struct {
	deps     Deps
	w, h     int
	focus    int
	table    *datatable.Model
	prev     *preview.Model
	list     []funcs.Function
	body     string
	headers  map[string]string
	query    map[string]string
	last     funcs.Response
	watching bool
	zoom     bool

	// cached layout, computed once in Resize() and reused by View() so the
	// sizes handed to child components always match the sizes used to
	// compose the final frame.
	leftW int
}

func New(deps Deps) *Model {
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "NAME", Flex: true, MinWidth: 14},
		{Title: "TRIGGER", Width: 14},
		{Title: "", Width: 2},
	})
	return &Model{
		deps: deps, table: tbl, prev: preview.New(deps.Theme),
		body: "{}\n", headers: map[string]string{}, query: map[string]string{},
	}
}

func (m *Model) Init() tea.Cmd { return m.reload() }

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

func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	if w <= 0 || h <= 0 {
		return
	}
	hFrame, vFrame := m.frameSize()
	if m.zoom {
		m.prev.SetSize(max(w-hFrame, 1), max(h-vFrame, 1))
		return
	}

	if !ui.Wide(w) {
		// Narrow layout: left and right boxes are stacked full-width, each
		// getting roughly half the height, not the full height each (the
		// original code sized both children as if they'd each get the
		// entire h, then concatenated their already-oversized rendered
		// boxes with "\n" and crammed the result into ui.Fit(w, h)).
		m.leftW = 0
		half := h / 2
		m.table.SetSize(max(w-hFrame, 10), max(half-vFrame, 1))
		m.prev.SetSize(max(w-hFrame, 10), max(h-half-vFrame, 1))
		return
	}

	m.leftW = ui.SideWidth(w)
	m.table.SetSize(max(m.leftW-hFrame, 1), max(h-vFrame, 1))
	m.prev.SetSize(max(w-m.leftW-hFrame, 10), max(h-vFrame, 1))
}

func (m *Model) Scope() string { return keymap.ScopeFunctions }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("func.invoke", "func.edit_body", "func.start", "func.stop", "func.watch")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case fnList:
		m.list = v
		m.fill()
		m.showCurrent()
		return m, nil
	case invokeMsg:
		m.last = funcs.Response(v)
		m.showCurrent()
		return m, nil
	case msg.FunctionsLoaded, msg.Refresh, msg.ServiceState:
		return m, m.reload()
	case msg.Edited:
		return m, m.onEdited(v)
	case msg.ModalResult:
		return m, m.onModal(v)
	case tea.KeyMsg:
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	if km.Matches(k, "func.preview_zoom") || (m.zoom && km.Matches(k, "app.escape")) {
		m.setPreviewZoom(!m.zoom)
		return nil
	}
	if m.zoom {
		if n := ui.ResolveNav(km, k); n != ui.NavNone {
			m.prev.Scroll(n)
			return nil
		}
		if km.Matches(k, "app.next_pane") || km.Matches(k, "app.prev_pane") {
			return nil
		}
	}

	if km.Matches(k, "app.next_pane") {
		m.focus = 1 - m.focus
		m.sync()
		return nil
	}
	if km.Matches(k, "app.prev_pane") {
		m.focus = 1 - m.focus
		m.sync()
		return nil
	}

	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		if n == ui.NavSelect || km.Matches(k, "func.invoke") {
			return m.invoke()
		}
		switch m.focus {
		case paneList:
			if n == ui.NavRight && ui.Wide(m.w) {
				m.focus = panePrev
				m.sync()
				return nil
			}
			m.table.Move(n)
			m.showCurrent()
		case panePrev:
			if n == ui.NavLeft && ui.Wide(m.w) {
				m.focus = paneList
				m.sync()
				return nil
			}
			m.prev.Scroll(n)
		}
		return nil
	}
	switch {
	case km.Matches(k, "func.invoke"):
		return m.invoke()
	case km.Matches(k, "func.edit_body"):
		body := m.body
		return func() tea.Msg {
			edited, err := util.EditTemp(".json", body)
			return msg.Edited{Kind: "func.body", Content: edited, Err: err}
		}
	case km.Matches(k, "func.headers"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Header", Prompt: "Name: value", Action: "header"}
		}
	case km.Matches(k, "func.start"):
		return m.hostOp("start", m.deps.Stack.Start)
	case km.Matches(k, "func.stop"):
		return m.hostOp("stop", m.deps.Stack.Stop)
	case km.Matches(k, "func.restart"):
		return m.hostOp("restart", m.deps.Stack.Restart)
	case km.Matches(k, "func.watch"):
		m.watching = !m.watching
		if m.watching {
			_ = m.deps.Stack.StartWatch()
			return func() tea.Msg { return msg.Status{Text: "watch on"} }
		}
		return func() tea.Msg { return msg.Status{Text: "watch off"} }
	case km.Matches(k, "func.logs"):
		return func() tea.Msg { return msg.FocusLogs{Source: "functions"} }
	case km.Matches(k, "func.settings"):
		path := filepath.Join(m.deps.Stack.Cfg.FunctionAppDir(), funcs.SettingsFile)
		return func() tea.Msg {
			if err := util.EditFile(path); err != nil {
				return ui.Fail(err)
			}
			raw, _ := os.ReadFile(path)
			if err := funcs.Validate(raw); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "local.settings.json saved"}
		}
	case km.Matches(k, "func.open_source"):
		if f, ok := m.current(); ok && f.Source != "" {
			return func() tea.Msg {
				if err := util.EditFile(f.Source); err != nil {
					return ui.Fail(err)
				}
				return nil
			}
		}
	case km.Matches(k, "app.yank"):
		if f, ok := m.current(); ok && f.URL != "" {
			_ = util.Yank(f.URL)
			return func() tea.Msg { return msg.Status{Text: "yanked invoke URL"} }
		}
	case km.Matches(k, "func.curl"):
		if f, ok := m.current(); ok {
			req := m.request(f)
			_ = util.Yank(funcs.CurlCommand(req))
			return func() tea.Msg { return msg.Status{Text: "yanked curl"} }
		}
	case km.Matches(k, "func.save_request"):
		return m.saveRequest()
	case km.Matches(k, "func.replay"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Replay request", Prompt: "name", Action: "replay"}
		}
	case km.Matches(k, "func.trigger_target"):
		return m.jumpTarget()
	case km.Matches(k, "func.toggle_enabled"):
		return m.toggle()
	}
	return nil
}

func (m *Model) sync() {
	m.table.Focused = m.focus == paneList
}

func (m *Model) setPreviewZoom(on bool) {
	m.zoom = on
	if on {
		m.focus = panePrev
	} else if m.focus == panePrev {
		m.focus = paneList
	}
	m.sync()
	m.Resize(m.w, m.h)
}

func (m *Model) fill() {
	rows := make([]datatable.Row, 0, len(m.list))
	for _, f := range m.list {
		dot := "●"
		if f.Disabled {
			dot = "○"
		}
		trigger := f.Trigger
		if len(f.Methods) > 0 {
			trigger = "http  [" + strings.Join(f.Methods, ",") + "]"
		}
		rows = append(rows, datatable.Row{ID: f.Name, Cells: []string{f.Name, trigger, dot}, Dim: f.Disabled})
	}
	m.table.SetRows(rows)
}

func (m *Model) current() (funcs.Function, bool) {
	row, ok := m.table.Current()
	if !ok {
		return funcs.Function{}, false
	}
	for _, f := range m.list {
		if f.Name == row.ID {
			return f, true
		}
	}
	return funcs.Function{}, false
}

func (m *Model) showCurrent() {
	f, ok := m.current()
	if !ok {
		m.prev.Clear()
		return
	}
	var b strings.Builder
	b.WriteString("trigger   " + f.Trigger)
	if len(f.Methods) > 0 {
		b.WriteString("  [" + strings.Join(f.Methods, " ") + "]")
	}
	b.WriteString("\n")
	if f.Route != "" {
		b.WriteString("route     " + f.Route + "\n")
	}
	if f.URL != "" {
		b.WriteString("url       " + f.URL + "\n")
	}
	if f.AuthLevel != "" {
		b.WriteString("auth      " + f.AuthLevel + "\n")
	}
	if f.Source != "" {
		b.WriteString("file      " + f.Source + "\n")
	}
	for _, bind := range f.Bindings {
		if !strings.HasSuffix(strings.ToLower(bind.Type), "trigger") {
			b.WriteString("bind      " + bind.Direction + " → " + bind.Type + " " + bind.Target + "\n")
		}
	}
	b.WriteString("\n--- request body ---\n")
	b.WriteString(m.body)
	if m.last.Status != 0 || m.last.Body != "" {
		b.WriteString("\n--- last response  " + strconv.Itoa(m.last.Status) + "  " + m.last.Duration.String() + " ---\n")
		b.WriteString(m.last.Body)
	}
	m.prev.SetContent(f.Name, []byte(b.String()), false)
}

func (m *Model) reload() tea.Cmd {
	dir := m.deps.Stack.Cfg.FunctionAppDir()
	sup := m.deps.Stack.Sup
	return func() tea.Msg {
		discovered, _ := funcs.Discover(dir)
		var fromHost []funcs.Function
		if ring, err := sup.Logs("functions"); err == nil {
			var lines []string
			for _, l := range ring.Lines() {
				lines = append(lines, l.Text)
			}
			fromHost = funcs.ParseRoutes(lines)
		}
		return fnList(funcs.Merge(discovered, fromHost))
	}
}

func (m *Model) invoke() tea.Cmd {
	f, ok := m.current()
	if !ok {
		return nil
	}
	client := funcs.NewClient(funcs.BaseURL(m.deps.Stack.Cfg))
	req := m.request(f)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var resp funcs.Response
		if f.IsHTTP() {
			resp = client.InvokeHTTP(ctx, req)
		} else {
			resp = client.InvokeNonHTTP(ctx, f.Name, m.body)
		}
		return invokeMsg(resp)
	}
}

func (m *Model) request(f funcs.Function) funcs.Request {
	method := "GET"
	if len(f.Methods) > 0 {
		method = f.Methods[0]
	}
	url := f.URL
	if url == "" {
		url = funcs.BaseURL(m.deps.Stack.Cfg) + "/api/" + f.Name
	}
	return funcs.Request{Method: method, URL: url, Headers: m.headers, Query: m.query, Body: m.body}
}

func (m *Model) hostOp(verb string, fn func(context.Context, ...string) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := fn(ctx, "functions"); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: verb + "ed functions"}
	}
}

func (m *Model) onEdited(e msg.Edited) tea.Cmd {
	if e.Err != nil {
		return func() tea.Msg { return ui.Fail(e.Err) }
	}
	if e.Kind == "func.body" {
		m.body = e.Content
		m.showCurrent()
	}
	return nil
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	switch r.Action {
	case "header":
		name, val, ok := strings.Cut(r.Value, ":")
		if ok {
			m.headers[strings.TrimSpace(name)] = strings.TrimSpace(val)
		}
	case "replay":
		path := filepath.Join(m.deps.Stack.Cfg.ProjectDir(), ".azstorecli", "requests", r.Value)
		data, err := os.ReadFile(path)
		if err != nil {
			return func() tea.Msg { return ui.Fail(err) }
		}
		m.body = string(data)
		m.showCurrent()
	}
	return nil
}

func (m *Model) saveRequest() tea.Cmd {
	dir := filepath.Join(m.deps.Stack.Cfg.ProjectDir(), ".azstorecli", "requests")
	f, ok := m.current()
	if !ok {
		return nil
	}
	body := m.body
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ui.Fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name+".json"), []byte(body), 0o644); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "saved request " + f.Name}
	}
}

func (m *Model) jumpTarget() tea.Cmd {
	f, ok := m.current()
	if !ok || f.Target == "" {
		return nil
	}
	t := strings.ToLower(f.Trigger)
	switch {
	case strings.Contains(t, "queue"):
		return func() tea.Msg { return msg.FocusQueue{Queue: f.Target} }
	case strings.Contains(t, "blob"):
		container, prefix, _ := strings.Cut(f.Target, "/")
		return func() tea.Msg { return msg.FocusBlob{Container: container, Prefix: prefix} }
	case strings.Contains(t, "table"):
		return func() tea.Msg { return msg.FocusTable{Table: f.Target} }
	}
	return nil
}

func (m *Model) toggle() tea.Cmd {
	f, ok := m.current()
	if !ok {
		return nil
	}
	path := funcs.FunctionJSONPath(m.deps.Stack.Cfg.FunctionAppDir(), f.Name)
	if path == "" {
		return func() tea.Msg { return msg.Status{Text: "no function.json to patch", Warning: true} }
	}
	return func() tea.Msg {
		if err := funcs.SetDisabled(path, !f.Disabled); err != nil {
			return ui.Fail(err)
		}
		return msg.Refresh{}
	}
}

func (m *Model) View() string {
	if m.w <= 0 {
		return ""
	}

	if m.zoom {
		body := m.box(funcTitle(m), m.prev.View(), true)
		return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
	}

	left := m.box("Functions", m.table.View(), m.focus == paneList)
	right := m.box(funcTitle(m), m.prev.View(), m.focus == panePrev)

	if !ui.Wide(m.w) {
		half := m.h / 2
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Width(m.w).Height(half).Render(left),
			lipgloss.NewStyle().Width(m.w).Height(m.h-half).Render(right),
		)
	}

	// Reuse the layout computed once in Resize(); never recompute leftW
	// here, or it can drift out of sync with what m.table/m.prev were
	// actually sized to.
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.leftW).Height(m.h).Render(left),
		lipgloss.NewStyle().Width(m.w-m.leftW).Height(m.h).Render(right),
	)
}

func (m *Model) box(title, body string, active bool) string {
	s := m.deps.Theme.Pane
	if active {
		s = m.deps.Theme.PaneActive
	}
	return s.Render(m.deps.Theme.Header.Render(title) + "\n" + body)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func funcTitle(m *Model) string {
	if f, ok := m.current(); ok {
		return f.Name
	}
	return "Invoke"
}

type fnList []funcs.Function
type invokeMsg funcs.Response
