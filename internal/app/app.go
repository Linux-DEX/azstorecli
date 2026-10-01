package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/components/helpbar"
	"github.com/Linux-DEX/azstorecli/internal/components/helpoverlay"
	"github.com/Linux-DEX/azstorecli/internal/components/modal"
	"github.com/Linux-DEX/azstorecli/internal/components/palette"
	"github.com/Linux-DEX/azstorecli/internal/components/statusbar"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/screens/blob"
	"github.com/Linux-DEX/azstorecli/internal/screens/dashboard"
	"github.com/Linux-DEX/azstorecli/internal/screens/functions"
	"github.com/Linux-DEX/azstorecli/internal/screens/logs"
	"github.com/Linux-DEX/azstorecli/internal/screens/profiles"
	"github.com/Linux-DEX/azstorecli/internal/screens/queue"
	"github.com/Linux-DEX/azstorecli/internal/screens/snapshots"
	"github.com/Linux-DEX/azstorecli/internal/screens/table"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
)

// RootModel is the top-level tea.Model.
type RootModel struct {
	deps     Deps
	router   *router
	status   statusbar.Model
	palette  *palette.Model
	modal    tea.Model
	help     *helpoverlay.Model
	drawer   bool
	logs     *logs.Model
	w, h     int
	quitting bool
}

