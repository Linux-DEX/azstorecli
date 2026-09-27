package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeJoin resolves an archive member name against root and refuses any
// path that would escape it. Guards against tar traversal ("../../etc"),
// absolute members ("/etc/passwd"), and — on Windows — drive-relative
// and UNC members that filepath.Join would otherwise honour.
func SafeJoin(root, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path in archive")
	}
	// A backslash is a legal character in a tar member name on unix but
	// a separator on Windows; normalise before validating so the same
	// archive is rejected identically on both.
	//
	// Go 1.20+ filepath.Join no longer drops earlier elements when a
	// later one starts with '/', so we cannot rely on Clean("/"+name)
	// plus Join to surface a traversal — it silently lands inside root.
	// Reject `..` and absolute members up front instead.
	norm := strings.ReplaceAll(name, `\`, "/")
	if filepath.IsAbs(norm) || strings.HasPrefix(norm, "/") {
		return "", fmt.Errorf("illegal path in archive: %q", name)
	}
	for _, part := range strings.Split(norm, "/") {
		if part == ".." {
			return "", fmt.Errorf("illegal path in archive: %q", name)
		}
	}
	clean := filepath.Clean(norm)
	dst := filepath.Join(root, clean)

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return "", err
	}
	if absDst != absRoot && !strings.HasPrefix(absDst, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal path in archive: %q", name)
	}
	return dst, nil
}

// RemoveContents empties dir without removing dir itself. Restore needs
// this so the flock held on the workspace directory survives the wipe.
func RemoveContents(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// DirSize sums the apparent size of every regular file under dir.
// Unreadable entries are skipped rather than failing the whole walk —
// this only ever feeds a display value.
func DirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort size
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Exists reports whether path exists, treating any stat error other than
// not-exist as "exists" so callers fail loudly later rather than
// silently overwriting something they could not read.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
