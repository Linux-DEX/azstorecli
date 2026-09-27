// Package funcs drives Azure Functions Core Tools: spawning the host,
// discovering its functions, and invoking them.
package funcs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
)

// ErrCoreToolsMissing means `func` is not installed.
var ErrCoreToolsMissing = fmt.Errorf("azure functions core tools not found")

// Locate finds the `func` binary.
func Locate() (string, error) {
	p, err := exec.LookPath("func")
	if err != nil {
		return "", fmt.Errorf("%w: install with `npm i -g azure-functions-core-tools@4`", ErrCoreToolsMissing)
	}
	return p, nil
}

// IsFunctionApp reports whether dir looks like a Functions project.
// host.json is the one file every model — in-process, isolated, Node v3
// and v4, Python v1 and v2 — still has.
func IsFunctionApp(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "host.json"))
	return err == nil
}

// Spec builds the supervisor spec for the Functions host.
//
// It depends on azurite: starting the host first makes it fail its
// storage connection check and exit, with an error that points at
// AzureWebJobsStorage rather than at the emulator not being up yet.
func Spec(cfg config.Config) (supervisor.Spec, error) {
	bin, err := Locate()
	if err != nil {
		return supervisor.Spec{}, err
	}
	dir := cfg.FunctionAppDir()
	if !IsFunctionApp(dir) {
		return supervisor.Spec{}, fmt.Errorf("no host.json in %s — not a functions app", dir)
	}

	argv := []string{bin, "host", "start", "--port", strconv.Itoa(cfg.Functions.Port)}
	if cfg.Functions.Verbose {
		argv = append(argv, "--verbose")
	}
	argv = append(argv, cfg.Functions.ExtraArgs...)

	env := make([]string, 0, len(cfg.Functions.Env))
	for k, v := range cfg.Functions.Env {
		env = append(env, k+"="+v)
	}
	// Point the host at our Azurite ports when they are non-default;
	// UseDevelopmentStorage=true hardcodes 10000-10002.
	if cfg.Azurite.BlobPort != 10000 || cfg.Azurite.QueuePort != 10001 || cfg.Azurite.TablePort != 10002 {
		env = append(env, "AzureWebJobsStorage="+config.AzuriteConnStrPorts(
			cfg.Azurite.BlobPort, cfg.Azurite.QueuePort, cfg.Azurite.TablePort))
	}

	return supervisor.Spec{
		Name:      "functions",
		Argv:      argv,
		Dir:       dir,
		Env:       env,
		DependsOn: []string{"azurite"},
		Health:    supervisor.FunctionsHealth(cfg.Functions.Port),
		// A crashed host is nearly always a code error the developer is
		// about to fix; restarting in a loop just buries the stack trace.
		Restart:     supervisor.RestartNever,
		StopTimeout: 5 * time.Second,
		LogCapacity: cfg.UI.LogCapacity,
	}, nil
}

// BaseURL is the host's local address.
func BaseURL(cfg config.Config) string {
	return fmt.Sprintf("http://127.0.0.1:%d", cfg.Functions.Port)
}

// Version reports the installed Core Tools version, for doctor.
func Version() (string, error) {
	bin, err := Locate()
	if err != nil {
		return "", err
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "", err
	}
	return trimLine(string(out)), nil
}
