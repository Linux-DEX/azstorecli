package blob

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/components/datatable"
	"github.com/Linux-DEX/azstorecli/internal/components/preview"
	"github.com/Linux-DEX/azstorecli/internal/components/treepane"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/storage"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

const previewHead = 256 << 10

// Deps is the slice of services this screen needs.
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

// Model is the blob explorer.
type Model struct {
	deps   Deps
	w, h   int
	focus  int
	side   *treepane.Model
	table  *datatable.Model
	prev   *preview.Model
	filter textinput.Model

	container string
	prefix    string
	entries   []storage.BlobEntry
	marker    string
	filtering bool
	sortCol   int

	// cached layout, computed once in layout() and reused by View() so the
	// sizes handed to child components always match the sizes used to
	// compose the final frame.
	sideW, listH, prevH int
}

// New builds the explorer.
func New(deps Deps) *Model {
	side := treepane.New(deps.Theme, "Containers")
	side.SetEmptyText("no containers — n to create")
	tbl := datatable.New(deps.Theme, []datatable.Column{
		{Title: "NAME", Flex: true, MinWidth: 16},
		{Title: "SIZE", Width: 9, Right: true},
		{Title: "TYPE", Width: 6},
		{Title: "MODIFIED", Width: 12},
	})
	tbl.Multi = true
	tbl.SetEmptyText("empty prefix")
	ti := textinput.New()
	ti.Placeholder = "prefix filter"
	ti.CharLimit = 200
	ti.Width = 40
	return &Model{
		deps: deps, side: side, table: tbl, prev: preview.New(deps.Theme), filter: ti,
	}
}

func (m *Model) Init() tea.Cmd { return m.loadContainers() }

func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	m.layout()
}

// frameSize returns how many columns/rows pane() adds on top of a child
// component's raw body: the border+padding cost of the pane style, plus
// one row for the "title\n" header line that pane() prepends. Deriving
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) layout() {
	if m.w <= 0 || m.h <= 0 {
		return
	}
	hFrame, vFrame := m.frameSize()

	if ui.Wide(m.w) {
		m.sideW = clamp(m.w/4, 14, 28)
		m.prevH = clamp(m.h/3, 4, 10)
		m.listH = m.h - m.prevH

		m.side.SetSize(max(1, m.sideW-hFrame), max(1, m.h-vFrame))
		m.table.SetSize(max(1, m.w-m.sideW-hFrame), max(1, m.listH-vFrame))
		m.prev.SetSize(max(1, m.w-m.sideW-hFrame), max(1, m.prevH-vFrame))
	} else {
		m.sideW, m.listH, m.prevH = 0, 0, 0
		m.side.SetSize(max(1, m.w-hFrame), max(1, m.h-vFrame))
		m.table.SetSize(max(1, m.w-hFrame), max(1, m.h-vFrame))
		m.prev.SetSize(max(1, m.w-hFrame), max(1, m.h-vFrame))
	}
}

func (m *Model) Scope() string { return keymap.ScopeBlob }

func (m *Model) ShortHelp() []key.Binding {
	return m.deps.Keys.Bindings("blob.upload", "blob.download", "blob.delete", "blob.new", "blob.sas")
}

