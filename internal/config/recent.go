package config

import (
	"encoding/json"
	"os"

	"github.com/Linux-DEX/azstorecli/internal/util"
)

// Recent is the last-session crumb file under XDG state.
type Recent struct {
	LastScreen string `json:"lastScreen"`
	Project    string `json:"project"`
	Profile    string `json:"profile"`
}

// LoadRecent reads recent.json. A missing file is an empty Recent.
func LoadRecent() Recent {
	raw, err := os.ReadFile(RecentPath())
	if err != nil {
		return Recent{}
	}
	var r Recent
	_ = json.Unmarshal(raw, &r)
	return r
}

// SaveRecent writes recent.json atomically.
func SaveRecent(r Recent) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteAtomic(RecentPath(), append(data, '\n'), 0o600)
}
