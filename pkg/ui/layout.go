package ui

import (
	"errors"
	"fmt"

	"github.com/awesome-gocui/gocui"
)

// Layout draws all panels
func layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	leftWidth := maxX / 3
	contentTop := 1
	contentBottom := maxY - 1
	totalHeight := contentBottom - contentTop + 1

	// --- Left panel: distribute heights evenly and handle remainder ---
	heights := make([]int, len(leftSections))
	baseHeight := totalHeight / len(leftSections)
	remainder := totalHeight % len(leftSections)
	for i := range heights {
		heights[i] = baseHeight
		if i < remainder {
			heights[i]++ // distribute leftover pixels to top sections
		}
	}

	y := contentTop
	for i, name := range leftSections {
		y0 := y
		y1 := y + heights[i] - 1
		y = y1 + 1

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

	// --- Right panel: full height to match left ---
	right, err := g.SetView("right", leftWidth, contentTop, maxX-1, contentBottom, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	right.Clear()
	right.Wrap = true
	right.Autoscroll = false

	if showLogs {
		right.Title = "Azurite Logs (press L to hide, R to reattach)"
		right.Highlight = false
		right.Autoscroll = true

		for _, line := range logsBuf {
			fmt.Fprintln(right, line)
		}
	} else {
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

	// Ensure cursor is visible for logs
	if showLogs && focusSide == "logs" {
		right.SetCursor(0, 0)
	}

	// --- Popup ---
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