func (m *Model) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case containersMsg:
		items := make([]treepane.Item, 0, len(v))
		for _, c := range v {
			items = append(items, treepane.Item{Name: c.Name, Badge: c.Access})
		}
		m.side.SetItems(items)
		if m.container == "" && len(v) > 0 {
			m.container = v[0].Name
			return m, m.loadBlobs()
		}
		return m, nil
	case blobsMsg:
		m.entries, m.marker = v.Entries, v.Marker
		m.fillTable()
		return m, m.previewCurrent()
	case previewMsg:
		m.prev.SetContent(v.name, v.data, v.trunc)
		return m, nil
	case msg.ProfileChanged, msg.Refresh, msg.SnapshotComplete:
		return m, m.loadContainers()
	case msg.FocusBlob:
		m.container, m.prefix = v.Container, v.Prefix
		m.side.Select(v.Container)
		return m, m.loadBlobs()
	case msg.ModalResult:
		return m, m.onModal(v)
	case msg.Edited:
		return m, m.onEdited(v)
	case tea.KeyMsg:
		if m.filtering {
			return m, m.handleFilter(v)
		}
		return m, m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleFilter(k tea.KeyMsg) tea.Cmd {
	if k.String() == "esc" || k.String() == "enter" {
		m.filtering = false
		m.filter.Blur()
		m.prefix = strings.TrimSpace(m.filter.Value())
		if m.prefix != "" && !strings.HasSuffix(m.prefix, "/") {
			// a typed prefix is a server-side starts-with, not a directory
		}
		return m.loadBlobs()
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	return cmd
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	km := m.deps.Keys
	if km.Matches(k, "app.next_pane") {
		m.focus = (m.focus + 1) % 3
		m.syncFocus()
		return nil
	}
	if km.Matches(k, "app.prev_pane") {
		m.focus = (m.focus + 2) % 3
		m.syncFocus()
		return nil
	}

	if n := ui.ResolveNav(km, k); n != ui.NavNone {
		if !ui.Wide(m.w) {
			return m.navNarrow(n)
		}
		switch m.focus {
		case paneSide:
			if n == ui.NavRight || n == ui.NavSelect {
				m.focus = paneList
				m.syncFocus()
				if it, ok := m.side.Current(); ok && it.Name != m.container {
					m.container, m.prefix = it.Name, ""
					return m.loadBlobs()
				}
				return nil
			}
			m.side.Move(n)
			if it, ok := m.side.Current(); ok && it.Name != m.container {
				m.container, m.prefix = it.Name, ""
				return m.loadBlobs()
			}
		case paneList:
			if n == ui.NavLeft {
				m.focus = paneSide
				m.syncFocus()
				return nil
			}
			if n == ui.NavSelect {
				return m.open()
			}
			m.table.Move(n)
			return m.previewCurrent()
		case panePrev:
			if n == ui.NavLeft {
				m.focus = paneList
				m.syncFocus()
				return nil
			}
			m.prev.Scroll(n)
		}
		return nil
	}

	switch {
	case km.Matches(k, "app.filter"):
		m.filtering = true
		m.filter.SetValue(m.prefix)
		m.filter.Focus()
		return nil
	case km.Matches(k, "blob.up_level"):
		m.prefix = storage.ParentPrefix(m.prefix)
		return m.loadBlobs()
	case km.Matches(k, "blob.multi_select"):
		m.table.ToggleSelect()
		return nil
	case km.Matches(k, "blob.select_all"):
		m.table.SelectAll()
		return nil
	case km.Matches(k, "blob.upload"):
		return m.ask("upload", "Upload file", "local path")
	case km.Matches(k, "blob.upload_dir"):
		return m.ask("upload_dir", "Upload directory", "local directory")
	case km.Matches(k, "blob.download"):
		return m.download()
	case km.Matches(k, "blob.delete"):
		return m.confirmDelete()
	case km.Matches(k, "blob.new"):
		if m.focus == paneSide {
			return m.ask("new_container", "New container", "name")
		}
		return m.ask("new_dir", "New virtual directory", "prefix (e.g. archive/)")
	case km.Matches(k, "blob.properties"):
		return m.showProps()
	case km.Matches(k, "blob.sas"):
		return m.sas()
	case km.Matches(k, "app.yank"):
		return m.yankURL()
	case km.Matches(k, "blob.yank_content"):
		return m.yankContent()
	case km.Matches(k, "blob.edit"):
		return m.edit()
	case km.Matches(k, "blob.copy"):
		return m.ask("copy", "Copy blob to", "container/name")
	case km.Matches(k, "blob.move"):
		return m.ask("move", "Move / rename", "container/name")
	case km.Matches(k, "blob.tier"):
		return m.cycleTier()
	case km.Matches(k, "blob.hexview"):
		m.prev.ToggleHex()
		return nil
	case km.Matches(k, "blob.versions"):
		return m.versions()
	case km.Matches(k, "blob.break_lease"):
		return m.breakLease()
	case km.Matches(k, "blob.deep_search"):
		return m.ask("search", "Search blobs", "name contains")
	case km.Matches(k, "blob.sort"):
		m.sortCol = (m.sortCol + 1) % 4
		m.table.SetSort(m.sortCol)
		return nil
	case km.Matches(k, "blob.load_full"):
		return m.previewFull()
	}
	return nil
}

func (m *Model) navNarrow(n ui.Nav) tea.Cmd {
	switch m.focus {
	case paneSide:
		if n == ui.NavRight || n == ui.NavSelect {
			if it, ok := m.side.Current(); ok {
				m.container, m.prefix = it.Name, ""
				m.focus = paneList
				m.syncFocus()
				return m.loadBlobs()
			}
		}
		m.side.Move(n)
	case paneList:
		if n == ui.NavLeft {
			m.focus = paneSide
			m.syncFocus()
			return nil
		}
		if n == ui.NavSelect {
			return m.open()
		}
		m.table.Move(n)
		return m.previewCurrent()
	case panePrev:
		if n == ui.NavLeft {
			m.focus = paneList
			m.syncFocus()
			return nil
		}
		m.prev.Scroll(n)
	}
	return nil
}

func (m *Model) syncFocus() {
	m.side.Focused = m.focus == paneSide
	m.table.Focused = m.focus == paneList
}

func (m *Model) open() tea.Cmd {
	e, ok := m.current()
	if !ok {
		return nil
	}
	if e.IsDir {
		m.prefix = e.Name
		return m.loadBlobs()
	}
	if !ui.Wide(m.w) {
		m.focus = panePrev
		m.syncFocus()
	}
	return m.previewCurrent()
}

func (m *Model) current() (storage.BlobEntry, bool) {
	row, ok := m.table.Current()
	if !ok {
		return storage.BlobEntry{}, false
	}
	for _, e := range m.entries {
		if e.Name == row.ID {
			return e, true
		}
	}
	return storage.BlobEntry{}, false
}

func (m *Model) selectedBlobs() []storage.BlobEntry {
	ids := m.table.Selected()
	var out []storage.BlobEntry
	for _, e := range m.entries {
		if e.IsDir {
			continue
		}
		for _, id := range ids {
			if e.Name == id {
				out = append(out, e)
			}
		}
	}
	return out
}

func (m *Model) fillTable() {
	rows := make([]datatable.Row, 0, len(m.entries))
	for _, e := range m.entries {
		size, kind := "—", "dir"
		mod := ""
		if !e.IsDir {
			size, kind = util.Bytes(e.Size), e.BlobType
			mod = util.RelTime(e.LastModified)
		}
		name := e.Display
		if e.Leased {
			name += " 🔒"
		}
		rows = append(rows, datatable.Row{ID: e.Name, Cells: []string{name, size, kind, mod}, Dim: e.IsDir})
	}
	m.table.SetRows(rows)
}

func (m *Model) loadContainers() tea.Cmd {
	c := m.deps.Stack.Clients
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		list, err := c.ListContainers(ctx)
		if err != nil {
			return ui.Fail(err)
		}
		return containersMsg(list)
	}
}

