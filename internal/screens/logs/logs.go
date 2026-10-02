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
	m.syncViewport()
}

// chromeLines is the title and the status row, plus the filter row when
// it is on screen.
func (m *Model) chromeLines() int {
	if m.filtering || m.filter.Value() != "" {
		return 3
	}
	return 2
}

func (m *Model) syncViewport() {
	m.vp.Width = m.w
	m.vp.Height = max(m.h-m.chromeLines(), 1)
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
	width := m.w
	if width <= 0 {
		width = 80
	}
	var buf strings.Builder
	for _, l := range m.lines {
		if !matchLine(l, expr) {
			continue
		}
		buf.WriteString(m.formatLine(l, width) + "\n")
	}
	m.vp.SetContent(strings.TrimRight(buf.String(), "\n"))
	if !m.paused {
		m.vp.GotoBottom()
	}
}

// formatLine keeps the clock and the source in fixed columns and colors
// only the message, so a warning does not paint the timestamp.
func (m *Model) formatLine(l supervisor.LogLine, width int) string {
	tag := "AZ"
	if l.Process == "functions" {
		tag = "FN"
	}
	prefix := tag
	col := 4
	if m.times {
		prefix = l.At.Format("15:04:05") + "  " + tag
		col = 14
	}
	text := l.Text
	if !m.wrap && width > col {
		text = ui.Truncate(text, width-col)
	}
	return m.deps.Theme.Muted.Render(ui.Pad(prefix, col-2)) + "  " + colorize(m.deps.Theme, text)
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
	if m.h <= 0 {
		return ""
	}
	m.syncViewport()
	var b strings.Builder
	b.WriteString(m.deps.Theme.Header.Render("Logs"))
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	if m.filtering || m.filter.Value() != "" {
		b.WriteString("\n")
		b.WriteString(m.deps.Theme.Muted.Render(ui.Pad("filter", 8)) + m.filter.View())
	}
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	// The body must be exactly m.h lines. A short view lets the tab bar
	// float up when this screen is focused.
	return fillHeight(b.String(), m.h)
}

func (m *Model) statusLine() string {
	src := "all"
	if m.source != "" {
		src = m.source
	}
	state := "live"
	style := m.deps.Theme.StatusOK
	if m.paused {
		state = "paused"
		style = m.deps.Theme.StatusWarn
	}
	return m.field("source", src) + m.field("state", style.Render(state)) + m.field("lines", itoa(len(m.lines)))
}

func (m *Model) field(label, value string) string {
	return m.deps.Theme.Muted.Render(ui.Pad(label, 8)) + ui.Pad(value, 12)
}

func fillHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
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
