package app

import (
	"github.com/Linux-DEX/azstorecli/internal/stack"
)

// Deps is the container handed to the root model. Screens receive a
// narrower slice of it at construction time.
type Deps struct {
	Stack *stack.Stack
}

// NewDeps wraps an already-opened stack.
func NewDeps(s *stack.Stack) Deps { return Deps{Stack: s} }
