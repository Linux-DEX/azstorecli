package snapshots

import (
	"strings"
	"testing"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestSnapshotDetailSitsBesideTheList(t *testing.T) {
	store, err := azurite.NewStore(t.TempDir(), nil, "3.30.0")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Theme: theme.Dark(), Stack: &stack.Stack{Snaps: store, Version: "3.30.0"}})
	m.Resize(120, 24)
	m.list = []azurite.Snapshot{{
		ID: "1", Name: "seed", CreatedAt: time.Now(), SizeBytes: 128,
		Project: "demo", Azurite: "3.30.0", Checksum: "abc123", Notes: "clean",
	}}
	m.fill()
	view := m.View()
	for _, want := range []string{"Snapshots", "Selected", "seed", "checksum", "abc123", "demo"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
}
