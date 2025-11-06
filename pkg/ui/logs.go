package ui

import (
	"time"

	"github.com/awesome-gocui/gocui"
	"github.com/Linux-DEX/azstorecli/pkg/storage"
)

// --- Logs ---
func toggleLogs(g *gocui.Gui, v *gocui.View) error {
	showLogs = !showLogs

	// The focusSide controls how J/K/Enter behave
	if showLogs {
		focusSide = "logs"
	} else {
		// When logs are hidden, revert to left panel focus
		focusSide = "left"
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func reattachLogs(g *gocui.Gui, v *gocui.View) error {
	id := storage.GetAzuriteContainerID()
	newChan, _ := storage.AttachLogs(id)
	logChan = newChan
	logsBuf = []string{}
	return nil
}

// --- Logs listener ---
func listenLogs(g *gocui.Gui) {
	for {
		select {
		case <-done:
			return
		case line, ok := <-logChan:
			if !ok {
				return
			}
			logsBuf = append(logsBuf, line)
			if len(logsBuf) > 500 {
				logsBuf = logsBuf[len(logsBuf)-500:]
			}
			if showLogs {
				g.UpdateAsync(func(gui *gocui.Gui) error { return nil })
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// --- Logs scrolling ---
// These functions now target the "right" view
func scrollLogsUp(g *gocui.Gui) error {
	v, _ := g.View("right") // Target "right" view
	if v == nil || !showLogs {
		return nil
	}
	ox, oy := v.Origin()
	if oy > 0 {
		v.SetOrigin(ox, oy-1)
	}
	return nil
}

func scrollLogsDown(g *gocui.Gui) error {
	v, _ := g.View("right") // Target "right" view
	if v == nil || !showLogs {
		return nil
	}
	ox, oy := v.Origin()
	v.SetOrigin(ox, oy+1)
	return nil
}

func scrollLogsUpPage(g *gocui.Gui, v *gocui.View) error {
	if !showLogs {
		return nil
	}
	for i := 0; i < 5; i++ {
		scrollLogsUp(g)
	}
	return nil
}

func scrollLogsDownPage(g *gocui.Gui, v *gocui.View) error {
	if !showLogs {
		return nil
	}
	for i := 0; i < 5; i++ {
		scrollLogsDown(g)
	}
	return nil
}
