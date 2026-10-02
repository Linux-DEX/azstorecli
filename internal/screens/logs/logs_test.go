package logs

import (
	"strings"
	"testing"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestViewStaysFullHeight(t *testing.T) {
	m := New(Deps{Theme: theme.Dark(), Keys: keymap.Defaults()})
	m.Resize(80, 20)
	if n := len(strings.Split(m.View(), "\n")); n != 20 {
		t.Fatalf("lines %d, want 20", n)
	}
	m.filtering = true
	view := m.View()
	if n := len(strings.Split(view, "\n")); n != 20 {
		t.Fatalf("filter lines %d, want 20", n)
	}
	if !strings.Contains(view, "source") || !strings.Contains(view, "state") || !strings.Contains(view, "filter") {
		t.Fatalf("status row missing:\n%s", view)
	}
}
