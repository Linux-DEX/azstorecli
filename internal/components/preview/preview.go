// Package preview renders blob and message content: syntax-highlighted
// text where possible, a hex dump where not.
package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/Linux-DEX/azstorecli/internal/components/hexview"
	"github.com/Linux-DEX/azstorecli/internal/theme"
	"github.com/Linux-DEX/azstorecli/internal/ui"
)

// Mode is how the content is being displayed.
type Mode int

const (
	ModeAuto Mode = iota
	ModeText
	ModeHex
)

// Model is the preview pane.
type Model struct {
	vp    viewport.Model
	theme theme.Theme

	name      string
	raw       []byte
	truncated bool
	mode      Mode
	err       error
	loaded    bool
}

// New builds a preview pane.
func New(t theme.Theme) *Model {
	vp := viewport.New(0, 0)
	return &Model{vp: vp, theme: t}
}

// SetSize resizes the pane.
func (m *Model) SetSize(w, h int) {
	m.vp.Width, m.vp.Height = max(w, 0), max(h, 0)
	m.render()
}

// Clear empties the pane.
func (m *Model) Clear() {
	m.name, m.raw, m.err, m.loaded, m.truncated = "", nil, nil, false, false
	m.vp.SetContent("")
}

// SetError shows a failure instead of content.
func (m *Model) SetError(name string, err error) {
	m.Clear()
	m.name, m.err, m.loaded = name, err, true
	m.render()
}

// SetContent loads bytes for display. truncated marks that only the
// head of a large blob was fetched.
func (m *Model) SetContent(name string, data []byte, truncated bool) {
	m.name, m.raw, m.truncated, m.err, m.loaded = name, data, truncated, nil, true
	m.vp.GotoTop()
	m.render()
}

// Name returns the currently previewed item.
func (m *Model) Name() string { return m.name }

// Loaded reports whether anything has been loaded.
func (m *Model) Loaded() bool { return m.loaded }

// Truncated reports whether only part of the content is shown.
func (m *Model) Truncated() bool { return m.truncated }

// ToggleHex switches between the text and hex renderings.
func (m *Model) ToggleHex() {
	if m.mode == ModeHex {
		m.mode = ModeAuto
	} else {
		m.mode = ModeHex
	}
	m.render()
}

// Mode returns the current display mode.
func (m *Model) Mode() Mode { return m.mode }

// Scroll applies a navigation motion to the viewport.
func (m *Model) Scroll(n ui.Nav) {
	switch n {
	case ui.NavUp:
		m.vp.LineUp(1)
	case ui.NavDown:
		m.vp.LineDown(1)
	case ui.NavHalfUp:
		m.vp.HalfViewUp()
	case ui.NavHalfDown:
		m.vp.HalfViewDown()
	case ui.NavTop:
		m.vp.GotoTop()
	case ui.NavBottom:
		m.vp.GotoBottom()
	}
}

// Raw returns the loaded bytes, for yanking or editing.
func (m *Model) Raw() []byte { return m.raw }

// View renders the pane.
func (m *Model) View() string { return m.vp.View() }

// render rebuilds the viewport content for the current mode and size.
func (m *Model) render() {
	if m.vp.Width <= 0 {
		return
	}
	switch {
	case m.err != nil:
		m.vp.SetContent(m.theme.StatusErr.Render("cannot preview: " + m.err.Error()))
		return
	case !m.loaded:
		m.vp.SetContent(m.theme.Muted.Render("no selection"))
		return
	case len(m.raw) == 0:
		m.vp.SetContent(m.theme.Muted.Render("(empty)"))
		return
	}

	// Binary content has no useful text rendering; forcing one produces
	// a screenful of replacement characters that tells the user nothing.
	if m.mode == ModeHex || (m.mode == ModeAuto && !isText(m.raw)) {
		m.vp.SetContent(hexview.Render(m.raw, m.vp.Width, m.theme))
		return
	}

	body := string(m.raw)
	if pretty, ok := prettyJSON(m.raw); ok {
		body = pretty
	}
	m.vp.SetContent(highlight(body, m.name, m.vp.Width))
}

// prettyJSON reformats a compact JSON document for reading. A body that
// is not valid JSON is left exactly as it is — reformatting guesswork
// would misrepresent what is actually stored.
func prettyJSON(data []byte) (string, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", false
	}
	var out bytes.Buffer
	if err := json.Indent(&out, trimmed, "", "  "); err != nil {
		return "", false
	}
	return out.String(), true
}

// highlight runs chroma over the content, falling back to plain text
// when the terminal or the lexer cannot help.
func highlight(body, name string, width int) string {
	lexer := lexers.Match(name)
	if lexer == nil {
		lexer = lexers.Analyse(body)
	}
	if lexer == nil {
		return wrapLines(body, width)
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("nord")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return wrapLines(body, width)
	}

	iterator, err := lexer.Tokenise(nil, body)
	if err != nil {
		return wrapLines(body, width)
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return wrapLines(body, width)
	}
	return buf.String()
}

// wrapLines hard-truncates each line to the pane width. Soft-wrapping
// source text scrambles indentation, and a preview is for orientation,
// not for reading a 400-character minified line in full.
func wrapLines(body string, width int) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = ui.Truncate(strings.ReplaceAll(l, "\t", "    "), width)
	}
	return strings.Join(lines, "\n")
}

// isText decides whether bytes can be shown as text: valid UTF-8 with
// no NUL bytes and not too many control characters.
func isText(data []byte) bool {
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	if !utf8.Valid(sample) {
		return false
	}
	control := 0
	for _, r := range string(sample) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			control++
		}
	}
	return control*100 <= len(sample)
}

// Footer renders the status line under the preview.
func (m *Model) Footer(extra string) string {
	parts := make([]string, 0, 3)
	if m.mode == ModeHex || (m.mode == ModeAuto && len(m.raw) > 0 && !isText(m.raw)) {
		parts = append(parts, "hex")
	}
	if m.truncated {
		parts = append(parts, "truncated — F loads all")
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	if len(m.raw) > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", int(m.vp.ScrollPercent()*100)))
	}
	return strings.Join(parts, "  ·  ")
}
