package ui

import (
	"github.com/awesome-gocui/gocui"
)

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
