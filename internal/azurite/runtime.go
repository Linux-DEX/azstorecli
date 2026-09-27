// Package azurite resolves how to run Azurite, builds its argv, and owns
// the workspace directory it persists into.
//
// azstorecli never parses Azurite's on-disk files to read data: they are
// LokiJS dumps whose schema changes between releases. Data is read
// through the REST API like any other client. The files here are treated
// as an opaque blob, and only for snapshot and restore.
package azurite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/config"
)

// Kind names a way of launching Azurite.
type Kind string

const (
	KindNPX    Kind = "npx"
	KindGlobal Kind = "global"
	KindDocker Kind = "docker"
	KindPath   Kind = "path"
)

// Runtime is a resolved, launchable Azurite.
type Runtime struct {
	Kind Kind
	// Base is the argv prefix; azurite flags are appended to it.
	Base []string
	// Version is the reported Azurite version, empty if undetectable.
	Version string
	// Detail describes the resolution for the doctor screen.
	Detail string
	// InContainer is true for docker, where --location must be a path
	// inside the container rather than on the host.
	InContainer bool
}

// ErrRuntimeUnavailable means the configured runtime is not installed.
var ErrRuntimeUnavailable = errors.New("azurite runtime unavailable")

// Resolve turns the configured runtime choice into a launchable Runtime,
// failing early with an actionable message rather than letting exec
// report a bare "executable file not found".
func Resolve(ctx context.Context, cfg config.Config) (Runtime, error) {
	switch Kind(cfg.Azurite.Runtime) {
	case KindNPX:
		npx, err := exec.LookPath("npx")
		if err != nil {
			return Runtime{}, fmt.Errorf("%w: npx not on PATH — install Node.js, or set azurite.runtime to docker", ErrRuntimeUnavailable)
		}
		// -y stops npx prompting to install on a fresh machine, which
		// would otherwise hang the spawn forever with no visible cause.
		return Runtime{
			Kind:   KindNPX,
			Base:   []string{npx, "-y", "azurite"},
			Detail: npx + " -y azurite",
		}, nil

	case KindGlobal:
		bin, err := exec.LookPath("azurite")
		if err != nil {
			if vs := vscodeBundled(); vs != "" {
				node, nodeErr := exec.LookPath("node")
				if nodeErr == nil {
					return Runtime{Kind: KindGlobal, Base: []string{node, vs}, Detail: "VS Code bundled: " + vs}, nil
				}
			}
			return Runtime{}, fmt.Errorf("%w: `azurite` not on PATH — run `npm i -g azurite`", ErrRuntimeUnavailable)
		}
		return Runtime{Kind: KindGlobal, Base: []string{bin}, Detail: bin}, nil

	case KindDocker:
		docker, err := exec.LookPath("docker")
		if err != nil {
			return Runtime{}, fmt.Errorf("%w: docker not on PATH", ErrRuntimeUnavailable)
		}
		return Runtime{
			Kind:        KindDocker,
			Base:        []string{docker},
			Detail:      "docker run " + cfg.Azurite.Image,
			InContainer: true,
		}, nil

	case KindPath:
		p := cfg.Resolve(cfg.Azurite.Path)
		info, err := os.Stat(p)
		if err != nil {
			return Runtime{}, fmt.Errorf("%w: azurite.path %q: %v", ErrRuntimeUnavailable, p, err)
		}
		if info.IsDir() {
			return Runtime{}, fmt.Errorf("%w: azurite.path %q is a directory", ErrRuntimeUnavailable, p)
		}
		// A .js entry point needs node in front of it; a native binary
		// or shell wrapper is executed directly.
		if strings.EqualFold(filepath.Ext(p), ".js") {
			node, nodeErr := exec.LookPath("node")
			if nodeErr != nil {
				return Runtime{}, fmt.Errorf("%w: %q is a script but node is not on PATH", ErrRuntimeUnavailable, p)
			}
			return Runtime{Kind: KindPath, Base: []string{node, p}, Detail: node + " " + p}, nil
		}
		return Runtime{Kind: KindPath, Base: []string{p}, Detail: p}, nil

	default:
		return Runtime{}, fmt.Errorf("unknown azurite.runtime %q", cfg.Azurite.Runtime)
	}
}

var versionRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// DetectVersion asks Azurite for its version. Surfacing this in the
// doctor screen turns the x-ms-version mismatch class of bug from an
// opaque 400 into something a user can act on.
func DetectVersion(ctx context.Context, rt Runtime, image string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var argv []string
	if rt.Kind == KindDocker {
		argv = append(append([]string{}, rt.Base...), "run", "--rm", "--entrypoint", "azurite", image, "--version")
	} else {
		argv = append(append([]string{}, rt.Base...), "--version")
	}

	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	if m := versionRe.FindStringSubmatch(string(out)); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("could not parse azurite version from %q", strings.TrimSpace(string(out)))
}

// vscodeBundled looks for the Azurite VS Code extension's bundled copy.
// It is the last resort before telling the user to install something —
// plenty of machines have it and nothing else.
func vscodeBundled() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	roots := []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
		filepath.Join(home, ".vscode-server", "extensions"),
	}
	if runtime.GOOS == "windows" {
		roots = append(roots, filepath.Join(home, ".vscode", "extensions"))
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), "azurite.azurite-") {
				continue
			}
			candidate := filepath.Join(root, e.Name(), "dist", "azurite.js")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// Available reports which runtimes this machine could use, for the
// onboarding wizard and the doctor screen.
func Available() map[Kind]string {
	found := map[Kind]string{}
	if p, err := exec.LookPath("npx"); err == nil {
		found[KindNPX] = p
	}
	if p, err := exec.LookPath("azurite"); err == nil {
		found[KindGlobal] = p
	} else if vs := vscodeBundled(); vs != "" {
		found[KindGlobal] = vs
	}
	if p, err := exec.LookPath("docker"); err == nil {
		found[KindDocker] = p
	}
	return found
}
