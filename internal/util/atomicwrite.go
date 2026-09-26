package util

import (
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path by first writing to a temp file in the
// same directory, syncing it, then renaming it over the destination. The
// rename is atomic on a single filesystem, so a crash mid-write never
// leaves a corrupt or partially-written config file behind.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // durability before rename
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path) // atomic on same filesystem
}
