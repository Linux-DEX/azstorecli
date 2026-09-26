package app

import (
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

// Deps is the dependency container passed to every screen constructor.
// It exists so screens depend on a small, explicit set of services
// instead of reaching into globals or into each other. As real services
// arrive (supervisor in M2, storage facade in M3, ...) they get added
// here as interfaces, not concrete types, so screens stay testable.
type Deps struct {
	Config config.Config
	Theme  theme.Theme
	Keys   keymap.Global

	// Supervisor supervisor.Supervisor  // added in M2
	// Storage    storage.Client         // added in M3
}

// NewDeps builds the dependency container for a run of the TUI.
func NewDeps(cfg config.Config) Deps {
	return Deps{
		Config: cfg,
		Theme:  theme.Dark(), // theme.Load(cfg.UI.Theme) once M7 lands
		Keys:   keymap.DefaultGlobal(),
	}
}
