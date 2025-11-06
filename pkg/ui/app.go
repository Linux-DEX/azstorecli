package ui

import (
	"sync"

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
		"users":  {"alice", "bob", "charlie"},
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
