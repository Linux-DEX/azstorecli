package snapshots

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
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
	list  []azurite.Snapshot
	busy  string
}

func New(deps Deps) *Model {
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "NAME", Flex: true, MinWidth: 14},
		{Title: "CREATED", Width: 12},
		{Title: "SIZE", Width: 9, Right: true},
	})
	return &Model{deps: deps, table: tbl}
}

func (m *Model) Init() tea.Cmd { return m.reload() }
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.layout()
}
func (m *Model) Scope() string { return keymap.ScopeSnapshots }
func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("snapshot.save", "snapshot.restore", "snapshot.delete", "snapshot.export")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case listMsg:
		m.list = v
		m.busy = ""
		m.fill()
		return m, nil
	case msg.SnapshotComplete, msg.Refresh:
		return m, m.reload()
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
			return m.confirmRestore()
		}
		m.table.Move(n)
		return nil
	}
	switch {
	case km.Matches(k, "snapshot.save"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "New snapshot", Prompt: "name", Action: "save"}
		}
	case km.Matches(k, "snapshot.restore"):
		return m.confirmRestore()
	case km.Matches(k, "snapshot.delete"):
		s, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalConfirm, Title: "Delete snapshot " + s.Name + "?", Destructive: true, Action: "delete"}
		}
	case km.Matches(k, "snapshot.rename"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Rename snapshot", Prompt: "new name", Action: "rename"}
		}
	case km.Matches(k, "snapshot.export"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Export to", Prompt: "path", Action: "export"}
		}
	case km.Matches(k, "snapshot.import"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Import archive", Prompt: "path to .tar.zst", Action: "import"}
		}
	case km.Matches(k, "snapshot.verify"):
		return m.verify()
	case km.Matches(k, "app.yank"):
		if s, ok := m.current(); ok {
			_ = util.Yank(s.Archive(m.deps.Stack.Snaps.Dir()))
			return func() tea.Msg { return msg.Status{Text: "yanked archive path"} }
		}
	}
	return nil
}

func (m *Model) fill() {
	rows := make([]datatable.Row, 0, len(m.list))
	for _, s := range m.list {
		rows = append(rows, datatable.Row{
			ID:    s.ID,
			Cells: []string{s.Name, util.RelTime(s.CreatedAt), util.Bytes(s.SizeBytes)},
		})
	}
	m.table.SetRows(rows)
}

func (m *Model) current() (azurite.Snapshot, bool) {
	row, ok := m.table.Current()
	if !ok {
		return azurite.Snapshot{}, false
	}
	for _, s := range m.list {
		if s.ID == row.ID {
			return s, true
		}
	}
	return azurite.Snapshot{}, false
}

func (m *Model) confirmRestore() tea.Cmd {
	s, ok := m.current()
	if !ok {
		return nil
	}
	warn := ""
	if s.VersionSkew(m.deps.Stack.Version) {
		warn = "This snapshot was made by Azurite " + s.Azurite + "; current is " + m.deps.Stack.Version + ".\nLokiJS schema drift can make a restore load but behave oddly.\n"
	}
	return func() tea.Msg {
		return msg.OpenModal{
			Kind: msg.ModalConfirm, Title: "Restore " + s.Name + "?",
			Body: warn + "Current workspace will be autosaved first.", Destructive: true, Action: "restore",
		}
	}
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	store, ws := m.deps.Stack.Snaps, m.deps.Stack.WS
	switch r.Action {
	case "save":
		m.busy = "Snapshotting — Azurite will restart"
		name := r.Value
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			started := time.Now()
			snap, err := store.Save(ctx, ws, name, "", nil)
			if err != nil {
				return ui.Fail(err)
			}
			return msg.SnapshotComplete{Name: snap.Name, Bytes: snap.SizeBytes, Duration: time.Since(started)}
		}
	case "restore":
		s, ok := m.current()
		if !ok {
			return nil
		}
		m.busy = "Restoring " + s.Name
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := store.Restore(ctx, ws, s.ID, nil); err != nil {
				return ui.Fail(err)
			}
			return msg.SnapshotComplete{Name: s.Name, Restored: true}
		}
	case "delete":
		s, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			if err := store.Delete(s.ID); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "rename":
		s, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			if err := store.Rename(s.ID, r.Value, s.Notes); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "export":
		s, ok := m.current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := store.Export(ctx, s.ID, r.Value); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "exported " + r.Value}
		}
	case "import":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if _, err := store.Import(ctx, r.Value); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	}
	return nil
}

func (m *Model) verify() tea.Cmd {
	s, ok := m.current()
	if !ok {
		return nil
	}
	store := m.deps.Stack.Snaps
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := store.Verify(ctx, &s); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "checksum verified"}
	}
}

func (m *Model) reload() tea.Cmd {
	store := m.deps.Stack.Snaps
	return func() tea.Msg {
		list, err := store.List()
		if err != nil {
			return ui.Fail(err)
		}
		return listMsg(list)
	}
}

func (m *Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	leftW, rightW, leftH, rightH := m.split()
	left := m.pane("Snapshots", m.table.View(), leftW, leftH, true)
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
	lines := []string{
		m.kv(width, "saved", strconv.Itoa(len(m.list))),
		m.kv(width, "total", util.Bytes(m.deps.Stack.Snaps.TotalSize())),
	}
	s, ok := m.current()
	if !ok {
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "",
		m.kv(width, "name", s.Name),
		m.kv(width, "created", util.RelTime(s.CreatedAt)),
		m.kv(width, "size", util.Bytes(s.SizeBytes)),
		m.kv(width, "project", s.Project),
		m.kv(width, "azurite", s.Azurite),
	)
	if s.Checksum != "" {
		lines = append(lines, m.kv(width, "checksum", s.Checksum))
	}
	if s.Notes != "" {
		lines = append(lines, m.kv(width, "notes", s.Notes))
	}
	if len(s.Services) > 0 {
		lines = append(lines, m.kv(width, "services", strings.Join(s.Services, ", ")))
	}
	if s.VersionSkew(m.deps.Stack.Version) {
		lines = append(lines, m.deps.Theme.StatusWarn.Render("version skew"))
	}
	if m.busy != "" {
		lines = append(lines, "", m.deps.Theme.StatusWarn.Render(m.busy))
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

type listMsg []azurite.Snapshot
