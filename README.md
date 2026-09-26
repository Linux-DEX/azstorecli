# azstorecli

A terminal replacement for Azure Storage Explorer, fused with Azurite and
Azure Functions Core Tools, so local Azure development happens in one TUI.

This repo currently contains the **M1 — Foundation** scaffold: the dead TUI
dependencies are gone, `internal/` replaces `pkg/`, config loading works,
and an empty Bubble Tea shell boots with screen routing and a help bar.
Nothing talks to Azurite or the Functions host yet — that starts at M2.

## Status: M1 (Foundation)

- [x] Cobra root command (`azstore`)
- [x] Config loading: defaults → global YAML → project YAML → env → flags
- [x] `WriteAtomic` for crash-safe config writes
- [x] Theme + keymap skeletons
- [x] Bubble Tea root model with screen routing and a help bar
- [ ] Supervisor, Dashboard screen (M2)
- [ ] Blob / Queue / Table explorers (M3–M4)
- [ ] Functions host integration (M5)
- [ ] Snapshots + seed data (M6)
- [ ] Command palette, profiles, GoReleaser (M7)

## Requirements

- Go 1.23+
- Node.js (for `azurite`, via `npx`) — only needed once M2 lands
- Azure Functions Core Tools (`func`) — only needed once M5 lands

## Getting started

```bash
git clone https://github.com/yourorg/azstorecli.git
cd azstorecli
go mod tidy      # resolves go.sum against the requires in go.mod
go run ./cmd/azstore
```

Running with no subcommand launches the TUI shell. It currently shows a
single placeholder Dashboard screen so you can confirm the routing, help
bar, and keymap are wired correctly before building real screens on top.

Headless subcommands are stubbed and print a "not implemented yet" message:

```bash
go run ./cmd/azstore up
go run ./cmd/azstore down
go run ./cmd/azstore status
```

## Configuration

Global config lives under your OS's XDG config dir (resolved via `adrg/xdg`),
e.g. `~/.config/azstorecli/config.yaml` on Linux. A project-local
`.azstorecli/config.yaml` overrides it when present. See
`internal/config/defaults.go` for the full default set and
`docs/ARCHITECTURE.md` for the precedence order and on-disk layout.

## Project layout

```
cmd/azstore/        cobra root, dispatches to the TUI or headless subcommands
internal/app/       Bubble Tea root model — the only place that knows all screens
internal/screens/   one package per screen; each is a tea.Model
internal/config/    koanf-backed config loading and profiles
internal/keymap/    typed key.Binding set, user overrides
internal/theme/     lipgloss style set + embedded themes
internal/util/      atomic file writes and other small helpers
docs/               architecture and keybinding reference
```

**Rule:** `internal/screens/*` may import `components`, `keymap`, `theme`,
and service interfaces — never each other. Cross-screen communication goes
through `tea.Msg` values defined in `internal/app/messages.go`.

## License

MIT — see [LICENSE](LICENSE). Swap this out before publishing if your org
needs something else.
