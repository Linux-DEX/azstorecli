//go:build unix

package stack

import "syscall"

func killPID(pid int) error {
	pgid, err := syscall.Getpgid(pid)
	if err == nil && pgid > 1 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}
