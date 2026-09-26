package app

import tea "github.com/charmbracelet/bubbletea"

// router owns the set of routable screens and which one currently has
// focus. It does NOT know how any individual screen renders or behaves —
// it only tracks IDs and dispatches Update/View to whichever tea.Model
// is registered for the focused one.
type router struct {
	order   []ScreenID // display/tab order, dashboard first
	screens map[ScreenID]tea.Model
	focused ScreenID
}

func newRouter() *router {
	return &router{
		screens: make(map[ScreenID]tea.Model),
	}
}

// register adds a screen to the router. Screens not yet built (M3+)
// simply aren't registered, so Tab-cycling only visits real screens.
func (r *router) register(id ScreenID, model tea.Model) {
	r.screens[id] = model
	r.order = append(r.order, id)
	if r.focused == "" {
		r.focused = id
	}
}

func (r *router) focus(id ScreenID) {
	if _, ok := r.screens[id]; ok {
		r.focused = id
	}
}

func (r *router) next() {
	r.shift(1)
}

func (r *router) prev() {
	r.shift(-1)
}

func (r *router) shift(delta int) {
	if len(r.order) == 0 {
		return
	}
	idx := 0
	for i, id := range r.order {
		if id == r.focused {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(r.order)) % len(r.order)
	r.focused = r.order[idx]
}

func (r *router) current() tea.Model {
	return r.screens[r.focused]
}

// updateCurrent forwards a message to the focused screen only and stores
// its returned model back into the registry.
func (r *router) updateCurrent(msg tea.Msg) tea.Cmd {
	model, ok := r.screens[r.focused]
	if !ok {
		return nil
	}
	updated, cmd := model.Update(msg)
	r.screens[r.focused] = updated
	return cmd
}
