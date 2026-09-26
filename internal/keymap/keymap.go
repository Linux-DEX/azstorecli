// Package keymap defines the typed key.Binding set used by the root model
// and every screen. Bindings are grouped so bubbles/help can render a
// short and full help view without each screen hand-rolling its own
// help strings.
package keymap

import "github.com/charmbracelet/bubbles/key"

// Global holds the bindings that work on every screen, regardless of
// which screen is focused. See docs/KEYBINDINGS.md for the full
// reference this mirrors.
type Global struct {
	Quit          key.Binding
	Help          key.Binding
	CommandPalette key.Binding
	NextScreen    key.Binding
	PrevScreen    key.Binding
}

// DefaultGlobal returns the built-in global keymap. User overrides are
// merged on top of this by load.go once it exists (M7).
func DefaultGlobal() Global {
	return Global{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		CommandPalette: key.NewBinding(
			key.WithKeys(":"),
			key.WithHelp(":", "command palette"),
		),
		NextScreen: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next screen"),
		),
		PrevScreen: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev screen"),
		),
	}
}

// ShortHelp satisfies help.KeyMap so bubbles/help can render the
// abbreviated help bar directly from a Global value.
func (g Global) ShortHelp() []key.Binding {
	return []key.Binding{g.Help, g.CommandPalette, g.NextScreen, g.Quit}
}

// FullHelp satisfies help.KeyMap for the expanded help overlay (M7).
func (g Global) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{g.Quit, g.Help},
		{g.CommandPalette},
		{g.NextScreen, g.PrevScreen},
	}
}
