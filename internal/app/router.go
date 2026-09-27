package app

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

type router struct {
	order   []msg.ScreenID
	screens map[msg.ScreenID]tea.Model
	focused msg.ScreenID
	stack   []msg.ScreenID
}

func newRouter() *router {
	return &router{screens: make(map[msg.ScreenID]tea.Model)}
}

func (r *router) register(id msg.ScreenID, model tea.Model) {
	r.screens[id] = model
	r.order = append(r.order, id)
	if r.focused == "" {
		r.focused = id
	}
}

func (r *router) focus(id msg.ScreenID) {
	if _, ok := r.screens[id]; !ok {
		return
	}
	if r.focused != "" && r.focused != id {
		r.stack = append(r.stack, r.focused)
	}
	r.focused = id
}

func (r *router) back() bool {
	if len(r.stack) == 0 {
		return false
	}
	r.focused = r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	return true
}

func (r *router) current() tea.Model { return r.screens[r.focused] }

func (r *router) currentID() msg.ScreenID { return r.focused }

func (r *router) updateCurrent(m tea.Msg) tea.Cmd {
	model, ok := r.screens[r.focused]
	if !ok {
		return nil
	}
	updated, cmd := model.Update(m)
	r.screens[r.focused] = updated
	return cmd
}

func (r *router) resizeAll(w, h int) {
	for id, model := range r.screens {
		if s, ok := model.(interface{ Resize(int, int) }); ok {
			s.Resize(w, h)
		}
		r.screens[id] = model
	}
}

func (r *router) shortHelp() []key.Binding {
	if s, ok := r.current().(ui.Screen); ok {
		return s.ShortHelp()
	}
	return nil
}

func (r *router) scope() string {
	if s, ok := r.current().(ui.Screen); ok {
		return s.Scope()
	}
	return ""
}

func (r *router) initAll() tea.Cmd {
	var cmds []tea.Cmd
	for id, model := range r.screens {
		if c := model.Init(); c != nil {
			cmds = append(cmds, c)
		}
		r.screens[id] = model
	}
	return tea.Batch(cmds...)
}
