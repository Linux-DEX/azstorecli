package theme

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed themes/*.yaml
var embedded embed.FS

// Load resolves a theme by name, preferring a user file in themesDir
// over the embedded set so a custom theme can shadow a built-in one.
// An unknown name falls back to Dark rather than failing the boot —
// a typo in config.yaml should not prevent the app from starting.
func Load(name, themesDir string) (Theme, error) {
	if name == "" {
		name = "dark"
	}

	if themesDir != "" {
		userPath := filepath.Join(themesDir, name+".yaml")
		if raw, err := os.ReadFile(userPath); err == nil {
			return parse(raw, name)
		}
	}

	raw, err := embedded.ReadFile("themes/" + name + ".yaml")
	if err != nil {
		return Dark(), fmt.Errorf("unknown theme %q; using dark (available: %s)",
			name, strings.Join(Names(themesDir), ", "))
	}
	return parse(raw, name)
}

func parse(raw []byte, name string) (Theme, error) {
	var p Palette
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return Dark(), fmt.Errorf("theme %s: %w", name, err)
	}
	if p.Name == "" {
		p.Name = name
	}
	return Build(p), nil
}

// Names lists every selectable theme, embedded plus user-provided.
func Names(themesDir string) []string {
	seen := map[string]bool{}

	entries, _ := embedded.ReadDir("themes")
	for _, e := range entries {
		seen[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}
	if themesDir != "" {
		if userEntries, err := os.ReadDir(themesDir); err == nil {
			for _, e := range userEntries {
				if strings.HasSuffix(e.Name(), ".yaml") {
					seen[strings.TrimSuffix(e.Name(), ".yaml")] = true
				}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
