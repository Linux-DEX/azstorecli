//go:build unix

package supervisor

import (
	"os"
	"os/exec"
	"syscall"
)

// DefaultStopSignal is what a Spec gets when it names no signal.
// Azurite overrides this with SIGINT so LokiJS flushes its database on
// the way out (see ARCHITECTURE.md §5.2).
var DefaultStopSignal os.Signal = syscall.SIGTERM

// configureProcAttr puts the child in its own process group.
//
// `func host start` forks a language worker (node/dotnet/python). Signal
// only the parent and the worker is orphaned still holding port 7071,
// and the next start fails with a bind error that points nowhere near
// the real cause. Its own group means one kill reaches the whole tree.
func configureProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalTree delivers sig to the child's entire process group. It falls
// back to signalling the bare PID if the group lookup fails, which
// happens when the child has already been reaped.
func signalTree(p *os.Process, sig os.Signal) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return p.Signal(sig)
	}
	pgid, err := syscall.Getpgid(p.Pid)
	if err != nil {
		return p.Signal(sig)
	}
	// Negative PID addresses the group. Guard against pgid 0/1: a failed
	// Setpgid would otherwise turn this into "signal every process I own".
	if pgid <= 1 {
		return p.Signal(sig)
	}
	return syscall.Kill(-pgid, sysSig)
}

// killTree is the non-negotiable SIGKILL after the graceful timeout.
func killTree(p *os.Process) error {
	return signalTree(p, syscall.SIGKILL)
}
