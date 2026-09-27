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
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	left := min(36, m.w/3)
	m.table.SetSize(left-2, m.h-2)
	m.prev.SetSize(max(m.w-left-2, 10), m.h-8)
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
	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		if n == ui.NavSelect || km.Matches(k, "func.invoke") {
			return m.invoke()
		}
		m.table.Move(n)
		m.showCurrent()
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
	leftW := min(36, m.w/3)
	left := m.box("Functions", m.table.View(), true)
	right := m.box(funcTitle(m), m.prev.View(), m.focus == 1)
	if !ui.Wide(m.w) {
		return ui.Fit(left+"\n"+right, m.w, m.h)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, ui.Fit(left, leftW, m.h), ui.Fit(right, m.w-leftW, m.h))
}

func (m *Model) box(title, body string, active bool) string {
	s := m.deps.Theme.Pane
	if active {
		s = m.deps.Theme.PaneActive
	}
	return s.Render(m.deps.Theme.Header.Render(title) + "\n" + body)
}

func funcTitle(m *Model) string {
	if f, ok := m.current(); ok {
		return f.Name
	}
	return "Invoke"
}

type fnList []funcs.Function
type invokeMsg funcs.Response
