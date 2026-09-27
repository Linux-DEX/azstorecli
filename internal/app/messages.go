package app

// Cross-screen messages live in internal/msg. This file exists so the
// architecture's "messages.go" path still points at a real file that
// documents the rule: screens never import each other.
import _ "github.com/Linux-DEX/azstorecli/internal/msg"
