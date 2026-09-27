package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/doctor"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// Deps is the slice of services this screen needs.
type Deps struct {
	Theme theme.Theme
	Keys  *keymap.KeyMap
	Stack *stack.Stack
}

// Model is the dashboard.
type Model struct {
	deps     Deps
	w, h     int
	cursor   int
	statuses []supervisor.Status
	ws       azurite.Stats
	snaps    []azurite.Snapshot
	doctor   []doctor.Check
	detail   bool
	busy     string
}

// New builds the dashboard.
func New(deps Deps) *Model { return &Model{deps: deps} }

func (m *Model) Init() tea.Cmd { return m.reload() }

func (m *Model) Resize(w, h int) { m.w, m.h = w, h }

func (m *Model) Scope() string { return keymap.ScopeDashboard }

func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings(
		"service.start", "service.stop", "service.restart",
		"service.start_all", "service.stop_all", "service.doctor",
	)
}

func (m *Model) reload() tea.Cmd {
	return func() tea.Msg {
		return dashData{
			statuses: m.deps.Stack.Sup.Snapshot(),
			ws:       m.deps.Stack.WS.Stat(),
			snaps:    mustList(m.deps.Stack.Snaps),
		}
	}
}

type dashData struct {
	statuses []supervisor.Status
	ws       azurite.Stats
	snaps    []azurite.Snapshot
}

func mustList(s *azurite.Store) []azurite.Snapshot {
	if s == nil {
		return nil
	}
	all, _ := s.List()
	return all
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case dashData:
		m.statuses, m.ws, m.snaps = v.statuses, v.ws, v.snaps
		if m.cursor >= len(m.statuses) {
			m.cursor = max(len(m.statuses)-1, 0)
		}
		return m, nil
	case doctorMsg:
		m.doctor = v
		m.detail = true
		return m, nil
	case msg.ServiceState, msg.Refresh:
		return m, m.reload()
	case tea.KeyMsg:
		if m.detail && m.deps.Keys.Matches(v, "app.escape") {
			m.detail = false
			m.doctor = nil
			return m, nil
		}
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		switch n {
		case ui.NavUp:
			m.cursor--
		case ui.NavDown:
			m.cursor++
		case ui.NavTop:
			m.cursor = 0
		case ui.NavBottom:
			m.cursor = len(m.statuses) - 1
		case ui.NavSelect:
			m.detail = !m.detail
		}
		m.cursor = ui.Clamp(m.cursor, 0, max(len(m.statuses)-1, 0))
		return nil
	}

	switch {
	case km.Matches(k, "service.start"):
		return m.op("start", func(ctx context.Context, name string) error {
			return m.deps.Stack.Start(ctx, name)
		})
	case km.Matches(k, "service.stop"):
		return m.op("stop", func(ctx context.Context, name string) error {
			return m.deps.Stack.Stop(ctx, name)
		})
	case km.Matches(k, "service.restart"):
		return m.op("restart", func(ctx context.Context, name string) error {
			return m.deps.Stack.Restart(ctx, name)
		})
	case km.Matches(k, "service.start_all"):
		return m.all("starting stack", m.deps.Stack.Start)
	case km.Matches(k, "service.stop_all"):
		return m.all("stopping stack", m.deps.Stack.Stop)
	case km.Matches(k, "service.restart_all"):
		return m.all("restarting stack", m.deps.Stack.Restart)
	case km.Matches(k, "service.logs"):
		name := m.selected()
		return func() tea.Msg { return msg.FocusLogs{Source: name} }
	case km.Matches(k, "service.open_workspace"):
		return func() tea.Msg {
			if err := util.OpenPath(m.deps.Stack.WS.Dir); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "opened " + m.deps.Stack.WS.Dir}
		}
	case km.Matches(k, "service.edit_config"):
		path := config.ProjectConfigPath(m.deps.Stack.Cfg.ProjectDir())
		return func() tea.Msg {
			if err := util.EditFile(path); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "config saved — restart to apply"}
		}
	case km.Matches(k, "service.copy_connstr"):
		p := m.deps.Stack.Clients.Profile()
		if err := util.Yank(config.Redact(p.ConnString())); err != nil {
			return func() tea.Msg { return ui.Fail(err) }
		}
		return func() tea.Msg { return msg.Status{Text: "connection string copied (key redacted)"} }
	case km.Matches(k, "service.doctor"):
		cfg := m.deps.Stack.Cfg
		return func() tea.Msg { return doctorMsg(doctor.Run(cfg)) }
	}
	return nil
}

func (m *Model) selected() string {
	if m.cursor < 0 || m.cursor >= len(m.statuses) {
		return ""
	}
	return m.statuses[m.cursor].Name
}

