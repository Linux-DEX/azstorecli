# Keybindings

User overrides live in `~/.config/azstorecli/keymap.yaml` and merge by action ID. `azstore keys list` prints every ID; `azstore keys check` validates the file.

## Global

| Key | Action |
|-----|--------|
| 1–8 | Jump to screen |
| Tab / Shift+Tab | Cycle panes |
| `:` / Ctrl+K | Command palette |
| `/` | Filter |
| `?` | Full help |
| Esc | Close modal / clear filter |
| q | Back; on root, quit prompt |
| Ctrl+C | Quit now |
| Ctrl+R | Refresh |
| Ctrl+L | Log drawer |
| Ctrl+P | Profiles |
| Ctrl+S | Snapshot |
| Ctrl+W | Unlock read-only profile for 5 minutes |
| j/k/h/l | Navigate (arrows alias) |
| g / G | Top / bottom |
| y | Yank identifier |

## Screens

- **Dashboard** `s/x/r` start/stop/restart · `S/X/R` all · `d` doctor · `c` copy connstr
- **Blob** `u/d/D` upload/download/delete · `n` new · `s` SAS · `e` edit · `v` hex
- **Queue** `a` add · `D` delete · `C` clear · `w` watch · `t` trigger bound function
- **Table** `f` OData filter · `E` edit · `D` delete · `>/</` page
- **Functions** Enter invoke · `s/x/r` host · `w` watch · `E` settings
- **Logs** Space pause · `1/2/3` source · `f` filter · `x` clear
- **Snapshots** `n` new · Enter restore · `D` delete · `v` verify
- **Profiles** Enter activate · `t` test · `R` toggle read-only

Destructive confirms never default the cursor onto the destructive button. Confirm with `y` or Enter on the focused button.
