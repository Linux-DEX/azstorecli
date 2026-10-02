package profiles

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
	"github.com/Linux-DEX/azstorecli/internal/config"
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
	deps  Deps
	w, h  int
	table *datatable.Model
}

func New(deps Deps) *Model {
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "", Width: 2},
		{Title: "NAME", Flex: true, MinWidth: 14},
		{Title: "TYPE", Width: 10},
	})
	return &Model{deps: deps, table: tbl}
}

func (m *Model) Init() tea.Cmd { m.fill(); return nil }
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.layout()
}
func (m *Model) Scope() string { return keymap.ScopeProfiles }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("profile.activate", "profile.new", "profile.test", "profile.readonly")
}

func (m *Model) fill() {
	store := m.deps.Stack.Profiles
	rows := make([]datatable.Row, 0, len(store.Profiles))
	for _, p := range store.Profiles {
		mark := "○"
		if p.Name == store.Active {
			mark = "●"
		}
		rows = append(rows, datatable.Row{
			ID:    p.Name,
			Cells: []string{mark, p.Name, string(p.Type)},
		})
	}
	m.table.SetRows(rows)
}

func (m *Model) current() (config.Profile, bool) {
	row, ok := m.table.Current()
	if !ok {
		return config.Profile{}, false
	}
	return m.deps.Stack.Profiles.Get(row.ID)
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case msg.ProfileChanged, msg.Refresh:
		m.fill()
		return m, nil
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
		if n == ui.NavSelect {
			return m.activate()
		}
		m.table.Move(n)
		return nil
	}
	switch {
	case km.Matches(k, "profile.activate"):
		return m.activate()
	case km.Matches(k, "profile.new"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "New profile", Prompt: "name|connection-string", Action: "new"}
		}
	case km.Matches(k, "profile.edit"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Edit connection string", Prompt: "connection string", Action: "edit"}
		}
	case km.Matches(k, "profile.delete"):
		p, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalConfirm, Title: "Delete profile " + p.Name + "?", Destructive: true, Action: "delete"}
		}
	case km.Matches(k, "profile.test"):
		return m.test()
	case km.Matches(k, "app.yank"):
		if p, ok := m.current(); ok {
			_ = util.Yank(config.Redact(p.ConnString()))
			return func() tea.Msg { return msg.Status{Text: "yanked (redacted)"} }
		}
	case km.Matches(k, "profile.yank_full"):
		if p, ok := m.current(); ok {
			_ = util.Yank(p.ConnString())
			return func() tea.Msg { return msg.Status{Text: "yanked connection string", Warning: true} }
		}
	case km.Matches(k, "profile.readonly"):
		p, ok := m.current()
		if !ok {
			return nil
		}
		p.SetReadOnly(!p.IsReadOnly())
		m.deps.Stack.Profiles.Upsert(p)
		_ = m.deps.Stack.Profiles.Save()
		m.fill()
		return func() tea.Msg { return msg.Status{Text: "readonly=" + boolStr(p.IsReadOnly())} }
	}
	return nil
}

func (m *Model) activate() tea.Cmd {
	p, ok := m.current()
	if !ok {
		return nil
	}
	if err := m.deps.Stack.Profiles.Activate(p.Name); err != nil {
		return func() tea.Msg { return ui.Fail(err) }
	}
	_ = m.deps.Stack.Profiles.Save()
	m.deps.Stack.RebindClients()
	m.fill()
	return func() tea.Msg { return msg.ProfileChanged{Name: p.Name} }
}

func (m *Model) test() tea.Cmd {
	c := m.deps.Stack.Clients
	if p, ok := m.current(); ok {
		// test the selected profile, not necessarily the active one
		_ = p
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.Test(ctx); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "connection ok"}
	}
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	store := m.deps.Stack.Profiles
	switch r.Action {
	case "new":
		name, cs, _ := strings.Cut(r.Value, "|")
		name = strings.TrimSpace(name)
		cs = strings.TrimSpace(cs)
		p := config.Profile{Name: name, Type: config.ProfileAzure, ConnectionString: cs}
		if !p.IsEmulator() {
			p.SetReadOnly(true)
		}
		store.Upsert(p)
		_ = store.Save()
		m.fill()
	case "edit":
		p, ok := m.current()
		if !ok {
			return nil
		}
		p.ConnectionString = r.Value
		store.Upsert(p)
		_ = store.Save()
		m.fill()
	case "delete":
		p, ok := m.current()
		if !ok {
			return nil
		}
		if err := store.Delete(p.Name); err != nil {
			return func() tea.Msg { return ui.Fail(err) }
		}
		_ = store.Save()
		m.fill()
	}
	return nil
}

func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	leftW, rightW, leftH, rightH := m.split()
	left := m.pane("Profiles", m.table.View(), leftW, leftH, true)
	right := m.pane("Selected", m.detailBody(m.inner(rightW)), rightW, rightH, false)
	if !ui.Wide(m.w) {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m *Model) layout() {
	if m.w <= 0 || m.h <= 0 {
		return
	}
	leftW, _, leftH, _ := m.split()
	frame := m.deps.Theme.Pane.GetVerticalFrameSize() + 1
	m.table.SetSize(m.inner(leftW), max(leftH-frame, 1))
}

func (m *Model) split() (leftW, rightW, leftH, rightH int) {
	if ui.Wide(m.w) {
		leftW = m.w * 3 / 5
		return leftW, m.w - leftW, m.h, m.h
	}
	leftH = m.h * 2 / 3
	if leftH < 1 {
		leftH = 1
	}
	rightH = m.h - leftH
	if rightH < 1 {
		rightH = 1
		leftH = max(m.h-1, 1)
	}
	return m.w, m.w, leftH, rightH
}

func (m *Model) inner(paneW int) int {
	return max(paneW-m.deps.Theme.Pane.GetHorizontalFrameSize(), 1)
}

func (m *Model) pane(title, body string, width, height int, active bool) string {
	style := m.deps.Theme.Pane
	if active {
		style = m.deps.Theme.PaneActive
	}
	innerW := m.inner(width)
	innerH := height - style.GetVerticalFrameSize() - 1
	if innerH < 1 {
		innerH = 1
	}
	block := m.deps.Theme.Header.Render(title) + "\n" + ui.Fit(body, innerW, innerH)
	return style.Width(innerW).Render(block)
}

func (m *Model) detailBody(width int) string {
	p, ok := m.current()
	if !ok {
		return m.deps.Theme.Muted.Render("no profile selected")
	}
	lines := []string{
		m.kv(width, "name", p.Name),
		m.kv(width, "type", string(p.Type)),
		m.kv(width, "auth", string(p.Auth)),
		m.kv(width, "readonly", boolStr(p.IsReadOnly())),
		m.kv(width, "blob", p.BlobEndpoint),
		m.kv(width, "queue", p.QueueEndpoint),
		m.kv(width, "table", p.TableEndpoint),
	}
	if p.Name == m.deps.Stack.Profiles.Active {
		lines = append([]string{m.deps.Theme.StatusOK.Render("active")}, lines...)
	}
	if m.deps.Stack.Profiles.InsecureMode {
		lines = append(lines, "", m.deps.Theme.StatusWarn.Render("profiles.yaml is not mode 0600"))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) kv(width int, label, value string) string {
	room := width - 12
	if room < 1 {
		room = 1
	}
	return m.deps.Theme.Muted.Render(ui.Pad(label, 12)) + ui.Truncate(value, room)
}

func boolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
