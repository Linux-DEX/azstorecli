package ui

import (
	"errors"
	"fmt"

	"github.com/Linux-DEX/azstorecli/pkg/storage"
	"github.com/awesome-gocui/gocui"
)

// --- Globals ---
var (
	logChan <-chan string
	showPopup = true
)

// --- Entry point ---
func RunApp() error {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return fmt.Errorf("failed to init gocui: %w", err)
	}
	defer g.Close()

	g.Cursor = false
	g.SetManagerFunc(layout)

	// --- Keybindings ---
	keys := []struct {
		view string
		key  interface{}
		mod  gocui.Modifier
		h    func(*gocui.Gui, *gocui.View) error
	}{
		{"", gocui.KeyCtrlC, gocui.ModNone, quit},
		{"", 'q', gocui.ModNone, quit},
		{"", 'r', gocui.ModNone, reattachLogs},
		{"", gocui.KeyArrowUp, gocui.ModNone, scrollUp},
		{"", gocui.KeyArrowDown, gocui.ModNone, scrollDown},
		{"", gocui.KeyPgup, gocui.ModNone, pageUp},
		{"", gocui.KeyPgdn, gocui.ModNone, pageDown},
		{"", gocui.KeyEsc, gocui.ModNone, closePopup},
	}
	for _, kb := range keys {
		if err := g.SetKeybinding(kb.view, kb.key, kb.mod, kb.h); err != nil {
			return err
		}
	}

	// --- Start Azurite logs ---
	logChan, _ = storage.StartAzurite()

	// --- Log listener goroutine ---
	go func() {
		for line := range logChan {
			g.Update(func(gui *gocui.Gui) error {
				v, err := gui.View("logs")
				if err != nil {
					return nil
				}

				fmt.Fprint(v, line)
				lines := len(v.BufferLines())
				_, sy := v.Size()
				_, oy := v.Origin()

				// Scroll to bottom only if already near bottom
				if oy+sy >= lines-2 {
					if lines > sy {
						v.SetOrigin(0, lines-sy)
					}
				}
				return nil
			})
		}
	}()

	// --- Main loop ---
	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		return err
	}
	return nil
}

// --- Layout ---
func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Header
	if v, err := g.SetView("header", 0, 0, maxX-1, 2, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Frame = true
		v.Title = "AZURE STORAGE LOCAL (Azurite Logs)"
		fmt.Fprintln(v, "Press [Q] to Quit | [R] to Reattach Logs")
	}

	// Logs view
	if v, err := g.SetView("logs", 0, 3, maxX-1, maxY-2, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Title = "Azurite Output"
		v.Wrap = true
		v.Autoscroll = false // manual scrolling
	}

	if showPopup {
		width := 70
		height := 20
		x0 := (maxX - width) / 2
		y0 := (maxY - height) / 2
		x1 := x0 + width
		y1 := y0 + height

		if v, err := g.SetView("popup", x0, y0, x1, y1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Title = "Welcome!"
			v.Wrap = true
			v.Frame = true

			fmt.Fprintln(v, "Welcome to azstorecli TUI for Azure Storage Explorer for Development\n")
			fmt.Fprintln(v, "Sample keybindings:")
			fmt.Fprintln(v, "  ↑ / ↓  : Scroll logs")
			fmt.Fprintln(v, "  PgUp/PgDn : Page scroll")
			fmt.Fprintln(v, "  R      : Reattach logs")
			fmt.Fprintln(v, "  Q / Ctrl+C : Quit")
			fmt.Fprintln(v, "\nPress [ESC] to close this popup.")
		}
	} else {
		g.DeleteView("popup")
	}

	return nil
}

// --- Handlers ---
func quit(g *gocui.Gui, v *gocui.View) error {
	storage.StopAzurite()
	return gocui.ErrQuit
}

func reattachLogs(g *gocui.Gui, v *gocui.View) error {
	logView, err := g.View("logs")
	if err != nil {
		return err
	}
	logView.Clear()
	fmt.Fprintln(logView, "Reattaching Azurite logs...\n")
	id := storage.GetAzuriteContainerID()
	newChan, _ := storage.AttachLogs(id)
	logChan = newChan
	return nil
}

func scrollUp(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		var err error
		v, err = g.View("logs")
		if err != nil {
			return err
		}
	}
	ox, oy := v.Origin()
	if oy > 0 {
		v.SetOrigin(ox, oy-1)
	}
	return nil
}

func scrollDown(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		var err error
		v, err = g.View("logs")
		if err != nil {
			return err
		}
	}
	ox, oy := v.Origin()
	lines := len(v.BufferLines())
	_, sy := v.Size()
	if oy+sy < lines {
		v.SetOrigin(ox, oy+1)
	}
	return nil
}

func pageUp(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		var err error
		v, err = g.View("logs")
		if err != nil {
			return err
		}
	}
	ox, oy := v.Origin()
	_, sy := v.Size()
	newY := oy - sy
	if newY < 0 {
		newY = 0
	}
	v.SetOrigin(ox, newY)
	return nil
}

func pageDown(g *gocui.Gui, v *gocui.View) error {
	if v == nil {
		var err error
		v, err = g.View("logs")
		if err != nil {
			return err
		}
	}
	ox, oy := v.Origin()
	lines := len(v.BufferLines())
	_, sy := v.Size()
	newY := oy + sy
	if newY+sy > lines {
		newY = lines - sy
		if newY < 0 {
			newY = 0
		}
	}
	v.SetOrigin(ox, newY)
	return nil
}

func closePopup(g *gocui.Gui, v *gocui.View) error {
	if showPopup {
		showPopup = false
		return g.DeleteView("popup")
	}
	return nil
}
