# azstorecli

A terminal replacement for Azure Storage Explorer, fused with Azurite and
Azure Functions Core Tools. Local Azure development — emulator, storage
browser, function invoke, snapshots — in one TUI, plus a headless CLI
for scripts and CI.

```
azstore                 # TUI
azstore up --detach     # start Azurite (+ Functions host if present)
azstore seed apply      # load committed fixtures
azstore down            # stop the stack
```

## Requirements

| Tool | Needed for |
|------|------------|
| Go 1.23+ | build |
| Node.js + `npx` | Azurite (default runtime) |
| Azure Functions Core Tools (`func`) | Functions host only |

Install Core Tools when you have a function app:

```bash
npm i -g azure-functions-core-tools@4
```

Azurite-only use does not need `func`. `azstore doctor` reports what is missing.

Azurite can also be a global `azurite` binary, Docker
(`mcr.microsoft.com/azure-storage/azurite`), or a path to a VS Code-bundled
copy. Set `azurite.runtime` to `npx`, `global`, `docker`, or `path`.

## Install

From this repo:

```bash
git clone https://github.com/Linux-DEX/azstorecli.git
cd azstorecli
make build          # writes bin/azstore
# or: go install ./cmd/azstore
```

From a published tag:

```bash
go install github.com/Linux-DEX/azstorecli/cmd/azstore@latest
```

## First run

Inside a Functions app (directory with `host.json`):

```bash
cd my-function-app
azstore init        # writes .azstorecli/, gitignores the live workspace,
                    # patches local.settings.json → UseDevelopmentStorage=true
azstore doctor      # node, azurite, func, ports
azstore             # TUI; S on the dashboard starts the stack
```

Without a function app, skip `init`. `azstore` still starts and can run
Azurite against a project or global workspace.

If you open the TUI in a function app that has no `.azstorecli/` yet, it
asks whether to initialise. Confirming does the same work as `azstore init`.

## TUI

`azstore` with no subcommand opens the TUI.

```
1 Dashboard   2 Blob   3 Queue   4 Table   5 Functions   6 Logs   7 Snapshots   8 Profiles
```

| Key | What it does |
|-----|----------------|
| `1`–`8` | Jump to a screen |
| `:` or `Ctrl+K` | Command palette |
| `/` | Filter the current list |
| `?` | Full keybinding overlay |
| `Tab` / `Shift+Tab` | Cycle panes |
| `j` `k` `h` `l` | Move (arrows work too) |
| `Ctrl+S` | Snapshot the workspace |
| `Ctrl+P` | Switch connection profile |
| `Ctrl+L` | Log drawer |
| `Ctrl+R` | Refresh |
| `Ctrl+W` | Unlock a read-only (real-cloud) profile for 5 minutes |
| `q` | Back; on the root screen, quit prompt |
| `Ctrl+C` | Quit now and stop child processes |

On the dashboard: `s`/`x`/`r` start/stop/restart the selected service,
`S`/`X`/`R` do the same for the whole stack, `d` runs doctor.

Destructive actions (`D` delete, clear queue, restore) open a confirm
modal. The cursor starts on Cancel; confirm with `y` or Enter on the
focused button.

A terminal narrower than 100 columns collapses the three-pane explorers
to a single pane. `h`/`l` then push/pop instead of moving focus.

The full map, including per-screen keys, is in [docs/KEYBINDINGS.md](docs/KEYBINDINGS.md).
Override any action in `~/.config/azstorecli/keymap.yaml` (`azstore keys list`
prints every ID).

## Headless CLI

Everything the TUI does is also a command.

### Stack

```bash
azstore up                  # start Azurite, then Functions if host.json exists
azstore up --detach         # start and return; azstore down still stops them
azstore down
azstore status              # azstore status --json
azstore doctor              # exits 1 if anything failed
```

`up` refuses to start if the workspace is locked by another azstore, or
if ports 10000/10001/10002/7071 are already taken.

