package azurite

import (
	"fmt"
	"time"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/supervisor"
)

// Spec builds the supervisor spec for Azurite. The stop signal is
// SIGINT on unix so LokiJS flushes its databases (ARCHITECTURE.md §5.2).
func Spec(cfg config.Config, rt Runtime, ws *Workspace) supervisor.Spec {
	return supervisor.Spec{
		Name:        "azurite",
		Argv:        BuildArgs(cfg, rt, ws.Dir),
		Dir:         ws.Dir,
		Health:      healthFor(cfg),
		Restart:     supervisor.RestartOnFailure,
		MaxRestarts: 3,
		StopSignal:  StopSignal(),
		StopTimeout: 10 * time.Second,
		LogCapacity: cfg.UI.LogCapacity,
	}
}

// healthFor probes whichever service is actually enabled. A blob-only
// health check against a queue-only launch would sit unhealthy forever.
func healthFor(cfg config.Config) supervisor.HealthCheck {
	switch {
	case cfg.HasService("blob"):
		return supervisor.AzuriteHealth(cfg.Azurite.BlobPort)
	case cfg.HasService("queue"):
		return supervisor.HTTPCheck{
			URL:      fmt.Sprintf("http://127.0.0.1:%d/devstoreaccount1?comp=list", cfg.Azurite.QueuePort),
			Accept:   func(code int) bool { return code > 0 && code < 500 },
			Interval: 2 * time.Second,
			Timeout:  time.Second,
			Retries:  15,
		}
	case cfg.HasService("table"):
		return supervisor.HTTPCheck{
			URL:      fmt.Sprintf("http://127.0.0.1:%d/devstoreaccount1/Tables", cfg.Azurite.TablePort),
			Accept:   func(code int) bool { return code > 0 && code < 500 },
			Interval: 2 * time.Second,
			Timeout:  time.Second,
			Retries:  15,
		}
	default:
		return supervisor.AzuriteHealth(cfg.Azurite.BlobPort)
	}
}
