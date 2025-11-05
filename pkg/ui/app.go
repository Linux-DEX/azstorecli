package ui

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Linux-DEX/azstorecli/pkg/storage"
	"github.com/awesome-gocui/gocui"
)

var (
	activeSection    = 0 // index of current left category
	activeLeftIndex  = 0
	activeRightIndex = 0

	leftSections = []string{"Containers", "Queues", "File Shares", "Tables"}
	leftData     = map[string][]string{
		"Containers":  {"images", "videos", "backups"},
		"Queues":      {"email-jobs", "task-queue"},
		"File Shares": {"projectA", "projectB"},
		"Tables":      {"users", "transactions"},
	}

	rightData = map[string][]string{
		"images": {"cat.png", "dog.jpg", "sunset.png"},
		"videos": {"intro.mp4", "trailer.mov"},
		"users":   {"alice", "bob", "charlie"},
	}

	focusSide = "left" // "left", "right", "logs"
	showLogs  = false  // Controls whether logs or content are in the right panel
	showPopup = true

	logChan <-chan string
	logsBuf []string

	// Global variables for safe goroutine cleanup
	done chan struct{}
	wg   sync.WaitGroup
)

// RunApp starts the GUI
func RunApp() error {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return err
	}
	// defer g.Close() is CRITICAL to restore terminal state
	defer g.Close()

	// Initialize channels & buffer
	done = make(chan struct{})
	logsBuf = []string{}

	g.Cursor = false
	g.Highlight = true
	g.SelFgColor = gocui.ColorCyan
	g.SetManagerFunc(layout)

	// Keybindings
	keys := []struct {
		key interface{}
		mod gocui.Modifier
		fn  func(*gocui.Gui, *gocui.View) error
	}{
		{gocui.KeyCtrlC, gocui.ModNone, quit},
		{'q', gocui.ModNone, quit},
		{'h', gocui.ModNone, moveLeft},
		{'l', gocui.ModNone, moveRight},
		{'j', gocui.ModNone, moveDown},
		{'k', gocui.ModNone, moveUp},
		{gocui.KeyEnter, gocui.ModNone, selectItem},
		{gocui.KeyEsc, gocui.ModNone, handleEsc},
		{'L', gocui.ModNone, toggleLogs},
		{'r', gocui.ModNone, reattachLogs},
		// Log scrolling keys still reference the "right" panel when showLogs is true
		{gocui.KeyPgup, gocui.ModNone, scrollLogsUpPage},
		{gocui.KeyPgdn, gocui.ModNone, scrollLogsDownPage},
	}

	for _, kb := range keys {
		if err := g.SetKeybinding("", kb.key, kb.mod, kb.fn); err != nil {
			return err
		}
	}

	// Start Azurite logs
	logChan, _ = storage.StartAzurite()

	// Start logs listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		listenLogs(g)
	}()

	// Run main GUI loop
	err = g.MainLoop()

	// Wait for goroutine to finish BEFORE defer g.Close() executes
	wg.Wait()

	return err
}

