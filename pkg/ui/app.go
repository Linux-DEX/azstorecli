package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/Linux-DEX/azstorecli/pkg/storage"
	"github.com/awesome-gocui/gocui"
)

var (
	logChan        <-chan string
	showPopup      = true
	showLogs       = false
	activeCategory = 0
	activeIndex    = 0
	activeView     = "left" // "left" or "logs"

	resources = []string{"Containers", "Queues", "File Shares", "Tables"}

	data = map[string][]string{
		"Containers":  {"azurite-container", "backup-container", "test-container"},
		"Queues":      {"notifications", "processing", "analytics"},
		"File Shares": {"shared-data", "logs", "archives"},
		"Tables":      {"users", "tasks", "settings"},
	}
)

// Entry point
func RunApp() error {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return fmt.Errorf("failed to init gocui: %w", err)
	}
	defer g.Close()

	g.Cursor = false
	g.SetManagerFunc(layout)

	// Keybindings (fixed struct)
	type binding struct {
		key interface{}
		mod gocui.Modifier
		fn  func(*gocui.Gui, *gocui.View) error
	}

	keys := []binding{
		{gocui.KeyCtrlC, gocui.ModNone, quit},
		{'q', gocui.ModNone, quit},
		{gocui.KeyEsc, gocui.ModNone, closePopup},

		{'h', gocui.ModNone, moveLeft},
		{'l', gocui.ModNone, moveRight},
		{'j', gocui.ModNone, moveDown},
		{'k', gocui.ModNone, moveUp},

		{gocui.KeyArrowLeft, gocui.ModNone, moveLeft},
		{gocui.KeyArrowRight, gocui.ModNone, moveRight},
		{gocui.KeyArrowUp, gocui.ModNone, moveUp},
		{gocui.KeyArrowDown, gocui.ModNone, moveDown},

		{'L', gocui.ModNone, toggleLogs},
		{'r', gocui.ModNone, reattachLogs},
		{gocui.KeyPgup, gocui.ModNone, scrollLogsUp},
		{gocui.KeyPgdn, gocui.ModNone, scrollLogsDown},
	}
	for _, kb := range keys {
		if err := g.SetKeybinding("", kb.key, kb.mod, kb.fn); err != nil {
			return err
		}
	}

	logChan, _ = storage.StartAzurite()
	go listenLogs(g)

	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		return err
	}
	return nil
}

