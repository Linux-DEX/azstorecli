// Package keymap turns the action registry into bindings, merges user
// overrides over the defaults, and reports conflicts.
package keymap

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// KeyMap resolves action IDs to bindings.
type KeyMap struct {
	byAction map[string]key.Binding
	scopeOf  map[string]string
}

// Defaults builds the keymap straight from the action registry, so a
// new action is bound the moment it is registered.
func Defaults() *KeyMap {
	km := &KeyMap{
		byAction: make(map[string]key.Binding, len(registry)),
		scopeOf:  make(map[string]string, len(registry)),
	}
	for _, a := range registry {
		km.byAction[a.ID] = key.NewBinding(
			key.WithKeys(a.Keys...),
			key.WithHelp(helpKey(a.Keys), a.Desc),
		)
		km.scopeOf[a.ID] = a.Scope
	}
	return km
}

// Get returns the binding for an action. An unknown ID yields a
// disabled binding rather than a panic, so a screen referring to an
// action that was renamed degrades to "that key does nothing".
func (k *KeyMap) Get(id string) key.Binding {
	if b, ok := k.byAction[id]; ok {
		return b
	}
	b := key.NewBinding(key.WithKeys())
	b.SetEnabled(false)
	return b
}

// Matches reports whether a key event triggers an action.
func (k *KeyMap) Matches(msg tea.KeyMsg, id string) bool {
	return key.Matches(msg, k.Get(id))
}

// MatchesAny returns the first action in ids the event triggers.
func (k *KeyMap) MatchesAny(msg tea.KeyMsg, ids ...string) (string, bool) {
	for _, id := range ids {
		if k.Matches(msg, id) {
			return id, true
		}
	}
	return "", false
}

// Bindings returns the bindings for a list of action IDs, for a
// screen's help bar.
func (k *KeyMap) Bindings(ids ...string) []key.Binding {
	out := make([]key.Binding, 0, len(ids))
	for _, id := range ids {
		if b, ok := k.byAction[id]; ok {
			out = append(out, b)
		}
	}
	return out
}

// KeysFor returns the currently bound keys for an action, for the
// palette's right-hand shortcut column.
func (k *KeyMap) KeysFor(id string) []string {
	return k.Get(id).Keys()
}

// SetKeys rebinds an action.
func (k *KeyMap) SetKeys(id string, keys []string) error {
	b, ok := k.byAction[id]
	if !ok {
		return fmt.Errorf("unknown action %q — run `azstore keys list`", id)
	}
	a, _ := Lookup(id)
	b.SetKeys(keys...)
	b.SetHelp(helpKey(keys), a.Desc)
	k.byAction[id] = b
	return nil
}

// DetectConflicts reports two actions bound to the same key in the same
// scope. A global action and a screen-local one may share a key — the
// screen wins, which is how `d` is both "doctor" and "download" — but
// two actions in one scope is always a mistake.
func (k *KeyMap) DetectConflicts() error {
	type slot struct{ scope, keyName string }
	owner := map[slot]string{}

	ids := make([]string, 0, len(k.byAction))
	for id := range k.byAction {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic error message

	var problems []string
	for _, id := range ids {
		scope := k.scopeOf[id]
		for _, keyName := range k.byAction[id].Keys() {
			s := slot{scope, keyName}
			if prev, taken := owner[s]; taken {
				where := scope
				if where == ScopeGlobal {
					where = "global"
				}
				problems = append(problems, fmt.Sprintf("%s: %q is bound to both %s and %s", where, keyName, prev, id))
				continue
			}
			owner[s] = id
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("keymap conflicts:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// FullHelp groups every binding by scope for the help overlay.
func (k *KeyMap) FullHelp(scope string) [][]key.Binding {
	var global, local []key.Binding
	for _, a := range ActionsFor(scope) {
		b, ok := k.byAction[a.ID]
		if !ok || len(b.Keys()) == 0 {
			continue
		}
		if a.Scope == ScopeGlobal {
			global = append(global, b)
		} else {
			local = append(local, b)
		}
	}
	// Two balanced columns read better than one long list in a terminal
	// that is usually wider than it is tall.
	out := [][]key.Binding{}
	if len(local) > 0 {
		out = append(out, splitColumns(local, 2)...)
	}
	return append(out, splitColumns(global, 2)...)
}

func splitColumns(in []key.Binding, cols int) [][]key.Binding {
	if len(in) == 0 {
		return nil
	}
	per := (len(in) + cols - 1) / cols
	var out [][]key.Binding
	for i := 0; i < len(in); i += per {
		end := min(i+per, len(in))
		out = append(out, in[i:end])
	}
	return out
}

// helpKey renders a binding's keys for the help bar, showing at most
// two so a three-alias binding does not blow out the column.
func helpKey(keys []string) string {
	pretty := make([]string, 0, len(keys))
	for _, k := range keys {
		switch k {
		case " ":
			pretty = append(pretty, "space")
		case "enter":
			pretty = append(pretty, "↵")
		default:
			pretty = append(pretty, k)
		}
		if len(pretty) == 2 {
			break
		}
	}
	return strings.Join(pretty, "/")
}
