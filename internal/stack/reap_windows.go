//go:build windows

package stack

import (
	"os"
	"os/exec"
	"strconv"
)

func killPID(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T")
	if err := cmd.Run(); err == nil {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
