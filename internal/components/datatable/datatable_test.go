package datatable

import (
	"strings"
	"testing"

	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestColumnsStayBesideTheName(t *testing.T) {
	m := New(theme.Dark(), []Column{
		{Title: "NAME", Flex: true, MinWidth: 16},
		{Title: "SIZE", Width: 9, Right: true},
		{Title: "TYPE", Width: 6},
		{Title: "MODIFIED", Width: 12},
	})
	m.SetSize(100, 6)
	m.SetRows([]Row{{ID: "1", Cells: []string{"README.md", "7.4 kB", "Block", "1 minute ago"}}})
	view := m.View()
	if !strings.Contains(view, "1 minute ago") {
		t.Fatalf("modified value clipped:\n%s", view)
	}
	if !strings.Contains(view, "7.4 kB") || !strings.Contains(view, "Block") {
		t.Fatalf("columns missing:\n%s", view)
	}
	// The name must not be padded out across the pane.
	if strings.Contains(view, "README.md"+"                    ") {
		t.Fatalf("name column still eats the row:\n%s", view)
	}
}

func TestLongNameWrapsAtHalfPane(t *testing.T) {
	m := New(theme.Dark(), []Column{
		{Title: "NAME", Flex: true},
		{Title: "SIZE"},
	})
	const pane = 100
	m.SetSize(pane, 8)
	long := strings.Repeat("a", 80)
	m.SetRows([]Row{{ID: "1", Cells: []string{long, "1 B"}}})
	view := m.View()
	half := strings.Repeat("a", pane/2)
	if strings.Contains(view, long) {
		t.Fatalf("name was not wrapped:\n%s", view)
	}
	if !strings.Contains(view, half) {
		t.Fatalf("name did not use half the pane:\n%s", view)
	}
	if !strings.Contains(view, "1 B") {
		t.Fatalf("size missing:\n%s", view)
	}
}
