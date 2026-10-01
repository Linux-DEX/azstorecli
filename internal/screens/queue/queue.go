package queue

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
	"github.com/Linux-DEX/azstorecli/internal/components/preview"
	"github.com/Linux-DEX/azstorecli/internal/components/treepane"
	"github.com/Linux-DEX/azstorecli/internal/funcs"
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
	panePrev
)

type Model struct {
	deps     Deps
	w, h     int
	focus    int
	side     *treepane.Model
	table    *datatable.Model
	prev     *preview.Model
	queue    string
	messages []storage.Message
	watch    bool
	showB64  bool

	// cached layout, computed once in Resize() and reused by View() so the
	// sizes handed to child components always match the sizes used to
	// compose the final frame.
	sideW, listH, prevH int
}

func New(deps Deps) *Model {
	side := treepane.New(deps.Theme, "Queues")
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "#", Width: 3, Right: true},
		{Title: "ID", Flex: true, MinWidth: 10},
		{Title: "INSERTED", Width: 10},
		{Title: "DEQ", Width: 4, Right: true},
		{Title: "EXPIRES", Width: 8},
		{Title: "SIZE", Width: 7, Right: true},
	})
	return &Model{deps: deps, side: side, table: tbl, prev: preview.New(deps.Theme)}
}

func (m *Model) Init() tea.Cmd { return m.loadQueues() }

// frameSize returns how many columns/rows box() adds on top of a child
// component's raw body: the border+padding cost of the pane style, plus
// one row for the "title\n" header line that box() prepends. Deriving
// this from the style itself (instead of a hardcoded constant) keeps the
// layout correct even if the theme's border/padding changes.
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

func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	if w <= 0 || h <= 0 {
		return
	}
	hFrame, vFrame := m.frameSize()

	m.sideW = ui.SideWidth(w)

	if !ui.Wide(w) {
		// Narrow layout: side pane is full-size on its own screen; the list
		// screen stacks table body + preview body inside ONE box, so the
		// two child components must split (h - frame) between them, not
		// each get a fresh (h - frame).
		m.side.SetSize(max(w-hFrame, 1), max(h-vFrame, 1))

		m.prevH = clamp(h/3, 4, 10)
		m.listH = h - m.prevH
		innerH := max(h-vFrame, 1) // total inner rows available inside the single box
		tableRows := clamp(innerH-m.prevH, 1, innerH-1)
		prevRows := innerH - tableRows

		m.table.SetSize(max(w-hFrame, 1), tableRows)
		m.prev.SetSize(max(w-hFrame, 1), prevRows)
		return
	}

	m.prevH = clamp(h/3, 4, 10)
	m.listH = h - m.prevH

	m.side.SetSize(max(m.sideW-hFrame, 1), max(h-vFrame, 1))
	m.table.SetSize(max(w-m.sideW-hFrame, 10), max(m.listH-vFrame, 1))
	m.prev.SetSize(max(w-m.sideW-hFrame, 10), max(m.prevH-vFrame, 1))
}