func (m *Model) op(verb string, fn func(context.Context, string) error) tea.Cmd {
	name := m.selected()
	if name == "" {
		return nil
	}
	m.busy = verb + " " + name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := fn(ctx, name); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: verb + "ed " + name}
	}
}

func (m *Model) all(label string, fn func(context.Context, ...string) error) tea.Cmd {
	m.busy = label
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: label}
	}
}

type doctorMsg []doctor.Check

func (m *Model) View() string {
	if m.w <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.themeTitle("Services"))
	if len(m.statuses) == 0 {
		b.WriteString(m.deps.Theme.Muted.Render("  no services registered — run `azstore doctor`") + "\n")
	}
	for i, st := range m.statuses {
		b.WriteString(m.renderService(i, st))
		b.WriteString("\n")
	}

	b.WriteString(m.themeTitle("Workspace"))
	b.WriteString(fmt.Sprintf("  %s   %s\n", m.deps.Stack.WS.Dir, util.Bytes(m.ws.SizeBytes)))
	if last := m.lastSnap(); last != nil {
		b.WriteString(fmt.Sprintf("  last snapshot  %s  ·  %s  ·  %s\n",
			last.Name, util.RelTime(last.CreatedAt), util.Bytes(last.SizeBytes)))
	} else {
		b.WriteString(m.deps.Theme.Muted.Render("  no snapshots yet") + "\n")
	}

	if m.busy != "" {
		b.WriteString("\n" + m.deps.Theme.StatusWarn.Render("  "+m.busy+"…"))
	}

	if m.detail {
		b.WriteString("\n" + m.themeTitle("Detail"))
		if len(m.doctor) > 0 {
			for _, c := range m.doctor {
				mark := m.deps.Theme.StatusOK.Render("✓")
				line := c.Detail
				if !c.OK {
					mark = m.deps.Theme.StatusErr.Render("✖")
					line = c.Error
				}
				b.WriteString(fmt.Sprintf("  %s %-18s %s\n", mark, c.Name, line))
			}
		} else if st, ok := m.current(); ok {
			b.WriteString(fmt.Sprintf("  argv     %s\n", strings.Join(st.Argv, " ")))
			b.WriteString(fmt.Sprintf("  health   %s\n", st.Health))
			b.WriteString(fmt.Sprintf("  depends  %s\n", strings.Join(st.DependsOn, ", ")))
			if st.LastError != "" {
				b.WriteString(m.deps.Theme.StatusErr.Render("  last     "+st.LastError) + "\n")
			}
		}
	}

	return ui.Fit(b.String(), m.w, max(m.h, 1))
}

func (m *Model) renderService(i int, st supervisor.Status) string {
	glyph := theme.StateGlyph(st.State)
	style := m.deps.Theme.StateColor(st.State)
	marker := "  "
	if i == m.cursor {
		marker = "> "
	}
	head := fmt.Sprintf("%s%s %-16s %-10s", marker, style.Render(glyph), title(st.Name), st.State)
	if st.PID > 0 {
		head += fmt.Sprintf("  pid %d  up %s", st.PID, util.Duration(st.Uptime))
	}
	if i == m.cursor {
		head = m.deps.Theme.Selected.Render(ui.Pad(stripSimple(head), m.w))
	}

	var extra string
	switch st.Name {
	case "azurite":
		cfg := m.deps.Stack.Cfg
		extra = fmt.Sprintf("     blob  :%d   queue :%d   table :%d",
			cfg.Azurite.BlobPort, cfg.Azurite.QueuePort, cfg.Azurite.TablePort)
	case "functions":
		cfg := m.deps.Stack.Cfg
		extra = fmt.Sprintf("     runtime  %s        port :%d", cfg.Functions.Runtime, cfg.Functions.Port)
	}
	if extra == "" {
		return head
	}
	return head + "\n" + m.deps.Theme.Muted.Render(extra)
}

func (m *Model) current() (supervisor.Status, bool) {
	if m.cursor < 0 || m.cursor >= len(m.statuses) {
		return supervisor.Status{}, false
	}
	return m.statuses[m.cursor], true
}

func (m *Model) lastSnap() *azurite.Snapshot {
	if len(m.snaps) == 0 {
		return nil
	}
	return &m.snaps[0]
}

func (m *Model) themeTitle(s string) string {
	return m.deps.Theme.Header.Render("─ "+s+" "+strings.Repeat("─", max(m.w-len(s)-3, 0))) + "\n"
}

func title(name string) string {
	switch name {
	case "azurite":
		return "Azurite"
	case "functions":
		return "Functions Host"
	default:
		return name
	}
}

func stripSimple(s string) string { return s }
