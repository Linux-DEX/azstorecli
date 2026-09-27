package util

import "github.com/atotto/clipboard"

// Yank copies text to the system clipboard.
func Yank(s string) error {
	if s == "" {
		return nil
	}
	return clipboard.WriteAll(s)
}
