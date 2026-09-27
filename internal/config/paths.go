package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const appName = "azstorecli"

// ProjectDirName is the per-project config folder walked up to from the
// working directory.
const ProjectDirName = ".azstorecli"

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

// DataDir returns $XDG_DATA_HOME/azstorecli.
func DataDir() string { return filepath.Join(xdg.DataHome, appName) }

// WorkspacesDir holds named global Azurite workspaces.
func WorkspacesDir() string { return filepath.Join(DataDir(), "workspaces") }

// SnapshotsDir holds .tar.zst archives and their sidecar manifests.
func SnapshotsDir() string { return filepath.Join(DataDir(), "snapshots") }

// LogFile is the rotating slog destination.
func LogFile() string { return filepath.Join(DataDir(), "logs", "azstore.log") }

// StateDir returns $XDG_STATE_HOME/azstorecli.
func StateDir() string { return filepath.Join(xdg.StateHome, appName) }

// RecentPath holds recent projects, last screen, and cursor positions.
func RecentPath() string { return filepath.Join(StateDir(), "recent.json") }

// PIDPath guards against two instances driving one workspace.
func PIDPath() string { return filepath.Join(StateDir(), "azstore.pid") }

// CacheDir returns $XDG_CACHE_HOME/azstorecli.
func CacheDir() string { return filepath.Join(xdg.CacheHome, appName) }

// BlobCacheDir holds LRU-evicted downloaded blob previews.
func BlobCacheDir() string { return filepath.Join(CacheDir(), "blobs") }

// ProjectConfigPath returns dir/.azstorecli/config.yaml.
func ProjectConfigPath(dir string) string {
	return filepath.Join(dir, ProjectDirName, "config.yaml")
}

// ProjectSeedDir returns dir/.azstorecli/seed.
func ProjectSeedDir(dir string) string {
	return filepath.Join(dir, ProjectDirName, "seed")
}

// FindProject walks up from the working directory looking for a
// .azstorecli/ folder or a host.json, and returns the project config
// path plus the directory it anchors to.
//
// Both markers matter: .azstorecli/ means azstore has been initialised
// here, host.json means this is a Functions app azstore has not been
// initialised in yet. Returning the (not-yet-existing) config path for
// the second case is what lets `azstore init` default to the right
// place. Returns empty strings — not an error — when neither marker is
// found, which is the normal case outside a project.
func FindProject() (configPath, projectDir string, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return findProjectFrom(dir)
}

// findProjectFrom is the testable core of FindProject.
func findProjectFrom(dir string) (configPath, projectDir string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}

	// .azstorecli/ wins over host.json at the same level, and a nearer
	// marker wins over a farther one — a nested function app inside a
	// monorepo should not pick up the repo root's workspace.
	for {
		if fi, statErr := os.Stat(filepath.Join(dir, ProjectDirName)); statErr == nil && fi.IsDir() {
			return ProjectConfigPath(dir), dir, nil
		}
		if _, statErr := os.Stat(filepath.Join(dir, "host.json")); statErr == nil {
			return ProjectConfigPath(dir), dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", nil // reached the filesystem root
		}
		dir = parent
	}
}

// EnsureDirs creates the XDG directories azstore writes to. Config and
// state get 0700 because profiles.yaml and recent.json can both reveal
// more than another local user should see.
func EnsureDirs() error {
	for dir, perm := range map[string]os.FileMode{
		filepath.Dir(GlobalConfigPath()): 0o700,
		WorkspacesDir():                  0o755,
		SnapshotsDir():                   0o755,
		filepath.Dir(LogFile()):          0o755,
		StateDir():                       0o700,
		BlobCacheDir():                   0o755,
	} {
		if err := os.MkdirAll(dir, perm); err != nil {
			return err
		}
	}
	return nil
}
