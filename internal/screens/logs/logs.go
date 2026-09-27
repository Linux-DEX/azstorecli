package logs

import (
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

type Deps struct {
	Theme theme.Theme
	Keys  *keymap.KeyMap
	Stack *stack.Stack
}

type Model struct {
	deps       Deps
	w, h       int
	vp         viewport.Model
	filter     textinput.Model
	filtering  bool
	source     string // "", "azurite", "functions"
	paused     bool
	wrap       bool
	times      bool
	jsonPretty bool
	search     string
	lines      []supervisor.LogLine
	suppress   bool
}

func New(deps Deps) *Model {
	ti := textinput.New()
	ti.Placeholder = "level>=warn  func:Name  /regex/"
	ti.CharLimit = 200
	ti.Width = 40
	return &Model{deps: deps, vp: viewport.New(0, 0), filter: ti, times: true}
}

func (m *Model) Init() tea.Cmd { return m.reload() }
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.vp.Width, m.vp.Height = w, max(h-3, 1)
}
func (m *Model) Scope() string { return keymap.ScopeLogs }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("logs.pause", "logs.filter", "logs.wrap", "logs.clear", "logs.save")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case linesMsg:
		m.lines = v
		m.render()
		return m, nil
	case msg.Log:
		if m.suppress && isStorageBlip(v.Line.Text) {
			return m, nil
		}
		if !m.paused {
			return m, m.reload()
		}
		return m, nil
	case msg.FocusLogs:
		m.source = v.Source
		return m, m.reload()
	case msg.SnapshotInFlight:
		m.suppress = v.Active
		return m, nil
	case msg.Refresh:
		return m, m.reload()
	case tea.KeyMsg:
		if m.filtering {
			return m, m.handleFilter(v)
		}
		return m, m.handleKey(v)
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(teaMsg)
	return m, cmd
}

func (m *Model) handleFilter(k tea.KeyMsg) tea.Cmd {
	if k.String() == "esc" || k.String() == "enter" {
		m.filtering = false
		m.filter.Blur()
		m.render()
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	return cmd
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	switch {
	case km.Matches(k, "logs.pause"):
		m.paused = !m.paused
		if !m.paused {
			return m.reload()
		}
	case km.Matches(k, "logs.source_all"):
		m.source = ""
		return m.reload()
	case km.Matches(k, "logs.source_azurite"):
		m.source = "azurite"
		return m.reload()
	case km.Matches(k, "logs.source_functions"):
		m.source = "functions"
		return m.reload()
	case km.Matches(k, "logs.filter"), km.Matches(k, "app.filter"):
		m.filtering = true
		m.filter.Focus()
	case km.Matches(k, "logs.wrap"):
		m.wrap = !m.wrap
		m.render()
	case km.Matches(k, "logs.clear"):
		if ring, err := m.deps.Stack.Sup.Logs(m.sourceOrFirst()); err == nil {
			ring.Clear()
		}
		return m.reload()
	case km.Matches(k, "logs.save"):
		return m.save()
	case km.Matches(k, "logs.timestamps"):
		m.times = !m.times
		m.render()
	case km.Matches(k, "logs.json"):
		m.jsonPretty = !m.jsonPretty
		m.render()
	case km.Matches(k, "logs.follow"), km.Matches(k, "nav.bottom"):
		m.paused = false
		m.vp.GotoBottom()
		return m.reload()
	default:
		if n := ui.ResolveNav(km, k); n != ui.NavNone {
			switch n {
			case ui.NavUp:
				m.vp.LineUp(1)
			case ui.NavDown:
				m.vp.LineDown(1)
			case ui.NavHalfUp:
				m.vp.HalfViewUp()
			case ui.NavHalfDown:
				m.vp.HalfViewDown()
			case ui.NavTop:
				m.vp.GotoTop()
			case ui.NavBottom:
				m.vp.GotoBottom()
			}
		}
	}
	return nil
}

func (m *Model) sourceOrFirst() string {
	if m.source != "" {
		return m.source
	}
	return "azurite"
}

func (m *Model) reload() tea.Cmd {
	sup, src := m.deps.Stack.Sup, m.source
	return func() tea.Msg {
		var out []supervisor.LogLine
		names := []string{"azurite", "functions"}
		if src != "" {
			names = []string{src}
		}
		for _, n := range names {
			ring, err := sup.Logs(n)
			if err != nil {
				continue
			}
			out = append(out, ring.Lines()...)
		}
		return linesMsg(out)
	}
}

func (m *Model) render() {
	expr := strings.TrimSpace(m.filter.Value())
	var buf strings.Builder
	for _, l := range m.lines {
		if !matchLine(l, expr) {
			continue
		}
		tag := "AZ"
		if l.Process == "functions" {
			tag = "FN"
		}
		line := l.Text
		if m.times {
			line = l.At.Format("15:04:05") + "  " + tag + "  " + line
		}
		if !m.wrap && m.w > 0 {
			line = ui.Truncate(line, m.w)
		}
		buf.WriteString(colorize(m.deps.Theme, line) + "\n")
	}
	m.vp.SetContent(buf.String())
	if !m.paused {
		m.vp.GotoBottom()
	}
}

func (m *Model) save() tea.Cmd {
	body := m.vp.View()
	return func() tea.Msg {
		name := "azstore-logs-" + time.Now().Format("20060102-150405") + ".log"
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "saved " + name}
	}
}

func (m *Model) View() string {
	src := "all"
	if m.source != "" {
		src = m.source
	}
	pause := "▶ live"
	if m.paused {
		pause = "⏸ paused"
	}
	head := m.deps.Theme.Header.Render("Logs  [" + src + "]  " + pause + "  " + itoa(len(m.lines)) + " lines")
	foot := ""
	if m.filtering || m.filter.Value() != "" {
		foot = "\nfilter: " + m.filter.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, head, m.vp.View()+foot)
}

func matchLine(l supervisor.LogLine, expr string) bool {
	if expr == "" {
		return true
	}
	low := strings.ToLower(l.Text)
	if strings.HasPrefix(expr, "level>=") {
		return levelAtLeast(low, strings.TrimPrefix(expr, "level>="))
	}
	if strings.HasPrefix(expr, "func:") {
		return strings.Contains(l.Text, strings.TrimPrefix(expr, "func:"))
	}
	if strings.HasPrefix(expr, "/") && strings.HasSuffix(expr, "/") && len(expr) > 2 {
		re, err := regexp.Compile(expr[1 : len(expr)-1])
		if err != nil {
			return strings.Contains(low, strings.ToLower(expr))
		}
		return re.MatchString(l.Text)
	}
	return strings.Contains(low, strings.ToLower(expr))
}

func levelAtLeast(line, min string) bool {
	order := map[string]int{"debug": 0, "info": 1, "warn": 2, "warning": 2, "error": 3}
	need := order[strings.ToLower(min)]
	have := 1
	switch {
	case strings.Contains(line, "error"):
		have = 3
	case strings.Contains(line, "warn"):
		have = 2
	case strings.Contains(line, "debug"):
		have = 0
	}
	return have >= need
}

func colorize(t theme.Theme, line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error"):
		return t.StatusErr.Render(line)
	case strings.Contains(low, "warn"):
		return t.StatusWarn.Render(line)
	default:
		return line
	}
}

func isStorageBlip(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "azurewebjobsstorage") ||
		strings.Contains(low, "connection refused") ||
		strings.Contains(low, "no connection could be made") ||
		strings.Contains(low, "econnrefused")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type linesMsg []supervisor.LogLine