func (m *Model) Scope() string { return keymap.ScopeQueue }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("queue.add", "queue.delete", "queue.clear", "queue.peek", "queue.watch", "queue.trigger")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case queuesMsg:
		items := make([]treepane.Item, 0, len(v))
		for _, q := range v {
			items = append(items, treepane.Item{Name: q.Name, Count: int(q.ApproxMessages), ShowCount: true})
		}
		m.side.SetItems(items)
		if m.queue == "" && len(v) > 0 {
			m.queue = v[0].Name
			return m, m.peek()
		}
		return m, nil
	case messagesMsg:
		m.messages = v
		m.fill()
		m.preview()
		if m.watch {
			return m, m.tick()
		}
		return m, nil
	case watchTick:
		if m.watch {
			return m, m.peek()
		}
		return m, nil
	case msg.FocusQueue:
		m.queue = v.Queue
		m.side.Select(v.Queue)
		return m, m.peek()
	case msg.ProfileChanged, msg.Refresh:
		return m, m.loadQueues()
	case msg.ModalResult:
		return m, m.onModal(v)
	case msg.Edited:
		return m, m.onEdited(v)
	case tea.KeyMsg:
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	if km.Matches(k, "app.next_pane") {
		m.focus = (m.focus + 1) % 3
		m.sync()
		return nil
	}
	if km.Matches(k, "app.prev_pane") {
		m.focus = (m.focus + 2) % 3
		m.sync()
		return nil
	}
	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		switch m.focus {
		case paneSide:
			if n == ui.NavRight || n == ui.NavSelect {
				m.focus = paneList
				m.sync()
			}
			m.side.Move(n)
			if it, ok := m.side.Current(); ok && it.Name != m.queue {
				m.queue = it.Name
				return m.peek()
			}
		case paneList:
			if n == ui.NavLeft {
				m.focus = paneSide
				m.sync()
				return nil
			}
			m.table.Move(n)
			m.preview()
		case panePrev:
			if n == ui.NavLeft {
				m.focus = paneList
				m.sync()
				return nil
			}
			m.prev.Scroll(n)
		}
		return nil
	}
	switch {
	case km.Matches(k, "queue.add"):
		return func() tea.Msg {
			body, err := util.EditTemp(".json", "{\n  \n}\n")
			return msg.Edited{Kind: "queue.add", Content: body, Err: err}
		}
	case km.Matches(k, "queue.add_file"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Enqueue file", Prompt: "path", Action: "add_file"}
		}
	case km.Matches(k, "queue.delete"):
		cur, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalConfirm, Title: "Delete message " + cur.ID[:min(8, len(cur.ID))] + "?", Destructive: true, Action: "delete"}
		}
	case km.Matches(k, "queue.clear"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalConfirm, Title: "Clear queue " + m.queue + "?", Body: "This cannot be undone.", Destructive: true, Action: "clear"}
		}
	case km.Matches(k, "queue.peek"):
		return m.peek()
	case km.Matches(k, "queue.watch"):
		m.watch = !m.watch
		if m.watch {
			return m.peek()
		}
		return nil
	case km.Matches(k, "queue.requeue"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Requeue to", Prompt: "destination queue", Action: "requeue"}
		}
	case km.Matches(k, "queue.visibility"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Visibility timeout", Prompt: "duration (e.g. 30s)", Action: "visibility"}
		}
	case km.Matches(k, "queue.base64"):
		m.showB64 = !m.showB64
		m.preview()
		return nil
	case km.Matches(k, "queue.new"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "New queue", Prompt: "name", Action: "new"}
		}
	case km.Matches(k, "queue.properties"):
		return m.props()
	case km.Matches(k, "app.yank"):
		if cur, ok := m.current(); ok {
			_ = util.Yank(cur.Body)
			return func() tea.Msg { return msg.Status{Text: "yanked body"} }
		}
	case km.Matches(k, "queue.trigger"):
		return m.trigger()
	}
	return nil
}

func (m *Model) sync() {
	m.side.Focused = m.focus == paneSide
	m.table.Focused = m.focus == paneList
}

func (m *Model) fill() {
	rows := make([]datatable.Row, 0, len(m.messages))
	for i, msg_ := range m.messages {
		id := msg_.ID
		if len(id) > 10 {
			id = id[:8] + "…"
		}
		rows = append(rows, datatable.Row{
			ID: msg_.ID,
			Cells: []string{
				strconv.Itoa(i + 1), id, util.RelTime(msg_.InsertionTime),
				strconv.FormatInt(msg_.DequeueCount, 10), util.RelTime(msg_.ExpirationTime),
				util.Bytes(int64(msg_.SizeBytes)),
			},
			Danger: msg_.NearPoison(),
		})
	}
	m.table.SetRows(rows)
}

func (m *Model) preview() {
	cur, ok := m.current()
	if !ok {
		m.prev.Clear()
		return
	}
	body := cur.Body
	if m.showB64 {
		body = cur.Raw
	}
	m.prev.SetContent(cur.ID, []byte(body), false)
}

func (m *Model) current() (storage.Message, bool) {
	row, ok := m.table.Current()
	if !ok {
		return storage.Message{}, false
	}
	for _, msq := range m.messages {
		if msq.ID == row.ID {
			return msq, true
		}
	}
	return storage.Message{}, false
}

