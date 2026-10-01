package blob

import (
	"testing"

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