// Layout draws all panels
func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	leftWidth := maxX / 3
	contentTop := 1
	contentBottom := maxY - 1

	// Left panel (no change needed here)
	boxHeight := (contentBottom - contentTop) / len(leftSections)
	for i, name := range leftSections {
		y0 := contentTop + i*boxHeight
		y1 := y0 + boxHeight - 1
		v, err := g.SetView(name, 0, y0, leftWidth-1, y1, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Clear()
		v.Title = name
		v.Wrap = true
		v.Highlight = true
		v.SelFgColor = gocui.ColorCyan

		items := leftData[name]
		for j, item := range items {
			prefix := "  "
			if focusSide == "left" && i == activeSection && j == activeLeftIndex {
				prefix = "> "
			}
			fmt.Fprintf(v, "%s%s\n", prefix, item)
		}

		if i == activeSection && focusSide == "left" {
			v.SetCursor(0, activeLeftIndex)
		} else {
			v.SetCursor(0, 0)
		}
	}

	// Right panel (Modified to host either content or logs)
	right, err := g.SetView("right", leftWidth+1, contentTop, maxX-1, contentBottom-1, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	right.Clear()
	right.Wrap = true
	right.Autoscroll = false // Reset autoscroll

	if showLogs {
		// Log View Mode
		right.Title = "Azurite Logs (press L to hide, R to reattach)"
		right.Highlight = false // No highlighting for logs
		right.Autoscroll = true // Autoscroll for new log lines

		for _, line := range logsBuf {
			fmt.Fprintln(right, line)
		}
	} else {
		// Content View Mode
		right.Title = fmt.Sprintf("Contents of %s", leftSections[activeSection])
		right.Highlight = true
		right.SelFgColor = gocui.ColorCyan

		current := leftSections[activeSection]
		items := leftData[current]

		if len(items) > 0 {
			selected := items[activeLeftIndex]
			if blobs, ok := rightData[selected]; ok {
				for i, b := range blobs {
					prefix := "  "
					if focusSide == "right" && i == activeRightIndex {
						prefix = "> "
					}
					fmt.Fprintf(right, "%s%s\n", prefix, b)
				}

				if focusSide == "right" {
					right.SetCursor(0, activeRightIndex)
				} else {
					right.SetCursor(0, 0)
				}
			} else {
				fmt.Fprintln(right, "No blobs or contents found.")
			}
		} else {
			fmt.Fprintln(right, "No items found.")
		}
	}
	
	// Ensure cursor is visible if focus is on a scrollable view (logs)
	if showLogs && focusSide == "logs" {
		right.SetCursor(0, 0) // Reset cursor to 0,0, origin handles actual display
	}


	// Popup
	if showPopup {
		popupW, popupH := 60, 10
		x0 := (maxX - popupW) / 2
		y0 := (maxY - popupH) / 2
		v, err := g.SetView("popup", x0, y0, x0+popupW, y0+popupH, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Title = "Welcome!"
		v.Wrap = true
		v.Clear()
		fmt.Fprintln(v, "Welcome to Azurite Local Storage Explorer")
		fmt.Fprintln(v, "")
		fmt.Fprintln(v, "[H/L] Switch Resource Type")
		fmt.Fprintln(v, "[J/K] Navigate")
		fmt.Fprintln(v, "[Enter] Open Selected")
		fmt.Fprintln(v, "[ESC] Return to Left Panel")
		fmt.Fprintln(v, "[L] Toggle Logs | [R] Reattach Logs")
		fmt.Fprintln(v, "[Q] Quit")
	} else {
		g.DeleteView("popup")
	}

	return nil
}

// --- Navigation ---
// (No change to moveLeft, moveRight, moveDown, moveUp, selectItem, handleEsc)

func moveLeft(g *gocui.Gui, v *gocui.View) error {
	if focusSide == "left" && activeSection > 0 {
		activeSection--
		activeLeftIndex = 0
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func moveRight(g *gocui.Gui, v *gocui.View) error {
	if focusSide == "left" && activeSection < len(leftSections)-1 {
		activeSection++
		activeLeftIndex = 0
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func moveDown(g *gocui.Gui, v *gocui.View) error {
	// focusSide check is still needed for logs scrolling
	if showLogs { // If logs are shown, 'j' scrolls down the logs
		return scrollLogsDown(g)
	}
	
	if focusSide == "left" {
		current := leftSections[activeSection]
		items := leftData[current]
		if activeLeftIndex < len(items)-1 {
			activeLeftIndex++
		}
	} else if focusSide == "right" {
		current := leftSections[activeSection]
		items := leftData[current]
		if len(items) == 0 {
			return nil
		}
		selected := items[activeLeftIndex]
		blobs := rightData[selected]
		if activeRightIndex < len(blobs)-1 {
			activeRightIndex++
		}
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func moveUp(g *gocui.Gui, v *gocui.View) error {
	// focusSide check is still needed for logs scrolling
	if showLogs { // If logs are shown, 'k' scrolls up the logs
		return scrollLogsUp(g)
	}

	if focusSide == "left" && activeLeftIndex > 0 {
		activeLeftIndex--
	} else if focusSide == "right" && activeRightIndex > 0 {
		activeRightIndex--
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func selectItem(g *gocui.Gui, v *gocui.View) error {
	// Only switch focus if we are NOT showing logs
	if focusSide == "left" && !showLogs {
		focusSide = "right"
		activeRightIndex = 0
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}

func handleEsc(g *gocui.Gui, v *gocui.View) error {
	if showPopup {
		showPopup = false
		g.DeleteView("popup")
	} else if focusSide == "right" && !showLogs {
		focusSide = "left"
		activeRightIndex = 0
	}
	g.Update(func(gui *gocui.Gui) error { return nil })
	return nil
}


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
	if !showLogs { return nil }
	for i := 0; i < 5; i++ {
		scrollLogsUp(g)
	}
	return nil
}

func scrollLogsDownPage(g *gocui.Gui, v *gocui.View) error {
	if !showLogs { return nil }
	for i := 0; i < 5; i++ {
		scrollLogsDown(g)
	}
	return nil
}

// --- Quit ---
func quit(g *gocui.Gui, v *gocui.View) error {
	if done != nil {
		close(done)
	}
	storage.StopAzurite()
	return gocui.ErrQuit
}
