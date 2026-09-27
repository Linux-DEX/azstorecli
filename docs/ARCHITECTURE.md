# Architecture

azstorecli is a supervisor + client + browser:

| Subsystem | How azstore talks to it |
|-----------|-------------------------|
| Azurite | Spawn as a child, health-check over HTTP |
| Functions host | `func host start`, parse the route table, invoke over HTTP |
| Storage data | Azure SDK for Go pointed at `127.0.0.1` |

Data is never read from Azurite's LokiJS files. Those are an opaque blob used only for snapshot/restore.

## Packages

`internal/screens/*` may import components, keymap, theme, and services — never each other. Cross-screen messages live in `internal/msg`.

`internal/stack` is the shared runtime used by the TUI and the headless CLI.

## Config precedence

defaults → `~/.config/azstorecli/config.yaml` → `./.azstorecli/config.yaml` → `AZSTORE_*` → flags.

Writes go through `util.WriteAtomic`. `profiles.yaml` is mode `0600`.

## Snapshots

Stop Azurite (SIGINT so Loki flushes) → flock → tar+zstd → manifest → restart. Restore autosaves first. Extract sanitises every tar path.

## Edge cases the code encodes

- Terminals narrower than 100 columns collapse multi-pane explorers to a stack; `h`/`l` push/pop rather than moving focus.
- Real-cloud profiles are read-only unless the file says otherwise. `Ctrl+W` unlocks writes for five minutes.
- `--skipApiVersionCheck` is on by default; the Go SDK's `x-ms-version` otherwise 400s older Azurite.
- Children run in their own process group so `func host start`'s worker dies with the parent.
- Log pumps never block the render loop (lossy event channel + ring buffer).
- Seed fixture paths cannot escape the seed directory.