// New builds the root model from an opened stack.
func New(s *stack.Stack) RootModel {
	deps := NewDeps(s)
	th, keys := s.Theme, s.Keys
	screenDeps := func() dashboard.Deps { return dashboard.Deps{Theme: th, Keys: keys, Stack: s} }

	r := newRouter()
	r.register(msg.ScreenDashboard, dashboard.New(screenDeps()))
	r.register(msg.ScreenBlob, blob.New(blob.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenQueue, queue.New(queue.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenTable, table.New(table.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenFunctions, functions.New(functions.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenLogs, logs.New(logs.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenSnapshots, snapshots.New(snapshots.Deps{Theme: th, Keys: keys, Stack: s}))
	r.register(msg.ScreenProfiles, profiles.New(profiles.Deps{Theme: th, Keys: keys, Stack: s}))

	start := msg.ScreenID(s.Cfg.UI.StartScreen)
	if start != "" {
		r.focus(start)
	} else {
		r.focus(msg.ScreenDashboard)
	}

	st := statusbar.New(th, s.Cfg.Project.Name)
	st.SetProfile(s.Clients.Profile())
	st.SetPorts(portSummary(s.Cfg))
	st.SetServices(s.Sup.Snapshot())

	return RootModel{
		deps:    deps,
		router:  r,
		status:  st,
		palette: palette.New(th, keys),
		help:    helpoverlay.New(th, keys),
		logs:    logs.New(logs.Deps{Theme: th, Keys: keys, Stack: s}),
	}
}

func (m RootModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.router.initAll(),
		m.logs.Init(),
		listenSupervisor(m.deps.Stack.Sup.Events()),
	}
	if stack.NeedsInit(m.deps.Stack.Cfg.ProjectDir()) {
		cmds = append(cmds, func() tea.Msg {
			return msg.OpenModal{
				Kind:   msg.ModalConfirm,
				Title:  "Initialise azstore in this function app?",
				Body:   "Writes .azstorecli/config.yaml, a workspace dir, and patches local.settings.json.",
				Action: "onboard",
			}
		})
	} else if len(m.deps.Stack.Cfg.Autostart) > 0 {
		cmds = append(cmds, m.autostart())
	}
	return tea.Batch(cmds...)
}

func (m RootModel) Update(teaMsg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := teaMsg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = v.Width, v.Height
		m.status.SetSize(v.Width)
		m.palette.SetWidth(v.Width)
		bodyH := m.bodyHeight()
		m.router.resizeAll(v.Width, bodyH)
		m.logs.Resize(v.Width, min(12, v.Height/3))
		return m, nil

	case tea.KeyMsg:
		if m.modal != nil {
			updated, cmd := m.modal.Update(v)
			m.modal = updated
			return m, cmd
		}
		if m.help.Opened() {
			if m.deps.Stack.Keys.Matches(v, "app.force_quit") {
				return m, m.shutdown()
			}
			m.help.Update(v)
			return m, nil
		}
		if m.palette.Focused() {
			updated, cmd := m.palette.Update(v)
			m.palette = updated.(*palette.Model)
			return m, cmd
		}
		if cmd, handled := m.handleGlobal(v); handled {
			return m, cmd
		}

	case msg.OpenModal:
		m.modal = modal.New(m.deps.Stack.Theme, v)
		return m, m.modal.Init()

	case msg.ModalResult:
		m.modal = nil
		if v.Action == "onboard" && v.OK {
			return m, m.doOnboard()
		}
		if v.Action == "save" && v.OK {
			return m, m.quickSnapshot(v.Value)
		}
		return m, m.router.updateCurrent(v)

	case msg.Action:
		return m, m.dispatchAction(v.ID)

	case msg.SwitchScreen:
		m.router.focus(v.Target)
		m.resizeBody()
		return m, nil

	case msg.FocusLogs:
		m.router.focus(msg.ScreenLogs)
		m.resizeBody()
		return m, m.router.updateCurrent(v)

	case msg.FocusQueue:
		m.router.focus(msg.ScreenQueue)
		m.resizeBody()
		return m, m.router.updateCurrent(v)

	case msg.FocusBlob:
		m.router.focus(msg.ScreenBlob)
		m.resizeBody()
		return m, m.router.updateCurrent(v)

	case msg.FocusTable:
		m.router.focus(msg.ScreenTable)
		m.resizeBody()
		return m, m.router.updateCurrent(v)

	case msg.ServiceState:
		m.status.SetService(v.Name, v.State, v.PID)
		return m, tea.Batch(m.router.updateCurrent(v), listenSupervisor(m.deps.Stack.Sup.Events()))

	case msg.Log:
		var cmds []tea.Cmd
		if m.drawer {
			updated, cmd := m.logs.Update(v)
			m.logs = updated.(*logs.Model)
			cmds = append(cmds, cmd)
		}
		if m.router.currentID() == msg.ScreenLogs {
			cmds = append(cmds, m.router.updateCurrent(v))
		}
		cmds = append(cmds, listenSupervisor(m.deps.Stack.Sup.Events()))
		return m, tea.Batch(cmds...)

	case msg.ProfileChanged:
		m.status.SetProfile(m.deps.Stack.Clients.Profile())
		return m, m.broadcast(v)

	case msg.SnapshotComplete:
		text := "saved " + v.Name
		if v.Restored {
			text = "restored " + v.Name
		}
		if v.Bytes > 0 {
			text += fmt.Sprintf(" · %d B · %s", v.Bytes, v.Duration.Truncate(time.Millisecond))
		}
		m.status.SetToast(text, false)
		return m, tea.Batch(m.broadcast(msg.SnapshotInFlight{Active: false}), m.broadcast(v))

	case msg.Error:
		m.status.SetToast(v.Err.Error(), true)
		return m, nil

	case msg.Status:
		m.status.SetToast(v.Text, v.Warning)
		return m, nil

	case msg.UnlockReadOnly:
		m.deps.Stack.Clients.Unlock(5 * time.Minute)
		m.status.SetToast("writes unlocked for 5 minutes", true)
		return m, nil

	case supervisor.Event:
		return m, tea.Batch(m.applyEvent(v), listenSupervisor(m.deps.Stack.Sup.Events()))
	}

	return m, m.router.updateCurrent(teaMsg)
}

func (m *RootModel) handleGlobal(k tea.KeyMsg) (tea.Cmd, bool) {
	km := m.deps.Stack.Keys
	switch {
	case km.Matches(k, "app.force_quit"):
		return m.shutdown(), true
	case km.Matches(k, "app.quit"):
		if m.router.back() {
			m.resizeBody()
			return nil, true
		}
		return m.shutdown(), true
	case km.Matches(k, "app.help"):
		m.help.Toggle()
		return nil, true
	case km.Matches(k, "app.palette"):
		m.palette.Open(m.router.scope())
		return nil, true
	case km.Matches(k, "app.refresh"):
		return func() tea.Msg { return msg.Refresh{} }, true
	case km.Matches(k, "app.log_drawer"):
		m.drawer = !m.drawer
		m.resizeBody()
		return nil, true
	case km.Matches(k, "app.profile_switch"):
		m.router.focus(msg.ScreenProfiles)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "app.snapshot_save"):
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Snapshot name", Prompt: "name", Action: "save"}
		}, true
	case km.Matches(k, "app.unlock"):
		return func() tea.Msg { return msg.UnlockReadOnly{} }, true
	case km.Matches(k, "screen.dashboard"):
		m.router.focus(msg.ScreenDashboard)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.blob"):
		m.router.focus(msg.ScreenBlob)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.queue"):
		m.router.focus(msg.ScreenQueue)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.table"):
		m.router.focus(msg.ScreenTable)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.functions"):
		m.router.focus(msg.ScreenFunctions)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.logs"):
		m.router.focus(msg.ScreenLogs)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.snapshots"):
		m.router.focus(msg.ScreenSnapshots)
		m.resizeBody()
		return nil, true
	case km.Matches(k, "screen.profiles"):
		m.router.focus(msg.ScreenProfiles)
		m.resizeBody()
		return nil, true
	}
	return nil, false
}

func (m *RootModel) dispatchAction(id string) tea.Cmd {
	switch id {
	case "screen.dashboard":
		m.router.focus(msg.ScreenDashboard)
	case "screen.blob":
		m.router.focus(msg.ScreenBlob)
	case "screen.queue":
		m.router.focus(msg.ScreenQueue)
	case "screen.table":
		m.router.focus(msg.ScreenTable)
	case "screen.functions":
		m.router.focus(msg.ScreenFunctions)
	case "screen.logs":
		m.router.focus(msg.ScreenLogs)
	case "screen.snapshots":
		m.router.focus(msg.ScreenSnapshots)
	case "screen.profiles":
		m.router.focus(msg.ScreenProfiles)
	case "app.snapshot_save", "snapshot.save":
		return func() tea.Msg {
			return msg.OpenModal{Kind: msg.ModalInput, Title: "Snapshot name", Prompt: "name", Action: "save"}
		}
	case "app.quit":
		if m.router.back() {
			m.resizeBody()
			return nil
		}
		return m.shutdown()
	case "app.help":
		m.help.Toggle()
		return nil
	default:
		return func() tea.Msg { return msg.Action{ID: id} }
	}
	m.resizeBody()
	return nil
}

func (m *RootModel) broadcast(v tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for id, model := range m.router.screens {
		updated, cmd := model.Update(v)
		m.router.screens[id] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *RootModel) applyEvent(e supervisor.Event) tea.Cmd {
	switch e.Kind {
	case supervisor.EventState:
		return func() tea.Msg {
			return msg.ServiceState{Name: e.Process, State: e.State, PID: e.PID, Err: e.Err}
		}
	case supervisor.EventLog:
		return func() tea.Msg { return msg.Log{Process: e.Process, Line: e.Line} }
	case supervisor.EventError:
		if e.Err != nil {
			return func() tea.Msg { return msg.Error{Err: e.Err} }
		}
	}
	return nil
}

func (m *RootModel) autostart() tea.Cmd {
	s := m.deps.Stack
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := s.Start(ctx, s.Cfg.Autostart...); err != nil {
			return msg.Error{Err: err}
		}
		return msg.Status{Text: "stack up"}
	}
}

func (m *RootModel) quickSnapshot(name string) tea.Cmd {
	s := m.deps.Stack
	if name == "" {
		name = time.Now().UTC().Format("20060102-150405")
	}
	m.status.SetToast("Snapshotting — Azurite will restart", true)
	return tea.Batch(
		func() tea.Msg { return msg.SnapshotInFlight{Active: true} },
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			started := time.Now()
			snap, err := s.Snaps.Save(ctx, s.WS, name, "", nil)
			if err != nil {
				return msg.Error{Err: err}
			}
			return msg.SnapshotComplete{Name: snap.Name, Bytes: snap.SizeBytes, Duration: time.Since(started)}
		},
	)
}