func (m *Model) loadBlobs() tea.Cmd {
	c, container, prefix := m.deps.Stack.Clients, m.container, m.prefix
	page := int32(m.deps.Stack.Cfg.UI.PageSize)
	return func() tea.Msg {
		if container == "" {
			return blobsMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		page, err := c.ListBlobs(ctx, container, prefix, "", page)
		if err != nil {
			return ui.Fail(err)
		}
		return blobsMsg(page)
	}
}

func (m *Model) previewCurrent() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		m.prev.Clear()
		return nil
	}
	return m.fetchPreview(e.Name, false)
}

func (m *Model) previewFull() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	return m.fetchPreview(e.Name, true)
}

func (m *Model) fetchPreview(name string, full bool) tea.Cmd {
	c, container := m.deps.Stack.Clients, m.container
	var limit int64 = previewHead
	if full {
		limit = 8 << 20
	} else if e, ok := m.current(); ok && e.Size > 0 && e.Size <= m.deps.Stack.Cfg.UI.PreviewLimitBytes {
		limit = e.Size
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		data, trunc, err := c.ReadRange(ctx, container, name, limit)
		if err != nil {
			return ui.Fail(err)
		}
		return previewMsg{name: name, data: data, trunc: trunc}
	}
}

func (m *Model) ask(action, title, prompt string) tea.Cmd {
	return func() tea.Msg {
		return msg.OpenModal{Kind: msg.ModalInput, Title: title, Prompt: prompt, Action: action}
	}
}

