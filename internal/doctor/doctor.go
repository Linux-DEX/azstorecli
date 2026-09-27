// Package doctor checks the local toolchain and ports before a start.
package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Linux-DEX/azstorecli/internal/azurite"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/funcs"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
)

// Check is one row of a doctor report.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Run walks the toolchain, ports, workspace, and settings.
func Run(cfg config.Config) []Check {
	var out []Check
	out = append(out, node())
	out = append(out, azuriteRuntime(cfg)...)
	out = append(out, coreTools())
	out = append(out, ports(cfg)...)
	out = append(out, workspace(cfg))
	out = append(out, settings(cfg))
	out = append(out, profiles(cfg))
	if pid := config.PIDConflict(); pid != "" {
		out = append(out, Check{Name: "instance", OK: false, Error: pid})
	} else {
		out = append(out, Check{Name: "instance", OK: true, Detail: "no other azstore running"})
	}
	return out
}

func node() Check {
	p, err := exec.LookPath("node")
	if err != nil {
		return Check{Name: "node", Error: "not on PATH — install Node.js"}
	}
	out, err := exec.Command(p, "--version").Output()
	if err != nil {
		return Check{Name: "node", OK: true, Detail: p}
	}
	return Check{Name: "node", OK: true, Detail: strings.TrimSpace(string(out)) + "  " + p}
}

func azuriteRuntime(cfg config.Config) []Check {
	rt, err := azurite.Resolve(context.Background(), cfg)
	if err != nil {
		return []Check{{Name: "azurite", Error: err.Error()}}
	}
	ver, vErr := azurite.DetectVersion(context.Background(), rt, cfg.Azurite.Image)
	detail := rt.Detail
	if ver != "" {
		detail = ver + "  " + rt.Detail
	} else if vErr != nil {
		detail = rt.Detail + "  (version unknown: " + vErr.Error() + ")"
	}
	return []Check{{Name: "azurite", OK: true, Detail: detail}}
}

func coreTools() Check {
	ver, err := funcs.Version()
	if err != nil {
		return Check{Name: "func", Error: err.Error()}
	}
	return Check{Name: "func", OK: true, Detail: ver}
}

func ports(cfg config.Config) []Check {
	wanted := azurite.Ports(cfg)
	if cfg.Functions.Enabled {
		wanted["functions"] = cfg.Functions.Port
	}
	var out []Check
	for name, port := range wanted {
		if supervisor.PortFree(port) {
			out = append(out, Check{Name: "port " + name, OK: true, Detail: fmt.Sprintf(":%d free", port)})
			continue
		}
		pid, holder, _ := supervisor.PortHolder(port)
		c := supervisor.PortConflict{Service: name, Port: port, PID: pid, Holder: holder}
		out = append(out, Check{Name: "port " + name, Error: c.Error()})
	}
	return out
}

func workspace(cfg config.Config) Check {
	ws, err := azurite.OpenWorkspace(cfg)
	if err != nil {
		return Check{Name: "workspace", Error: err.Error()}
	}
	defer ws.Close()
	if err := ws.Validate(); err != nil {
		return Check{Name: "workspace", Error: err.Error()}
	}
	st := ws.Stat()
	return Check{Name: "workspace", OK: true, Detail: fmt.Sprintf("%s  (%s)", ws.Dir, formatSize(st.SizeBytes))}
}

func settings(cfg config.Config) Check {
	dir := cfg.FunctionAppDir()
	if !funcs.IsFunctionApp(dir) {
		return Check{Name: "host.json", OK: true, Detail: "no function app in " + dir}
	}
	s, err := funcs.LoadSettings(dir)
	if err != nil {
		return Check{Name: "local.settings.json", Error: err.Error()}
	}
	val, ok := s.Get("AzureWebJobsStorage")
	if !ok || val == "" {
		return Check{Name: "local.settings.json", Error: "AzureWebJobsStorage is unset"}
	}
	return Check{Name: "local.settings.json", OK: true, Detail: "AzureWebJobsStorage is set"}
}

func profiles(cfg config.Config) Check {
	store, err := config.LoadProfiles(config.GlobalProfilesPath(), cfg)
	if err != nil {
		return Check{Name: "profiles", Error: err.Error()}
	}
	detail := fmt.Sprintf("%d profiles, active %s", len(store.Profiles), store.Active)
	if store.InsecureMode {
		return Check{Name: "profiles", OK: false, Error: "profiles.yaml is group/world readable — expected mode 0600", Detail: detail}
	}
	return Check{Name: "profiles", OK: true, Detail: detail}
}

func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// Healthy reports whether every check passed.
func Healthy(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// OS is a one-line environment summary.
func OS() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
