package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	leftW, rightW, leftH, rightH := m.split()
	left := m.pane("Services", m.servicesBody(m.inner(leftW)), leftW, leftH, true)
	right := m.pane("Workspace", m.sideBody(m.inner(rightW)), rightW, rightH, false)
	if !ui.Wide(m.w) {
		return lipgloss.JoinVertical(lipgloss.Left, left, right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// split gives the services pane the wider column. On a narrow terminal
// the two panes stack, with services taking the top two thirds.
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
	innerW := max(width-style.GetHorizontalFrameSize(), 1)
	innerH := height - style.GetVerticalFrameSize() - 1 // title line
	if innerH < 1 {
		innerH = 1
	}
	block := m.deps.Theme.Header.Render(title) + "\n" + ui.Fit(body, innerW, innerH)
	return style.Width(innerW).Render(block)
}

func (m *Model) servicesBody(inner int) string {
	if len(m.statuses) == 0 {
		return m.deps.Theme.Muted.Render("no services registered")
	}
	lines := make([]string, 0, len(m.statuses)*2)
	for i, st := range m.statuses {
		lines = append(lines, m.serviceLines(i, st, inner)...)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) serviceLines(i int, st supervisor.Status, inner int) []string {
	glyph := theme.StateGlyph(st.State)
	var tail string
	if st.PID > 0 {
		tail = fmt.Sprintf("pid %d   %s", st.PID, util.Duration(st.Uptime))
	}
	plain := fmt.Sprintf("  %s  %-16s  %-10s", glyph, title(st.Name), st.State)
	if tail != "" {
		plain += "  " + tail
	}
	var head string
	if i == m.cursor {
		head = m.deps.Theme.Selected.Render(ui.Pad(plain, inner))
	} else {
		color := m.deps.Theme.StateColor(st.State)
		head = fmt.Sprintf("  %s  %-16s  %s",
			color.Render(glyph), title(st.Name), color.Render(fmt.Sprintf("%-10s", st.State)))
		if tail != "" {
			head += "  " + tail
		}
	}
	sub := m.serviceMeta(st)
	if sub == "" {
		return []string{head}
	}
	return []string{head, m.deps.Theme.Muted.Render("     " + sub)}
}

func (m *Model) serviceMeta(st supervisor.Status) string {
	cfg := m.deps.Stack.Cfg
	switch st.Name {
	case "azurite":
		return fmt.Sprintf("blob %-5d  queue %-5d  table %d",
			cfg.Azurite.BlobPort, cfg.Azurite.QueuePort, cfg.Azurite.TablePort)
	case "functions":
		return fmt.Sprintf("%-16s  port %d", cfg.Functions.Runtime, cfg.Functions.Port)
	default:
		return ""
	}
}

func (m *Model) sideBody(width int) string {
	lines := []string{
		m.kv(width, "directory", ui.TruncateLeft(m.deps.Stack.WS.Dir, max(width-12, 1))),
		m.kv(width, "size", util.Bytes(m.ws.SizeBytes)),
	}
	if m.ws.HasData && !m.ws.Modified.IsZero() {
		lines = append(lines, m.kv(width, "updated", util.RelTime(m.ws.Modified)))
	}
	if last := m.lastSnap(); last != nil {
		lines = append(lines, m.kv(width, "snapshot", fmt.Sprintf("%s  ·  %s  ·  %s",
			last.Name, util.RelTime(last.CreatedAt), util.Bytes(last.SizeBytes))))
	} else {
		lines = append(lines, m.kv(width, "snapshot", "no snapshots yet"))
	}
	if m.detail {
		lines = append(lines, "", m.deps.Theme.Header.Render("Detail"), m.detailBody(width))
	}
	if m.busy != "" {
		lines = append(lines, "", m.deps.Theme.StatusWarn.Render(m.busy+"…"))
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

func (m *Model) detailBody(width int) string {
	if len(m.doctor) > 0 {
		lines := make([]string, 0, len(m.doctor))
		for _, c := range m.doctor {
			mark, text, style := "●", c.Detail, m.deps.Theme.StatusOK
			if !c.OK {
				mark, text, style = "✖", c.Error, m.deps.Theme.StatusErr
			}
			lines = append(lines, style.Render(mark)+"  "+ui.Pad(c.Name, 16)+"  "+ui.Truncate(text, max(width-20, 1)))
		}
		return strings.Join(lines, "\n")
	}
	st, ok := m.current()
	if !ok {
		return ""
	}
	var lines []string
	if len(st.Argv) > 0 {
		lines = append(lines, m.kv(width, "command", strings.Join(st.Argv, " ")))
	}
	if st.Health != "" {
		lines = append(lines, m.kv(width, "health", st.Health))
	}
	if len(st.DependsOn) > 0 {
		lines = append(lines, m.kv(width, "depends", strings.Join(st.DependsOn, ", ")))
	}
	if st.LastError != "" {
		lines = append(lines, m.deps.Theme.Muted.Render(ui.Pad("error", 12))+m.deps.Theme.StatusErr.Render(ui.Truncate(st.LastError, max(width-12, 1))))
	}
	return strings.Join(lines, "\n")
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
