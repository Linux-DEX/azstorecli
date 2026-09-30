package helpoverlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestViewListsScreenAndGlobalKeys(t *testing.T) {
	m := New(theme.Dark(), keymap.Defaults())
	m.Toggle()
	got := m.View("blob", 100, 40)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Upload") || !strings.Contains(plain, "blob") {
		t.Fatalf("missing screen keys:\n%s", plain)
	}
	if !strings.Contains(plain, "global") || !strings.Contains(plain, "keybindings") {
		t.Fatalf("missing global keys:\n%s", plain)
	}
	lines := strings.Split(got, "\n")
	width := lipgloss.Width(lines[0])
	for _, line := range lines[1:] {
		if lipgloss.Width(line) != width {
			t.Fatalf("dialog width %d, line %d", width, lipgloss.Width(line))
		}
	}
}

func TestColumnsAlign(t *testing.T) {
	km := keymap.Defaults()
	var local []keymap.Action
	for _, a := range keymap.ActionsFor("dashboard") {
		if a.Scope != keymap.ScopeGlobal {
			local = append(local, a)
		}
	}
	sep := " │ "
	lines := formatEntries(theme.Dark(), km, local, 10, 48, sep)
	if len(lines) < 2 {
		t.Fatalf("rows %d", len(lines))
	}
	bar0 := strings.Index(stripANSI(lines[0]), "│")
	bar1 := strings.Index(stripANSI(lines[1]), "│")
	if bar0 < 10 || bar0 != bar1 {
		t.Fatalf("separator at %d and %d\n%s\n%s", bar0, bar1, stripANSI(lines[0]), stripANSI(lines[1]))
	}
}

func TestUpdateClosesAndScrolls(t *testing.T) {
	m := New(theme.Dark(), keymap.Defaults())
	m.Toggle()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.offset != 1 {
		t.Fatalf("offset %d", m.offset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.Opened() {
		t.Fatal("esc should close")
	}
}

func TestPlaceKeepsBackground(t *testing.T) {
	bg := strings.Repeat(".", 10) + "\n" + strings.Repeat(".", 10)
	out := Place(bg, "AB", 10, 2)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines %d", len(lines))
	}
	if lines[0] != "....AB...." || lines[1] != ".........." {
		t.Fatalf("%q", lines)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && s[i] != 'm' {
			i++
		}
	}
	return b.String()
}
