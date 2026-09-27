//go:build windows

package supervisor

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// DefaultStopSignal on Windows is only ever used as a marker: there is
// no kill(2), so stopping always routes through taskkill.
var DefaultStopSignal os.Signal = os.Interrupt

// configureProcAttr gives the child its own console process group so a
// Ctrl+Break can address the tree, and so the child does not inherit
// azstore's console handlers.
func configureProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// signalTree approximates a graceful group signal with `taskkill /T`,
// which walks the child tree the way -pgid does on unix. Without /T the
// Functions host's language worker survives and keeps port 7071 bound.
func signalTree(p *os.Process, _ os.Signal) error {
	return taskkill(p.Pid, false)
}

// killTree is the forced equivalent.
func killTree(p *os.Process) error {
	return taskkill(p.Pid, true)
}

func taskkill(pid int, force bool) error {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	cmd := exec.Command("taskkill", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		// taskkill exits non-zero when the tree is already gone; fall
		// back to the direct kill so a genuine failure still surfaces.
		return p2Kill(pid)
	}
	return nil
}

func p2Kill(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