func (m *Model) confirmDelete() tea.Cmd {
	sel := m.selectedBlobs()
	if m.focus == paneSide {
		if m.container == "" {
			return nil
		}
		return func() tea.Msg {
			return msg.OpenModal{
				Kind: msg.ModalConfirm, Title: "Delete container " + m.container + "?",
				Body: "This cannot be undone.", Destructive: true, Action: "delete_container",
			}
		}
	}
	if len(sel) == 0 {
		return nil
	}
	var names []string
	for _, e := range sel {
		names = append(names, e.Name)
	}
	return func() tea.Msg {
		return msg.OpenModal{
			Kind: msg.ModalConfirm, Title: "Delete " + strconv.Itoa(len(sel)) + " blob(s)?",
			Body: strings.Join(names, "\n"), Destructive: true, Action: "delete_blobs",
		}
	}
}

func (m *Model) onModal(r msg.ModalResult) tea.Cmd {
	if !r.OK {
		return nil
	}
	c := m.deps.Stack.Clients
	container := m.container
	switch r.Action {
	case "new_container":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.CreateContainer(ctx, r.Value, ""); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "new_dir":
		m.prefix = storage.JoinPrefix(m.prefix, strings.TrimSuffix(r.Value, "/")+"/")
		return m.loadBlobs()
	case "upload":
		name := storage.JoinPrefix(m.prefix, filepath.Base(r.Value))
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := c.Upload(ctx, container, name, r.Value, nil); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "uploaded " + name}
		}
	case "upload_dir":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := c.UploadDir(ctx, container, m.prefix, r.Value, nil); err != nil {
				return ui.Fail(err)
			}
			return msg.Status{Text: "uploaded directory"}
		}
	case "delete_container":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.DeleteContainer(ctx, container); err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "delete_blobs":
		sel := m.selectedBlobs()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			for _, e := range sel {
				if err := c.DeleteBlob(ctx, container, e.Name); err != nil {
					return ui.Fail(err)
				}
			}
			return msg.Refresh{}
		}
	case "copy", "move":
		e, ok := m.current()
		if !ok {
			return nil
		}
		dstC, dstN := splitDest(r.Value, container)
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var err error
			if r.Action == "move" {
				err = c.MoveBlob(ctx, container, e.Name, dstC, dstN)
			} else {
				err = c.CopyBlob(ctx, container, e.Name, dstC, dstN)
			}
			if err != nil {
				return ui.Fail(err)
			}
			return msg.Refresh{}
		}
	case "search":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			hits, err := c.SearchBlobs(ctx, r.Value, 200)
			if err != nil {
				return ui.Fail(err)
			}
			return blobsMsg{Entries: hits}
		}
	}
	return nil
}

func (m *Model) download() tea.Cmd {
	sel := m.selectedBlobs()
	if len(sel) == 0 {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	dest := util.DownloadsDir()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		for _, e := range sel {
			dst := storage.LocalDownloadPath(dest, e.Name)
			if _, err := c.DownloadToFile(ctx, container, e.Name, dst); err != nil {
				return ui.Fail(err)
			}
		}
		return msg.Status{Text: "downloaded to " + dest}
	}
}

func (m *Model) showProps() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p, err := c.Properties(ctx, container, e.Name)
		if err != nil {
			return ui.Fail(err)
		}
		var b strings.Builder
		b.WriteString("size " + util.Bytes(p.Size) + "  type " + p.ContentType + "  tier " + p.Tier + "\n")
		b.WriteString("etag " + p.ETag + "  lease " + p.LeaseStatus + "\n")
		for k, v := range p.Metadata {
			b.WriteString(k + "=" + v + "\n")
		}
		return msg.OpenModal{Kind: msg.ModalNotice, Title: e.Name, Body: b.String()}
	}
}

func (m *Model) sas() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		url, err := c.BlobSAS(container, e.Name, storage.SASOptions{Read: true, Expiry: time.Hour})
		if err != nil {
			return ui.Fail(err)
		}
		_ = util.Yank(url)
		return msg.Status{Text: "SAS URL copied (1h read)"}
	}
}

