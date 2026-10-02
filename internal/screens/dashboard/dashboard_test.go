package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/doctor"
	"github.com/Linux-DEX/azstorecli/internal/stack"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
	"github.com/Linux-DEX/azstorecli/internal/theme"
)

func TestDashboardKeepsSectionsApart(t *testing.T) {
	cfg := config.Defaults()
	m := New(Deps{
		Theme: theme.Dark(),
		Stack: &stack.Stack{Cfg: cfg, WS: &azurite.Workspace{Dir: "/work/demo-project"}},
	})
	m.Resize(120, 30)
	m.statuses = []supervisor.Status{
		{Name: "azurite", State: "healthy", PID: 42, Uptime: time.Minute, Argv: []string{"azurite", "--silent"}, Health: "http://127.0.0.1:10000"},
		{Name: "functions", State: "stopped"},
	}
	m.ws.SizeBytes = 4096
	m.snaps = []azurite.Snapshot{{Name: "seed", CreatedAt: time.Now(), SizeBytes: 128}}
	m.detail = true
	m.doctor = []doctor.Check{{Name: "node", OK: true, Detail: "v20"}}

	view := m.View()
	for _, want := range []string{
		"Services", "Workspace", "Azurite", "Functions Host",
		"10000", "10001", "10002", "7071", "seed", "/work/demo-project", "node",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q\n%s", want, view)
		}
	}
	var portsTogether bool
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "blob") && strings.Contains(line, "queue") && strings.Contains(line, "table") {
			portsTogether = true
		}
	}
	if !portsTogether {
		t.Fatalf("ports are not on one line:\n%s", view)
	}
}
