package table

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

func TestLayoutMatchesFunctionsWidthAndFillsHeight(t *testing.T) {
	m := New(Deps{Theme: theme.Dark(), Keys: keymap.Defaults()})
	m.Resize(120, 30)
	if m.sideW != ui.SideWidth(120) {
		t.Fatalf("sidebar %d, want %d", m.sideW, ui.SideWidth(120))
	}
	m.name = "widgets"
	got := m.View()
	if h := lipgloss.Height(got); h != 30 {
		t.Fatalf("height %d, want 30", h)
	}
	right := m.box("widgets", "Query  (all)\nonly one row", true)
	if h := lipgloss.Height(right); h != 30 {
		t.Fatalf("right pane height %d, want 30", h)
	}
}
