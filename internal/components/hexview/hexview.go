// Package hexview renders binary content as a classic hex dump. It is
// the fallback whenever a blob or message is not displayable text.
package hexview

import (
	"fmt"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/theme"
)

// Render produces a hex dump sized to the available width.
//
// The layout is offset + hex + ASCII. When the pane is too narrow for
// 16 bytes per line it steps down to 8, then 4 — the dump stays aligned
// and readable instead of wrapping into noise.
func Render(data []byte, width int, t theme.Theme) string {
	perLine := bytesPerLine(width)
	if perLine == 0 {
		return t.Muted.Render("pane too narrow for hex view")
	}

	var b strings.Builder
	for offset := 0; offset < len(data); offset += perLine {
		end := min(offset+perLine, len(data))
		chunk := data[offset:end]

		var hex, ascii strings.Builder
		for i := 0; i < perLine; i++ {
			if i < len(chunk) {
				fmt.Fprintf(&hex, "%02x ", chunk[i])
				ascii.WriteByte(printable(chunk[i]))
				continue
			}
			hex.WriteString("   ")
			ascii.WriteByte(' ')
		}

		if offset > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t.Muted.Render(fmt.Sprintf("%08x  ", offset)))
		b.WriteString(hex.String())
		b.WriteString(" |")
		b.WriteString(ascii.String())
		b.WriteByte('|')
	}
	if len(data) == 0 {
		return t.Muted.Render("(empty)")
	}
	return b.String()
}

// bytesPerLine picks the widest power-of-two grouping that fits.
func bytesPerLine(width int) int {
	// 10 for the offset column, then 3 cells per byte of hex plus 1 of
	// ASCII, plus 3 for the " |" and "|" around the ASCII gutter.
	for _, n := range []int{16, 8, 4} {
		if 10+n*4+3 <= width {
			return n
		}
	}
	return 0
}

func printable(b byte) byte {
	if b >= 0x20 && b < 0x7f {
		return b
	}
	return '.'
}