func (m *Model) loadQueues() tea.Cmd {
	c := m.deps.Stack.Clients
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		list, err := c.ListQueues(ctx)
		if err != nil {
			return ui.Fail(err)
		}
		return queuesMsg(list)
	}
}

func (m *Model) peek() tea.Cmd {
	c, name := m.deps.Stack.Clients, m.queue
	return func() tea.Msg {
		if name == "" {
			return messagesMsg(nil)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		list, err := c.PeekMessages(ctx, name, 32)
		if err != nil {
			return ui.Fail(err)
		}
		return messagesMsg(list)
	}
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return watchTick{} })
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	c, q := m.deps.Stack.Clients, m.queue
	switch r.Action {
	case "new":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.CreateQueue(ctx, r.Value); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "clear":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.ClearQueue(ctx, q); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "delete":
		cur, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.DeleteMessageByID(ctx, q, cur.ID); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "requeue":
		cur, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Requeue(ctx, q, r.Value, cur.ID); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "add_file":
		return func() tea.Msg {
			data, err := readFile(r.Value)
			if err != nil {
				return ui.Fail(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := c.EnqueueMessage(ctx, q, data, false, 0, 0); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	}
	return nil
}

func (m *Model) onEdited(e msg.Edited) tea.Cmd {
	if e.Kind != "queue.add" {
		return nil
	}
	if e.Err != nil {
		return func() tea.Msg { return ui.Fail(e.Err) }
	}
	c, q := m.deps.Stack.Clients, m.queue
	body := strings.TrimSpace(e.Content)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := c.EnqueueMessage(ctx, q, body, false, 0, 0); err != nil {
			return ui.Fail(err)
		}
		return msg.Refresh{}
	}
}

func (m *Model) props() tea.Cmd {
	if m.queue == "" {
		return nil
	}
	return func() tea.Msg {
		return msg.OpenModal{Kind: msg.ModalNotice, Title: m.queue, Body: strconv.Itoa(len(m.messages)) + " visible messages (peek)"}
	}
}

func (m *Model) trigger() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}
	fnName := m.boundFunction()
	if fnName == "" {
		return func() tea.Msg { return msg.Status{Text: "no function bound to " + m.queue, Warning: true} }
	}
	client := funcs.NewClient(funcs.BaseURL(m.deps.Stack.Cfg))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		resp := client.InvokeNonHTTP(ctx, fnName, cur.Body)
		if resp.Err != nil {
			return ui.Fail(resp.Err)
		}
		return msg.Status{Text: "triggered " + fnName + "  " + strconv.Itoa(resp.Status)}
	}
}

func (m *Model) boundFunction() string {
	discovered, _ := funcs.Discover(m.deps.Stack.Cfg.FunctionAppDir())
	for _, f := range discovered {
		if strings.EqualFold(f.Trigger, "queueTrigger") && f.Target == m.queue {
			return f.Name
		}
	}
	return ""
}

func (m *Model) View() string {
	if m.w <= 0 {
		return ""
	}
	watch := ""
	if m.watch {
		watch = "  👁 watch"
	}

	if !ui.Wide(m.w) {
		if m.focus == paneSide {
			body := m.box(m.side.Title(), m.side.View(), true)
			return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
		}
		// Table body and preview body were sized in Resize() to together
		// fit inside ONE box's inner area, so just stack them and let the
		// single box() wrap the combined content.
		combined := m.table.View() + "\n" + m.prev.View()
		body := m.box(m.queue+watch, combined, m.focus == paneList)
		return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
	}

	// Reuse the layout computed once in Resize(); never recompute sideW/
	// listH/prevH here, or they can drift out of sync with what the child
	// components were actually sized to.
	left := m.box(m.side.Title(), m.side.View(), m.focus == paneSide)
	right := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(m.w-m.sideW).Height(m.listH).
			Render(m.box(m.queue+watch, m.table.View(), m.focus == paneList)),
		lipgloss.NewStyle().Width(m.w-m.sideW).Height(m.prevH).
			Render(m.box("Message", m.prev.View(), m.focus == panePrev)),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.sideW).Height(m.h).Render(left),
		right,
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

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

type queuesMsg []storage.QueueInfo
type messagesMsg []storage.Message
type watchTick struct{}