func (m *Model) yankURL() tea.Cmd {
	e, ok := m.current()
	if !ok {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		u, err := c.URL(container, e.Name)
		if err != nil {
			return ui.Fail(err)
		}
		if err := util.Yank(u); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "yanked URL"}
	}
}

func (m *Model) yankContent() tea.Cmd {
	if raw := m.prev.Raw(); len(raw) > 0 {
		_ = util.Yank(string(raw))
		return func() tea.Msg { return msg.Status{Text: "yanked content"} }
	}
	return nil
}

func (m *Model) edit() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	data := m.prev.Raw()
	name := e.Name
	return func() tea.Msg {
		edited, err := util.EditTemp(filepath.Ext(name), string(data))
		return msg.Edited{Kind: "blob", Content: edited, Err: err}
	}
}

func (m *Model) onEdited(e msg.Edited) tea.Cmd {
	if e.Kind != "blob" || e.Err != nil {
		if e.Err != nil {
			return func() tea.Msg { return ui.Fail(e.Err) }
		}
		return nil
	}
	cur, ok := m.current()
	if !ok {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.WriteAll(ctx, container, cur.Name, []byte(e.Content), storage.GuessContentType(cur.Name)); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "uploaded " + cur.Name}
	}
}

func (m *Model) cycleTier() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	next := "Hot"
	switch strings.ToLower(e.Tier) {
	case "hot":
		next = "Cool"
	case "cool":
		next = "Archive"
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.SetTier(ctx, container, e.Name, next); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "tier " + next}
	}
}

func (m *Model) versions() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		list, err := c.ListVersions(ctx, container, e.Name)
		if err != nil {
			return ui.Fail(err)
		}
		return blobsMsg{Entries: list}
	}
}

func (m *Model) breakLease() tea.Cmd {
	e, ok := m.current()
	if !ok || e.IsDir {
		return nil
	}
	c, container := m.deps.Stack.Clients, m.container
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.BreakLease(ctx, container, e.Name); err != nil {
			return ui.Fail(err)
		}
		return msg.Status{Text: "lease broken"}
	}
}

func (m *Model) View() string {
	if m.w <= 0 {
		return ""
	}
	if m.filtering {
		return ui.Fit("filter: "+m.filter.View(), m.w, m.h)
	}

	title := m.container
	if m.prefix != "" {
		title += "/" + m.prefix
	}
	if title == "" {
		title = "blobs"
	}

	if !ui.Wide(m.w) {
		var body string
		switch m.focus {
		case paneSide:
			body = m.pane(m.side.Title(), m.side.View(), true)
		case panePrev:
			body = m.pane("Preview: "+m.prev.Name(), m.prev.View(), true)
		default:
			body = m.pane(title, m.table.View(), true)
		}
		// Compose with lipgloss's own Width/Height instead of ui.Fit: it is
		// ANSI/border-aware and won't chew through box-drawing characters
		// the way a naive rune-cropping Fit does on an already-bordered pane.
		return lipgloss.NewStyle().Width(m.w).Height(m.h).Render(body)
	}

	// Reuse the layout computed once in layout(); never recompute sideW/
	// listH/prevH here, or they can drift out of sync with what the child
	// components were actually sized to.
	left := m.pane(m.side.Title(), m.side.View(), m.focus == paneSide)
	rightTop := m.pane(title+"  "+m.table.ScrollInfo(), m.table.View(), m.focus == paneList)
	rightBot := m.pane("Preview: "+m.prev.Name()+"  "+m.prev.Footer(""), m.prev.View(), m.focus == panePrev)

	right := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(m.w-m.sideW).Height(m.listH).Render(rightTop),
		lipgloss.NewStyle().Width(m.w-m.sideW).Height(m.prevH).Render(rightBot),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.sideW).Height(m.h).Render(left),
		right,
	)
}

func (m *Model) pane(title, body string, active bool) string {
	style := m.deps.Theme.Pane
	if active {
		style = m.deps.Theme.PaneActive
	}
	return style.Render(m.deps.Theme.Header.Render(title) + "\n" + body)
}

func splitDest(s, defaultContainer string) (string, string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i > 0 {
		return s[:i], s[i+1:]
	}
	return defaultContainer, s
}

type containersMsg []storage.ContainerInfo
type blobsMsg storage.BlobPage
type previewMsg struct {
	name  string
	data  []byte
	trunc bool
}
