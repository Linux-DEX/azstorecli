// Command azstore is the entry point for azstorecli.
//
// With no subcommand it launches the Bubble Tea TUI. Subcommands like
// `up`, `down`, and `status` are the headless surface described in
// docs/ARCHITECTURE.md §11 — they are stubbed here and gain real bodies
// once the supervisor lands in M2.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Linux-DEX/azstorecli/internal/app"
	"github.com/Linux-DEX/azstorecli/internal/config"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "azstore:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "azstore",
		Short: "A terminal replacement for Azure Storage Explorer, fused with Azurite and Functions Core Tools",
		// Running `azstore` with no subcommand launches the TUI.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}

	root.AddCommand(
		newUpCmd(),
		newDownCmd(),
		newStatusCmd(),
	)

	return root
}

func runTUI() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	root := app.New(cfg)
	p := tea.NewProgram(root, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start the local stack (Azurite + Functions host) headlessly",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(M2): wire to internal/supervisor once it exists.
			fmt.Println("azstore up: not implemented yet — arrives with the M2 supervisor")
			return nil
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the local stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("azstore down: not implemented yet — arrives with the M2 supervisor")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print the status of managed processes",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("azstore status: not implemented yet — arrives with the M2 supervisor")
			return nil
		},
	}
}
