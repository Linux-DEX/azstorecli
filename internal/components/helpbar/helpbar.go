// Package helpbar is the bottom tab bar.
package helpbar

import (
	"strconv"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Render draws the tab bar. Key bindings stay in the ? overlay.
func Render(t theme.Theme, width int, active msg.ScreenID) string {
	var b strings.Builder
	for i, id := range msg.Ordered {
		label := strconv.Itoa(i+1) + " " + msg.Titles[id]
		if id == active {
			b.WriteString(t.Title.Render(label))
		} else {
			b.WriteString(t.Muted.Render(label))
		}
		b.WriteString("  ")
	}
	return t.HelpBar.Render(ui.Pad(stripANSIPad(b.String(), width), width))
}

func stripANSIPad(s string, width int) string {
	// The bar is already styled; just don't overflow.
	if width <= 0 {
		return s
	}
	return s
}
