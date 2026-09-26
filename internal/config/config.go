package config

import (
	"errors"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// Load merges configuration in the order defined in docs/ARCHITECTURE.md
// §4.4: built-in defaults, then the global config file, then the
// project-local config file (if one is found), then AZSTORE_* environment
// variables. Command-line flags are merged separately by the caller via
// MergeFlags, since only cobra knows the parsed *pflag.FlagSet.
//
// Missing files are not errors — a fresh install has neither the global
// nor the project config yet, and Defaults() already covers that case.
func Load() (Config, error) {
	k := koanf.New(".")

	if err := k.Load(structs.Provider(Defaults(), "koanf"), nil); err != nil {
		return Config{}, err
	}

	if err := k.Load(file.Provider(GlobalConfigPath()), yaml.Parser()); err != nil {
		if !isNotExist(err) {
			return Config{}, err
		}
	}

	if projectPath, err := FindProjectConfig(); err == nil && projectPath != "" {
		if err := k.Load(file.Provider(projectPath), yaml.Parser()); err != nil {
			if !isNotExist(err) {
				return Config{}, err
			}
		}
	}

	if err := k.Load(env.Provider("AZSTORE_", ".", normalizeEnv), nil); err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// normalizeEnv converts AZSTORE_UI_THEME to ui.theme so it lines up with
// the koanf struct tags above.
func normalizeEnv(s string) string {
	s = strings.TrimPrefix(s, "AZSTORE_")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "_", ".")
}

// isNotExist reports whether err is (or wraps) a file-not-found error.
// koanf's file provider returns a plain *os.PathError, which errors.Is
// unwraps cleanly against os.ErrNotExist.
func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
