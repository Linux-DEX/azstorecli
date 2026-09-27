package funcs

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// functionJSON is the classic per-function descriptor used by the v3
// programming model and by every compiled language's build output.
type functionJSON struct {
	Disabled   any             `json:"disabled"` // bool, or a string naming an app setting
	ScriptFile string          `json:"scriptFile"`
	EntryPoint string          `json:"entryPoint"`
	Bindings   []bindingJSON   `json:"bindings"`
	Extra      json.RawMessage `json:"-"`
}

type bindingJSON struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Direction string   `json:"direction"`
	AuthLevel string   `json:"authLevel"`
	Methods   []string `json:"methods"`
	Route     string   `json:"route"`
	QueueName string   `json:"queueName"`
	Path      string   `json:"path"`
	TableName string   `json:"tableName"`
	Schedule  string   `json:"schedule"`
}

// Discover walks a function app for function.json descriptors.
//
// The v4 Node and v2 Python models register functions with decorators
// and write no function.json at all, so this returns nothing for them.
// That is fine and expected: ParseRoutes covers those, and Merge
// combines whichever sources produced results.
func Discover(root string) ([]Function, error) {
	var out []Function

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree should not abort discovery of the rest.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", ".git", "bin", "obj", ".venv", "__pycache__", ".azstorecli":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "function.json" {
			return nil
		}

		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var fj functionJSON
		if json.Unmarshal(raw, &fj) != nil {
			return nil
		}

		dir := filepath.Dir(path)
		f := Function{
			Name:     filepath.Base(dir),
			Disabled: isDisabled(fj.Disabled),
			Source:   sourceFile(dir, fj.ScriptFile),
		}
		for _, b := range fj.Bindings {
			binding := Binding{
				Name:      b.Name,
				Type:      b.Type,
				Direction: b.Direction,
				Target:    firstNonEmpty(b.QueueName, b.Path, b.TableName, b.Schedule),
			}
			f.Bindings = append(f.Bindings, binding)

			if strings.HasSuffix(strings.ToLower(b.Type), "trigger") {
				f.Trigger = b.Type
				f.Methods = upperAll(b.Methods)
				f.Route = b.Route
				f.AuthLevel = b.AuthLevel
				f.Target = binding.Target
			}
		}
		out = append(out, f)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// isDisabled reads function.json's `disabled` field, which may be a
// bool or the name of an app setting holding "true".
func isDisabled(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(os.Getenv(t), "true") || strings.EqualFold(t, "true")
	default:
		return false
	}
}

// SetDisabled patches a function.json's disabled flag in place,
// preserving every other field including ones azstore does not model.
func SetDisabled(functionJSONPath string, disabled bool) error {
	raw, err := os.ReadFile(functionJSONPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	doc["disabled"] = disabled

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteAtomic(functionJSONPath, append(out, '\n'), 0o644)
}

// FunctionJSONPath returns a function's descriptor path, or "" when the
// app uses a decorator-based model with no per-function file.
func FunctionJSONPath(root, name string) string {
	candidate := filepath.Join(root, name, "function.json")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func sourceFile(dir, scriptFile string) string {
	if scriptFile == "" {
		return dir
	}
	return filepath.Clean(filepath.Join(dir, scriptFile))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func upperAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.ToUpper(s))
	}
	return out
}
