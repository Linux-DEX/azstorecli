package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// WritePID records this process in azstore.pid.
func WritePID() error {
	return util.WriteAtomic(PIDPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// RemovePID deletes the pid file. It is a no-op when the file is gone.
func RemovePID() {
	_ = os.Remove(PIDPath())
}

// LivePID reports another azstore still running, if the pid file points
// at a live process that is not us. The workspace flock is the real
// mutual-exclusion; this is only so the doctor screen can name the
// other instance.
func LivePID() (int, bool) {
	raw, err := os.ReadFile(PIDPath())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return 0, false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}
	if err := signalAlive(p); err != nil {
		return 0, false
	}
	return pid, true
}

// PIDConflict is the human-readable form of LivePID.
func PIDConflict() string {
	pid, ok := LivePID()
	if !ok {
		return ""
	}
	return fmt.Sprintf("another azstore is running (pid %d)", pid)
}
