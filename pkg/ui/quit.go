package ui

import (
	"github.com/awesome-gocui/gocui"
	"github.com/Linux-DEX/azstorecli/pkg/storage"
)

// --- Quit ---
func quit(g *gocui.Gui, v *gocui.View) error {
	if done != nil {
		close(done)
	}
	storage.StopAzurite()
	return gocui.ErrQuit
}
