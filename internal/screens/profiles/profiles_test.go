package profiles

import (
	"strings"
	"testing"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestProfileDetailSitsBesideTheList(t *testing.T) {
	store := &config.ProfileStore{
		Active:   "local",
		Profiles: []config.Profile{config.AzuriteProfile("local", 10000, 10001, 10002)},
	}
	m := New(Deps{Theme: theme.Dark(), Stack: &stack.Stack{Profiles: store}})
	m.Resize(120, 24)
	m.fill()
	view := m.View()
	for _, want := range []string{"Profiles", "Selected", "local", "readonly", "10001", "10002"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
}
