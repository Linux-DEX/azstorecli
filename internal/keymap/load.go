package keymap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// userKeymap is the shape of keymap.yaml:
//
//	version: 1
//	global:
//	  quit: ["q"]
//	screens:
//	  blob:
//	    upload: ["u"]
//
// Section and key names are the action ID with its prefix split on the
// dot, so `app.quit` is `global: { quit: [...] }` and `blob.upload` is
// `screens: { blob: { upload: [...] } }`.
type userKeymap struct {
	Version int                            `yaml:"version"`
	Global  map[string][]string            `yaml:"global"`
	Screens map[string]map[string][]string `yaml:"screens"`
}

// Load merges a user keymap over the defaults and validates the result.
// A missing file is not an error — most users never write one.
func Load(path string) (*KeyMap, error) {
	km := Defaults()

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return km, nil
	}
	if err != nil {
		return nil, err
	}

	var user userKeymap
	if err := yaml.Unmarshal(raw, &user); err != nil {
		return nil, fmt.Errorf("keymap.yaml: %w", err)
	}
	if user.Version != 0 && user.Version != 1 {
		return nil, fmt.Errorf("keymap.yaml: unsupported version %d", user.Version)
	}

	for actionID, keys := range user.flatten() {
		if err := km.SetKeys(actionID, keys); err != nil {
			return nil, fmt.Errorf("keymap.yaml: %w", err)
		}
	}
	return km, km.DetectConflicts()
}

// flatten turns the nested document into action ID → keys.
//
// A bare name in `global:` is resolved against the global actions'
// real prefixes, so both `quit` and `app.quit` work: the file reads
// naturally, and an explicit ID is still accepted.
func (u userKeymap) flatten() map[string][]string {
	out := map[string][]string{}

	globalPrefixes := map[string]string{}
	for _, a := range registry {
		if a.Scope != ScopeGlobal {
			continue
		}
		if _, prefixed, ok := strings.Cut(a.ID, "."); ok {
			globalPrefixes[prefixed] = a.ID
		}
	}

	for name, keys := range u.Global {
		if strings.Contains(name, ".") {
			out[name] = keys
			continue
		}
		if full, ok := globalPrefixes[name]; ok {
			out[full] = keys
			continue
		}
		out[name] = keys // unknown; SetKeys reports it by name
	}

	for scope, actions := range u.Screens {
		for name, keys := range actions {
			if strings.Contains(name, ".") {
				out[name] = keys
				continue
			}
			out[scopePrefix(scope)+"."+name] = keys
		}
	}
	return out
}

// scopePrefix maps a screen name in keymap.yaml to its action prefix.
// Most match one-to-one; the ones that do not are listed here rather
// than forcing the user to know the internal ID.
func scopePrefix(scope string) string {
	switch scope {
	case "functions":
		return "func"
	case "snapshots":
		return "snapshot"
	case "profiles":
		return "profile"
	case "dashboard":
		return "service"
	default:
		return scope
	}
}

// ListIDs returns every action ID with its current keys, for
// `azstore keys list`.
func (k *KeyMap) ListIDs() []string {
	out := make([]string, 0, len(k.byAction))
	for id, b := range k.byAction {
		scope := k.scopeOf[id]
		if scope == ScopeGlobal {
			scope = "global"
		}
		out = append(out, fmt.Sprintf("%-28s %-11s %s", id, scope, strings.Join(b.Keys(), ", ")))
	}
	sort.Strings(out)
	return out
}
