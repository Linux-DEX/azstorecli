package util

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Editor returns $EDITOR, then $VISUAL, then a platform fallback.
func Editor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// FileManager returns $FILE_MANAGER, or the platform folder opener.
func FileManager() string {
	if e := os.Getenv("FILE_MANAGER"); e != "" {
		return e
	}
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "explorer"
	default:
		return "xdg-open"
	}
}

// EditFile opens path in $EDITOR and blocks until it exits.
func EditFile(path string) error {
	cmd := exec.Command(Editor(), path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// EditTemp writes content to a temp file, opens it in $EDITOR, and
// returns whatever the user saved. The caller owns applying it.
func EditTemp(ext, content string) (string, error) {
	if !strings.HasPrefix(ext, ".") && ext != "" {
		ext = "." + ext
	}
	f, err := os.CreateTemp("", "azstore-*"+ext)
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := EditFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// OpenPath reveals a file or directory in the platform file manager.
func OpenPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !Exists(abs) {
		return fmt.Errorf("%s does not exist", abs)
	}
	cmd := exec.Command(FileManager(), abs)
	return cmd.Start()
}

// DownloadsDir is the default destination for blob downloads.
func DownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}
