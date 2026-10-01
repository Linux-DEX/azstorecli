package blob

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/storage"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestCreatedContainerStaysInSidebar(t *testing.T) {
	m := New(Deps{Theme: theme.Dark(), Keys: keymap.Defaults()})
	m.side.SetItems(nil)
	m.reveal = "zeta"
	if !m.applyContainers([]storage.ContainerInfo{{Name: "alpha", Access: "private"}}) {
		t.Fatal("expected blob reload")
	}
	if m.side.CurrentName() != "zeta" {
		t.Fatalf("selected %q", m.side.CurrentName())
	}
	var names []string
	for _, it := range m.side.Items() {
		names = append(names, it.Name)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("sidebar %v", names)
	}
}

func TestPreviewZoomFillsTheScreen(t *testing.T) {
	m := New(Deps{Theme: theme.Dark(), Keys: keymap.Defaults()})
	m.Resize(120, 40)
	m.prev.SetContent("readme.md", []byte("hello"), false)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	view := m.View()
	if !strings.Contains(view, "Preview:") || strings.Contains(view, "Containers") {
		t.Fatalf("preview did not fill the screen:\n%s", view)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if !strings.Contains(m.View(), "Containers") {
		t.Fatal("list did not return")
	}
}
