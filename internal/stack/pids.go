package stack

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/util"
)

// childPIDs is what `azstore up --detach` leaves behind so `azstore down`
// can find the processes after this process has exited.
type childPIDs struct {
	Azurite   int `json:"azurite,omitempty"`
	Functions int `json:"functions,omitempty"`
}

func childPIDPath() string {
	return filepath.Join(config.StateDir(), "children.json")
}

func (s *Stack) saveChildPIDs() error {
	var p childPIDs
	if proc, err := s.Sup.Get("azurite"); err == nil {
		p.Azurite = proc.PID()
	}
	if proc, err := s.Sup.Get("functions"); err == nil {
		p.Functions = proc.PID()
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return util.WriteAtomic(childPIDPath(), data, 0o644)
}

func loadChildPIDs() childPIDs {
	raw, err := os.ReadFile(childPIDPath())
	if err != nil {
		return childPIDs{}
	}
	var p childPIDs
	_ = json.Unmarshal(raw, &p)
	return p
}

func clearChildPIDs() { _ = os.Remove(childPIDPath()) }

// ReapDetached kills processes recorded by a previous `up --detach`.
func ReapDetached() error {
	p := loadChildPIDs()
	var last error
	for _, pid := range []int{p.Functions, p.Azurite} {
		if pid <= 0 {
			continue
		}
		if err := killPID(pid); err != nil {
			last = err
		}
	}
	clearChildPIDs()
	return last
}
