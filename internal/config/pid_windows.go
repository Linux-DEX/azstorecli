//go:build windows

package config

import "os"

func signalAlive(p *os.Process) error {
	// FindProcess always succeeds on Windows; a subsequent OpenProcess
	// via Signal(0) is not available, so we treat any found PID as live.
	// The workspace flock is still the authority.
	_ = p
	return nil
}
