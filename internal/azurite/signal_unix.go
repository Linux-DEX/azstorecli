//go:build unix

package azurite

import (
	"os"
	"syscall"
)

// StopSignal is SIGINT: Azurite's LokiJS databases flush on SIGINT,
// and a torn snapshot is the cost of using anything else.
func StopSignal() os.Signal { return syscall.SIGINT }