### Snapshots

Azurite is stopped first so LokiJS flushes. A torn live copy is how you
get silently empty blobs after restore.

```bash
azstore snapshot save clean-seed --notes "after seed apply"
azstore snapshot list
azstore snapshot restore clean-seed
azstore snapshot export clean-seed ./clean-seed.tar.zst
azstore snapshot import ./clean-seed.tar.zst
azstore snapshot prune --older-than 720h
```

Restore autosaves the current workspace to `autosave-before-restore` first.

### Seed data (what belongs in git)

Snapshots are binary and locked to the Azurite version that made them.
Committed fixtures go in `.azstorecli/seed/containers.yaml` and are
applied through the Azure SDK:

```bash
azstore seed apply
azstore seed apply --file .azstorecli/seed/containers.yaml
azstore seed dump --out .azstorecli/seed/    # live state → yaml + fixtures
```

Apply is idempotent: create-if-not-exists, skip blobs whose content already
matches.

### Storage and functions

```bash
azstore blob ls orders --prefix samples/
azstore blob put orders ./order.json --name samples/order-001.json
azstore blob get orders samples/order-001.json --out ./order.json
azstore blob rm  orders samples/order-001.json

azstore queue put order-events --body '{"event":"OrderCreated"}'
azstore queue peek order-events -n 32

azstore func list
azstore func invoke HttpCreateOrder --body @./payload.json
```

### Keybindings

```bash
azstore keys list     # every action ID and its current keys
azstore keys check    # validate ~/.config/azstorecli/keymap.yaml
```

### Flags (any command)

```
--azurite.runtime npx|global|docker|path
--azurite.blobPort 10000
--azurite.queuePort 10001
--azurite.tablePort 10002
--functions.port 7071
--ui.theme dark|light|nord
```

Environment variables use the `AZSTORE_` prefix, dots as underscores:
`AZSTORE_UI_THEME=nord`, `AZSTORE_AZURITE_BLOBPORT=20000`.

## Configuration

Merged last-wins: built-in defaults → `~/.config/azstorecli/config.yaml`
→ `./.azstorecli/config.yaml` → `AZSTORE_*` → flags.

```
~/.config/azstorecli/
  config.yaml
  keymap.yaml          # optional
  profiles.yaml        # mode 0600; may hold real account keys

~/.local/share/azstorecli/
  workspaces/          # named global Azurite workspaces
  snapshots/           # .tar.zst + sidecar manifests
  logs/azstore.log

my-function-app/
  .azstorecli/
    config.yaml        # commit this
    seed/              # commit this
    workspace/         # gitignored: live Azurite data
```

A project `.azstorecli/config.yaml` looks like:

```yaml
version: 1
project:
  name: order-processor
  functionAppPath: .

azurite:
  runtime: npx              # npx | global | docker | path
  workspaceMode: project    # project | global | temp
  workspaceDir: .azstorecli/workspace
  blobPort: 10000
  queuePort: 10001
  tablePort: 10002
  skipApiVersionCheck: true # keep this; older Azurite 400s the Go SDK otherwise

functions:
  runtime: node
  port: 7071
  watch: true
  env:
    AzureWebJobsStorage: UseDevelopmentStorage=true

ui:
  theme: dark
  startScreen: dashboard
  confirmDestructive: true

autostart: [azurite, functions]
```

Real-cloud profiles in `profiles.yaml` are read-only unless you set
`readonly: false`. Mutating keys then refuse until `Ctrl+W`.

## CI

```yaml
- run: azstore up --detach
- run: azstore seed apply
- run: npm test
- run: azstore snapshot save ci-failure --notes "$GITHUB_RUN_ID"
  if: failure()
- run: azstore down
  if: always()
```

## Docs

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — packages, config, snapshots, process model
- [docs/KEYBINDINGS.md](docs/KEYBINDINGS.md) — full keymap

## License

MIT — see [LICENSE](LICENSE).