// Listen for logs
func listenLogs(g *gocui.Gui) {
	go func() {
		for line := range logChan {
			if showLogs {
				g.Update(func(gui *gocui.Gui) error {
					v, err := gui.View("logs")
					if err != nil {
						return nil
					}
					fmt.Fprint(v, line)
					// Auto-scroll if at bottom
					lines := len(v.BufferLines())
					_, sy := v.Size()
					_, oy := v.Origin()
					if oy+sy >= lines-2 {
						if lines > sy {
							v.SetOrigin(0, lines-sy)
						}
					}
					return nil
				})
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

// Layout
func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()

	// Header
	if v, err := g.SetView("header", 0, 0, maxX-1, 2, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Title = "Azurite Local Storage UI"
		fmt.Fprintln(v, "[H/L] Switch Category | [J/K] Navigate | [L] Toggle Logs | [ESC] Close Popup | [Q] Quit")
	}

	leftWidth := maxX / 4
	contentTop := 3
	contentBottom := maxY - 2
	logHeight := 8
	if showLogs {
		contentBottom = maxY - logHeight - 2
	}

	// Calculate box height so 4 boxes fill the available space
	totalHeight := contentBottom - contentTop
	boxHeight := totalHeight / len(resources)

	for i, name := range resources {
		y0 := contentTop + i*boxHeight
		y1 := y0 + boxHeight - 1
		if i == len(resources)-1 {
			y1 = contentBottom - 1
		}

		if v, err := g.SetView(name, 0, y0, leftWidth, y1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Wrap = true
			v.Highlight = true
		}

		v, _ := g.View(name)
		v.Clear()
		v.Title = name
		if i == activeCategory {
			v.BgColor = gocui.ColorBlue
			v.FgColor = gocui.ColorWhite
		} else {
			v.BgColor = gocui.ColorDefault
			v.FgColor = gocui.ColorDefault
		}
		for _, item := range data[name] {
			fmt.Fprintln(v, item)
		}
	}

	// Right pane
	if v, err := g.SetView("rightpane", leftWidth+1, contentTop, maxX-1, contentBottom, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Wrap = true
	}
	v, _ := g.View("rightpane")
	v.Clear()
	v.Title = fmt.Sprintf("Contents of %s", resources[activeCategory])
	renderRightPane(v)

	// Logs
	if showLogs {
		if v, err := g.SetView("logs", 0, contentBottom+1, maxX-1, maxY-1, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Title = "Azurite Logs (PgUp/PgDn to scroll)"
			v.Wrap = true
			v.Autoscroll = false
		}
	} else {
		g.DeleteView("logs")
	}

	// Popup
	if showPopup {
		w, h := 60, 10
		x0 := (maxX - w) / 2
		y0 := (maxY - h) / 2
		if v, err := g.SetView("popup", x0, y0, x0+w, y0+h, 0); err != nil {
			if !errors.Is(err, gocui.ErrUnknownView) {
				return err
			}
			v.Title = "Welcome!"
			v.Wrap = true
			fmt.Fprintln(v, "Welcome to Azurite Local Storage Explorer\n")
			fmt.Fprintln(v, "[H/L] Switch Resource Type")
			fmt.Fprintln(v, "[J/K] Navigate")
			fmt.Fprintln(v, "[L] Toggle Logs")
			fmt.Fprintln(v, "[ESC] Close Popup")
		}
	} else {
		g.DeleteView("popup")
	}

	return nil
}

func renderRightPane(v *gocui.View) {
	items := data[resources[activeCategory]]
	for i, item := range items {
		if i == activeIndex {
			fmt.Fprintf(v, "> %s\n", item)
		} else {
			fmt.Fprintf(v, "  %s\n", item)
		}
	}
}

// Movement and navigation
func moveLeft(g *gocui.Gui, v *gocui.View) error {
	if activeView == "logs" {
		return nil
	}
	if activeCategory > 0 {
		activeCategory--
		activeIndex = 0
		g.Update(func(gui *gocui.Gui) error { layout(gui); return nil })
	}
	return nil
}

func moveRight(g *gocui.Gui, v *gocui.View) error {
	if activeView == "logs" {
		return nil
	}
	if activeCategory < len(resources)-1 {
		activeCategory++
		activeIndex = 0
		g.Update(func(gui *gocui.Gui) error { layout(gui); return nil })
	}
	return nil
}

func moveUp(g *gocui.Gui, v *gocui.View) error {
	if activeView == "logs" {
		return scrollLogsUp(g, v)
	}
	if activeIndex > 0 {
		activeIndex--
		g.Update(func(gui *gocui.Gui) error { layout(gui); return nil })
	}
	return nil
}

func moveDown(g *gocui.Gui, v *gocui.View) error {
	if activeView == "logs" {
		return scrollLogsDown(g, v)
	}
	items := data[resources[activeCategory]]
	if activeIndex < len(items)-1 {
		activeIndex++
		g.Update(func(gui *gocui.Gui) error { layout(gui); return nil })
	}
	return nil
}

// Scroll logs manually
func scrollLogsUp(g *gocui.Gui, v *gocui.View) error {
	v, err := g.View("logs")
	if err != nil {
		return nil
	}
	ox, oy := v.Origin()
	if oy > 0 {
		v.SetOrigin(ox, oy-1)
	}
	return nil
}

func scrollLogsDown(g *gocui.Gui, v *gocui.View) error {
	v, err := g.View("logs")
	if err != nil {
		return nil
	}
	ox, oy := v.Origin()
	lines := len(v.BufferLines())
	_, sy := v.Size()
	if oy+sy < lines {
		v.SetOrigin(ox, oy+1)
	}
	return nil
}

// Misc
func toggleLogs(g *gocui.Gui, v *gocui.View) error {
	showLogs = !showLogs
	if showLogs {
		activeView = "logs"
	} else {
		activeView = "left"
	}
	g.Update(func(gui *gocui.Gui) error { layout(gui); return nil })
	return nil
}

func closePopup(g *gocui.Gui, v *gocui.View) error {
	if showPopup {
		showPopup = false
		g.DeleteView("popup")
	}
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	storage.StopAzurite()
	return gocui.ErrQuit
}

func reattachLogs(g *gocui.Gui, v *gocui.View) error {
	id := storage.GetAzuriteContainerID()
	newChan, _ := storage.AttachLogs(id)
	logChan = newChan
	return nil
}

