//go:build windows

package azurite

import "os"

// StopSignal on Windows is Interrupt; process_windows.go turns that
// into taskkill /T so the tree still dies together.
func StopSignal() os.Signal { return os.Interrupt }
