// Package helpbar is the bottom chrome: screen numbers and local keys.
package helpbar

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Render draws the help bar.
func Render(t theme.Theme, width int, active msg.ScreenID, local []key.Binding) string {
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
	if len(local) > 0 {
		b.WriteString(t.Muted.Render("·  "))
		for i, k := range local {
			if i > 0 {
				b.WriteString("  ")
			}
			help := k.Help()
			b.WriteString(t.Key.Render(help.Key) + " " + t.Muted.Render(help.Desc))
		}
	}
	b.WriteString("  " + t.Key.Render("?") + " " + t.Muted.Render("help"))
	return t.HelpBar.Render(ui.Pad(stripANSIPad(b.String(), width), width))
}

func stripANSIPad(s string, width int) string {
	// The bar is already styled; just don't overflow.
	if width <= 0 {
		return s
	}
	return s
}