func (m *RootModel) doOnboard() tea.Cmd {
	s := m.deps.Stack
	return func() tea.Msg {
		if err := stack.InitProject(s.Cfg, s.Cfg.ProjectDir()); err != nil {
			return msg.Error{Err: err}
		}
		return msg.Status{Text: "wrote .azstorecli/config.yaml"}
	}
}

func (m *RootModel) shutdown() tea.Cmd {
	s := m.deps.Stack
	last := string(m.router.currentID())
	return func() tea.Msg {
		_ = config.SaveRecent(config.Recent{LastScreen: last, Project: s.Cfg.Project.Name, Profile: s.Profiles.Active})
		_ = s.Close()
		return tea.Quit()
	}
}

func (m *RootModel) resizeBody() {
	if m.w > 0 {
		m.router.resizeAll(m.w, m.bodyHeight())
	}
}

func (m RootModel) bodyHeight() int {
	h := m.h - 2 // status + help
	if m.drawer {
		h -= min(12, m.h/3)
	}
	if h < 4 {
		h = 4
	}
	return h
}

func (m RootModel) View() string {
	if m.w == 0 {
		return "azstorecli…"
	}
	body := ""
	if cur := m.router.current(); cur != nil {
		body = cur.View()
	}
	chrome := []string{m.status.View(), body}
	if m.drawer {
		chrome = append(chrome, m.logs.View())
	}
	chrome = append(chrome, helpbar.Render(m.deps.Stack.Theme, m.w, m.router.currentID(), m.router.shortHelp()))
	frame := lipgloss.JoinVertical(lipgloss.Left, chrome...)
	if m.modal != nil {
		return modal.Place(m.modal.View(), m.w, m.h, m.deps.Stack.Theme)
	}
	if m.palette.Focused() {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Top, m.palette.View())
	}
	if m.help.Opened() {
		return helpoverlay.Place(frame, m.help.View(m.router.scope(), m.w, m.h), m.w, m.h)
	}
	return frame
}

func listenSupervisor(events <-chan supervisor.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			return nil
		}
		return e
	}
}

func portSummary(cfg config.Config) string {
	return fmt.Sprintf("blob:%d  f:%d", cfg.Azurite.BlobPort, cfg.Functions.Port)
}
