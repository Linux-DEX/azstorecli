package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const appName = "azstorecli"

// GlobalConfigPath returns $XDG_CONFIG_HOME/azstorecli/config.yaml.
func GlobalConfigPath() string {
	return filepath.Join(xdg.ConfigHome, appName, "config.yaml")
}

// GlobalKeymapPath returns $XDG_CONFIG_HOME/azstorecli/keymap.yaml.
func GlobalKeymapPath() string {
	return filepath.Join(xdg.ConfigHome, appName, "keymap.yaml")
}

// GlobalProfilesPath returns $XDG_CONFIG_HOME/azstorecli/profiles.yaml.
// Written with mode 0600 since it may hold real-cloud credentials.
func GlobalProfilesPath() string {
	return filepath.Join(xdg.ConfigHome, appName, "profiles.yaml")
}

// DataDir returns $XDG_DATA_HOME/azstorecli, the parent of workspaces/,
// snapshots/, and logs/.
func DataDir() string {
	return filepath.Join(xdg.DataHome, appName)
}

// StateDir returns $XDG_STATE_HOME/azstorecli, home to recent.json and
// the azstore.pid instance guard.
func StateDir() string {
	return filepath.Join(xdg.StateHome, appName)
}

// CacheDir returns $XDG_CACHE_HOME/azstorecli, home to LRU-evicted blob
// previews.
func CacheDir() string {
	return filepath.Join(xdg.CacheHome, appName)
}

// FindProjectConfig walks up from the current working directory looking
// for .azstorecli/config.yaml or host.json, returning the project config
// path if found. Returns "" (no error) if neither marker is found before
// reaching the filesystem root — that's the normal case outside a project.
func FindProjectConfig() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, ".azstorecli", "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "host.json")); err == nil {
			// Inside a Functions app with no .azstorecli/ yet — the
			// caller may still want the default project-relative path.
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil // reached filesystem root, no project found
		}
		dir = parent
	}
}
