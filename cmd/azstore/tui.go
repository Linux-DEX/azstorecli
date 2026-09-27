package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/Linux-DEX/azstorecli/internal/app"
	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/stack"
)

func runTUI(flags *pflag.FlagSet) error {
	cfg, err := config.Load(flags)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	s, err := stack.Open(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	p := tea.NewProgram(app.New(s), tea.WithAltScreen())
	_, err = p.Run()
	return err
}
