package funcs

import (
	"regexp"
	"sort"
	"strings"
)

// Function is one discovered function.
type Function struct {
	Name      string
	Trigger   string   // httpTrigger, queueTrigger, timerTrigger, blobTrigger, ...
	Methods   []string // HTTP verbs, empty for non-HTTP triggers
	URL       string   // invoke URL, empty for non-HTTP triggers
	Route     string   // path portion of URL
	Source    string   // source file, when discoverable
	Bindings  []Binding
	Target    string // the queue / container / table the trigger watches
	AuthLevel string
	Disabled  bool
}

// IsHTTP reports whether this function has an HTTP trigger.
func (f Function) IsHTTP() bool { return f.URL != "" || strings.EqualFold(f.Trigger, "httpTrigger") }

// Binding is one input or output binding.
type Binding struct {
	Name      string
	Type      string
	Direction string
	Target    string // queueName / path / tableName, whichever applies
}

// The Core Tools route table looks like:
//
//	Functions:
//
//	        HttpCreateOrder: [POST] http://localhost:7071/api/orders
//
//	        OrderCreated: queueTrigger
//
// Indentation, blank lines, and ANSI colour all vary between versions,
// so the parser anchors on the "Name:" shape rather than on layout.
var (
	httpRouteRe    = regexp.MustCompile(`^\s*([A-Za-z0-9_\-]+):\s*\[([A-Za-z,]+)\]\s+(https?://\S+)\s*$`)
	nonHTTPRouteRe = regexp.MustCompile(`^\s*([A-Za-z0-9_\-]+):\s*([A-Za-z]+Trigger)\s*$`)
	ansiRe         = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	functionsHdrRe = regexp.MustCompile(`(?i)^\s*Functions:\s*$`)
)

// ParseRoutes extracts the route table from captured host stdout.
//
// The host reprints the whole table on every restart, so later entries
// supersede earlier ones for the same name — otherwise a watch-mode
// restart would double every row.
func ParseRoutes(lines []string) []Function {
	byName := map[string]Function{}
	seenHeader := false

	for _, raw := range lines {
		line := ansiRe.ReplaceAllString(raw, "")
		if functionsHdrRe.MatchString(line) {
			seenHeader = true
			continue
		}
		// Only parse after the header: the startup banner contains
		// "Version: 4.0.5455" and other colon-shaped lines.
		if !seenHeader {
			continue
		}

		if m := httpRouteRe.FindStringSubmatch(line); m != nil {
			methods := strings.Split(strings.ToUpper(m[2]), ",")
			for i := range methods {
				methods[i] = strings.TrimSpace(methods[i])
			}
			byName[m[1]] = Function{
				Name:    m[1],
				Trigger: "httpTrigger",
				Methods: methods,
				URL:     m[3],
				Route:   routePath(m[3]),
			}
			continue
		}
		if m := nonHTTPRouteRe.FindStringSubmatch(line); m != nil {
			byName[m[1]] = Function{Name: m[1], Trigger: m[2]}
			continue
		}
		// A blank line after at least one entry ends the table; anything
		// after it is ordinary log output.
		if strings.TrimSpace(line) == "" && len(byName) > 0 {
			seenHeader = false
		}
	}

	out := make([]Function, 0, len(byName))
	for _, f := range byName {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// routePath strips scheme and host, leaving "/api/orders".
func routePath(url string) string {
	idx := strings.Index(url, "://")
	if idx < 0 {
		return url
	}
	rest := url[idx+3:]
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return "/"
	}
	return rest[slash:]
}

// Merge combines functions discovered on disk with the routes the host
// printed. The host is authoritative for URL and method — it applied
// host.json's routePrefix and the real port — while disk is the only
// source for source paths, bindings, and the disabled flag.
func Merge(discovered, fromHost []Function) []Function {
	byName := map[string]Function{}
	for _, f := range discovered {
		byName[f.Name] = f
	}
	for _, h := range fromHost {
		existing, ok := byName[h.Name]
		if !ok {
			byName[h.Name] = h
			continue
		}
		existing.Trigger = h.Trigger
		existing.URL = h.URL
		existing.Route = h.Route
		if len(h.Methods) > 0 {
			existing.Methods = h.Methods
		}
		byName[h.Name] = existing
	}

	out := make([]Function, 0, len(byName))
	for _, f := range byName {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// trimLine returns the first non-empty line of s, trimmed.
func trimLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}
