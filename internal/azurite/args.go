package azurite

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/Linux-DEX/azstorecli/internal/config"
)

// containerWorkspace is where the host workspace is bind-mounted inside
// the Azurite image.
const containerWorkspace = "/data"

// BuildArgs turns configuration plus a resolved runtime into the full
// argv for one Azurite launch.
//
// Service selection matters: `azurite` starts all three, but
// `azurite-blob` only starts blob. Running the full trio when the user
// only asked for blob would bind two ports they never approved, so a
// partial selection switches to the single-service entry points.
func BuildArgs(cfg config.Config, rt Runtime, hostWorkspace string) []string {
	location := hostWorkspace
	argv := append([]string{}, rt.Base...)

	if rt.Kind == KindDocker {
		location = containerWorkspace
		argv = append(argv,
			"run", "--rm",
			// Without --init, PID 1 in the container ignores SIGINT and
			// the graceful "let Loki flush" stop degrades to a SIGKILL.
			"--init",
			"-v", hostWorkspace+":"+containerWorkspace,
		)
		for _, p := range publishedPorts(cfg) {
			argv = append(argv, "-p", fmt.Sprintf("127.0.0.1:%d:%d", p, p))
		}
		argv = append(argv, "--entrypoint", entrypoint(cfg), cfg.Azurite.Image)
	} else if bin := entrypoint(cfg); bin != "azurite" {
		// Swap `azurite` for `azurite-blob` and friends so a partial
		// service list does not bind ports the user never enabled.
		argv = swapBinary(argv, bin)
	}

	argv = append(argv, "--location", location)

	if cfg.HasService("blob") {
		argv = append(argv, "--blobHost", "0.0.0.0", "--blobPort", strconv.Itoa(cfg.Azurite.BlobPort))
	}
	if cfg.HasService("queue") {
		argv = append(argv, "--queueHost", "0.0.0.0", "--queuePort", strconv.Itoa(cfg.Azurite.QueuePort))
	}
	if cfg.HasService("table") {
		argv = append(argv, "--tableHost", "0.0.0.0", "--tablePort", strconv.Itoa(cfg.Azurite.TablePort))
	}

	if cfg.Azurite.Loose {
		argv = append(argv, "--loose")
	}
	if cfg.Azurite.SkipAPIVersionCheck {
		argv = append(argv, "--skipApiVersionCheck")
	}
	if cfg.Azurite.Silent {
		argv = append(argv, "--silent")
	}
	if cfg.Azurite.Debug {
		argv = append(argv, "--debug", debugLogPath(cfg, location))
	}
	if cfg.Azurite.Cert != "" && cfg.Azurite.Key != "" {
		argv = append(argv, "--cert", cfg.Resolve(cfg.Azurite.Cert), "--key", cfg.Resolve(cfg.Azurite.Key))
	}

	return append(argv, cfg.Azurite.ExtraArgs...)
}

// swapBinary replaces the trailing `azurite` token of an argv prefix
// with a single-service variant, keeping the directory for an absolute
// path so a globally installed copy resolves to its own sibling.
func swapBinary(argv []string, bin string) []string {
	last := len(argv) - 1
	dir := filepath.Dir(argv[last])
	if dir == "." || dir == "" {
		argv[last] = bin
		return argv
	}
	argv[last] = filepath.Join(dir, bin)
	return argv
}

// entrypoint picks the binary matching the enabled services.
func entrypoint(cfg config.Config) string {
	if len(cfg.Azurite.Services) == 3 {
		return "azurite"
	}
	if len(cfg.Azurite.Services) == 1 {
		return "azurite-" + cfg.Azurite.Services[0]
	}
	return "azurite"
}

// publishedPorts lists the loopback ports docker must forward. We bind
// 127.0.0.1 explicitly: publishing an emulator with a well-known,
// published account key on 0.0.0.0 would expose it to the whole LAN.
func publishedPorts(cfg config.Config) []int {
	var ports []int
	if cfg.HasService("blob") {
		ports = append(ports, cfg.Azurite.BlobPort)
	}
	if cfg.HasService("queue") {
		ports = append(ports, cfg.Azurite.QueuePort)
	}
	if cfg.HasService("table") {
		ports = append(ports, cfg.Azurite.TablePort)
	}
	return ports
}

// debugLogPath keeps Azurite's own debug log inside the workspace so the
// snapshot skip list can exclude it in one place.
func debugLogPath(_ config.Config, location string) string {
	return location + "/debug.log"
}

// Ports maps service name to configured port, for conflict checks.
func Ports(cfg config.Config) map[string]int {
	out := map[string]int{}
	if cfg.HasService("blob") {
		out["azurite blob"] = cfg.Azurite.BlobPort
	}
	if cfg.HasService("queue") {
		out["azurite queue"] = cfg.Azurite.QueuePort
	}
	if cfg.HasService("table") {
		out["azurite table"] = cfg.Azurite.TablePort
	}
	return out
}
