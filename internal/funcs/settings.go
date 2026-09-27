package funcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// SettingsFile is the Core Tools local configuration filename.
const SettingsFile = "local.settings.json"

// Settings is local.settings.json. Unknown top-level keys are preserved
// through a round trip so patching AzureWebJobsStorage never drops a
// field azstore does not model.
type Settings struct {
	IsEncrypted bool
	Values      map[string]string
	raw         map[string]any
	path        string
}

// LoadSettings reads local.settings.json from a function app directory.
// A missing file yields an empty, writable Settings — that is the state
// of a freshly scaffolded app.
func LoadSettings(appDir string) (*Settings, error) {
	path := filepath.Join(appDir, SettingsFile)
	s := &Settings{
		Values: map[string]string{},
		raw:    map[string]any{},
		path:   path,
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.raw); err != nil {
		return nil, fmt.Errorf("%s: %w", SettingsFile, err)
	}

	if enc, ok := s.raw["IsEncrypted"].(bool); ok {
		s.IsEncrypted = enc
	}
	if values, ok := s.raw["Values"].(map[string]any); ok {
		for k, v := range values {
			// Core Tools requires every value to be a string; a number
			// or bool here is a user error the host rejects at startup.
			switch t := v.(type) {
			case string:
				s.Values[k] = t
			default:
				s.Values[k] = fmt.Sprintf("%v", t)
			}
		}
	}
	return s, nil
}

// Path returns the settings file path.
func (s *Settings) Path() string { return s.path }

// Get reads one setting.
func (s *Settings) Get(key string) (string, bool) {
	v, ok := s.Values[key]
	return v, ok
}

// Set writes one setting in memory.
func (s *Settings) Set(key, value string) { s.Values[key] = value }

// Keys returns the setting names, sorted.
func (s *Settings) Keys() []string {
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Save writes the file back atomically.
//
// It refuses to touch an encrypted file: Core Tools encrypts values
// with a machine key, and writing plaintext over them would corrupt the
// file in a way `func settings decrypt` cannot undo.
func (s *Settings) Save() error {
	if s.IsEncrypted {
		return errors.New("local.settings.json is encrypted — run `func settings decrypt` first")
	}

	values := make(map[string]any, len(s.Values))
	for k, v := range s.Values {
		values[k] = v
	}
	if s.raw == nil {
		s.raw = map[string]any{}
	}
	s.raw["IsEncrypted"] = false
	s.raw["Values"] = values

	data, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return err
	}
	// 0600: this file regularly holds real connection strings, which is
	// why Core Tools gitignores it. The mode should match that intent.
	return util.WriteAtomic(s.path, append(data, '\n'), 0o600)
}

// EnsureDevStorage points AzureWebJobsStorage at the emulator, which is
// the one setting that has to be right for anything else to work. It
// returns true when it changed something.
func (s *Settings) EnsureDevStorage(connString string) bool {
	const key = "AzureWebJobsStorage"
	want := connString
	if want == "" {
		want = "UseDevelopmentStorage=true"
	}
	if s.Values[key] == want {
		return false
	}
	// Never clobber a real cloud connection string without the user
	// asking: that is somebody's staging account.
	if existing, ok := s.Values[key]; ok && existing != "" &&
		existing != "UseDevelopmentStorage=true" && !isLocalConn(existing) {
		return false
	}
	s.Values[key] = want
	return true
}

func isLocalConn(s string) bool {
	return strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "localhost") ||
		strings.Contains(s, "devstoreaccount1")
}

// Validate checks a settings document the user just edited by hand,
// before the host is restarted onto it.
func Validate(data []byte) error {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	values, ok := doc["Values"]
	if !ok {
		return errors.New(`missing "Values" object`)
	}
	m, ok := values.(map[string]any)
	if !ok {
		return errors.New(`"Values" must be an object`)
	}
	for k, v := range m {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("Values.%s must be a string; the host rejects %T", k, v)
		}
	}
	return nil
}
