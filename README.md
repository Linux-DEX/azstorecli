# azstorecli

A terminal replacement for Azure Storage Explorer, fused with Azurite and
Azure Functions Core Tools, so local Azure development happens in one TUI.

```bash
go install github.com/Linux-DEX/azstorecli/cmd/azstore@latest
cd my-function-app
azstore init
azstore
```

Headless surface (CI-friendly):

```bash
azstore up --detach
azstore seed apply
azstore doctor
azstore down
```

See `docs/ARCHITECTURE.md` and `docs/KEYBINDINGS.md`.
