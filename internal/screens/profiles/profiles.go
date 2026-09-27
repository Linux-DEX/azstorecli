package profiles

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

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
		{Title: "ENDPOINT", Width: 28},
		{Title: "AUTH", Width: 8},
	})
	return &Model{deps: deps, table: tbl}
}

func (m *Model) Init() tea.Cmd { m.fill(); return nil }
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.table.SetSize(w, max(h-8, 4))
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
		lock := ""
		if p.IsReadOnly() {
			lock = " 🔒"
		}
		rows = append(rows, datatable.Row{
			ID:    p.Name,
			Cells: []string{mark, p.Name + lock, string(p.Type), p.BlobEndpoint, string(p.Auth)},
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
	if m.w <= 0 {
		return ""
	}
	detail := ""
	if p, ok := m.current(); ok {
		detail = strings.Join([]string{
			"name       " + p.Name,
			"type       " + string(p.Type),
			"blob       " + p.BlobEndpoint,
			"queue      " + p.QueueEndpoint,
			"table      " + p.TableEndpoint,
			"auth       " + string(p.Auth),
			"readonly   " + boolStr(p.IsReadOnly()),
		}, "\n")
	}
	warn := ""
	if m.deps.Stack.Profiles.InsecureMode {
		warn = "\n" + m.deps.Theme.StatusWarn.Render("profiles.yaml is not mode 0600")
	}
	return ui.Fit(m.deps.Theme.Header.Render("Connection Profiles")+"\n"+m.table.View()+"\n"+detail+warn, m.w, m.h)
}

func boolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
