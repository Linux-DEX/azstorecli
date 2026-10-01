package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Linux-DEX/azstorecli/internal/msg"
)

type capture struct{ got tea.Msg }

func (c *capture) Init() tea.Cmd { return func() tea.Msg { return "from-init" } }
func (c *capture) Update(m tea.Msg) (tea.Model, tea.Cmd) {
	c.got = m
	if m == "from-init" {
		return c, func() tea.Msg { return "follow-up" }
	}
	return c, nil
}
func (c *capture) View() string { return "" }

func TestInitResultStaysOnItsScreen(t *testing.T) {
	r := newRouter()
	dash := &capture{}
	blob := &capture{}
	r.register(msg.ScreenDashboard, dash)
	r.register(msg.ScreenBlob, blob)
	r.focus(msg.ScreenDashboard)

	if r.initAll() == nil {
		t.Fatal("expected init cmds")
	}

	var gotBlob bool
	for id, model := range r.screens {
		b := bind(id, model.Init())().(boundMsg)
		follow := r.deliver(b)
		if b.id != id {
			t.Fatalf("bound to %s, want %s", b.id, id)
		}
		if id != msg.ScreenBlob {
			continue
		}
		gotBlob = true
		if blob.got != "from-init" {
			t.Fatalf("blob got %#v", blob.got)
		}
		next := follow().(boundMsg)
		if next.id != msg.ScreenBlob || next.msg != "follow-up" {
			t.Fatalf("follow-up %#v", next)
		}
		r.deliver(next)
		if blob.got != "follow-up" {
			t.Fatalf("blob follow-up %#v", blob.got)
		}
	}
	if !gotBlob {
		t.Fatal("blob screen was not initialised")
	}
}
