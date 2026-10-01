// Package ui holds the screen contract and the text helpers every
// component needs. It is a leaf package so components, screens, and the
// root model can all import it without a cycle.
package ui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/Linux-DEX/azstorecli/internal/msg"
	"github.com/Linux-DEX/azstorecli/internal/storage"
)

// Screen is one routable screen.
//
// Screens are pointer-receiver models. Bubble Tea usually favours value
// receivers, but a screen here owns several sub-components and a cache;
// threading a value copy through Update makes it far too easy to drop a
// mutation on a path that returns early.
type Screen interface {
	tea.Model

	// Resize lays the screen out for a new terminal size.
	Resize(width, height int)
	// ShortHelp is this screen's contextual key list.
	ShortHelp() []key.Binding
	// Scope is the keymap scope this screen's actions live in.
	Scope() string
}

// MinWideColumns is the terminal width below which multi-pane screens
// collapse to a single pane.
//
// The three-pane blob layout needs roughly this much to be legible.
// Below it, letting lipgloss truncate produces a scrambled frame that
// looks like a rendering bug, so the layout switches mode instead.
const MinWideColumns = 100

// Wide reports whether there is room for a side-by-side layout.
func Wide(width int) bool { return width >= MinWideColumns }

// SideWidth is the left pane width shared by every split screen.
// It matches the functions list: a third of the terminal, between 20 and 36.
func SideWidth(width int) int {
	w := width / 3
	if w < 20 {
		return 20
	}
	if w > 36 {
		return 36
	}
	return w
}

// Truncate cuts s to w display cells, appending an ellipsis when it had
// to cut. Width is measured in cells, not bytes or runes, so CJK and
// emoji do not overflow the column.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	// lipgloss.Width ignores ANSI, so a selected row's color codes are not
	// counted as part of the name and then cropped away.
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return runewidth.Truncate(s, w, "…")
}

// TruncateLeft cuts from the front, keeping the tail. Blob names and
// file paths are far more identifiable by their last segment.
func TruncateLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for i := range runes {
		candidate := string(runes[i:])
		if lipgloss.Width(candidate)+1 <= w {
			return "…" + candidate
		}
	}
	return "…"
}

// Pad renders s in exactly w cells, truncating or space-padding.
func Pad(s string, w int) string {
	s = Truncate(s, w)
	if diff := w - lipgloss.Width(s); diff > 0 {
		return s + strings.Repeat(" ", diff)
	}
	return s
}

// PadLeft is Pad, right-aligned. Used for sizes and counts, which are
// only comparable at a glance when their digits line up.
func PadLeft(s string, w int) string {
	s = Truncate(s, w)
	if diff := w - lipgloss.Width(s); diff > 0 {
		return strings.Repeat(" ", diff) + s
	}
	return s
}

// Fit forces a block to exactly w×h cells: lines longer than w are cut,
// short blocks are padded with blank lines, tall ones are clipped.
//
// Every pane goes through this before being joined. Without it, one
// pane rendering a line too long or a row too many shifts every pane
// beside it and the whole frame looks corrupted.
func Fit(block string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := strings.Split(block, "\n")
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out = append(out, Pad(stripTrailing(lines[i]), w))
			continue
		}
		out = append(out, strings.Repeat(" ", w))
	}
	return strings.Join(out, "\n")
}

func stripTrailing(s string) string { return strings.TrimRight(s, " ") }

// Bar renders a full-width line of text in a style.
func Bar(style lipgloss.Style, text string, width int) string {
	return style.Render(Pad(text, width))
}

// Nav is the vim-plus-arrows motion set every list obeys.
type Nav int

const (
	NavNone Nav = iota
	NavUp
	NavDown
	NavLeft
	NavRight
	NavTop
	NavBottom
	NavHalfUp
	NavHalfDown
	NavSelect
)

// KeyNav classifies a key event as a motion, using the user's bindings
// so a remapped j/k works in every list at once.
type KeyNav interface {
	Matches(tea.KeyMsg, string) bool
}

// ResolveNav maps a key event to a motion, or NavNone.
func ResolveNav(km KeyNav, msg tea.KeyMsg) Nav {
	switch {
	case km.Matches(msg, "nav.up"):
		return NavUp
	case km.Matches(msg, "nav.down"):
		return NavDown
	case km.Matches(msg, "nav.left"):
		return NavLeft
	case km.Matches(msg, "nav.right"):
		return NavRight
	case km.Matches(msg, "nav.top"):
		return NavTop
	case km.Matches(msg, "nav.bottom"):
		return NavBottom
	case km.Matches(msg, "nav.half_up"):
		return NavHalfUp
	case km.Matches(msg, "nav.half_down"):
		return NavHalfDown
	case km.Matches(msg, "nav.select"):
		return NavSelect
	default:
		return NavNone
	}
}

// Clamp bounds v to [lo, hi].
// Fail turns an error into a status or error message. Read-only
// refusals become a warning with the unlock hint rather than a red
// toast that looks like a crash.
func Fail(err error) tea.Msg {
	if err == nil {
		return nil
	}
	if errors.Is(err, storage.ErrReadOnly) {
		return msg.Status{
			Text:    "profile is read-only — press Ctrl+W to unlock for 5 minutes",
			Warning: true,
		}
	}
	return msg.Error{Err: err}
}

func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
