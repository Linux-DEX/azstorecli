package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/doctor"
	"github.com/Linux-DEX/azstorecli/internal/stack"
)

func loadStack(cmd *cobra.Command) (*stack.Stack, error) {
	cfg, err := config.Load(cmd.Flags())
	if err != nil {
		return nil, err
	}
	return stack.Open(cfg)
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold .azstorecli/ in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Flags())
			if err != nil {
				return err
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := stack.InitProject(cfg, wd); err != nil {
				return err
			}
			fmt.Println("wrote", config.ProjectConfigPath(wd))
			return nil
		},
	}
}

func newUpCmd() *cobra.Command {
	var detach bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start Azurite and the Functions host",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := s.Start(ctx); err != nil {
				s.Close()
				return err
			}
			if detach {
				fmt.Println("stack started")
				// Leave children running; drop the lock so a later `down` can take it.
				_ = s.WS.Unlock()
				s.Sup.Close()
				return nil
			}
			fmt.Println("stack up — Ctrl+C to stop")
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt)
			<-sig
			return s.Close()
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "start and return")
	return cmd
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the local stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return s.Stop(ctx)
		},
	}
}

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the status of managed processes",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			snap := s.Sup.Snapshot()
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(snap)
			}
			for _, st := range snap {
				fmt.Printf("%-12s %-10s pid=%-6d up=%s\n", st.Name, st.State, st.PID, st.Uptime.Truncate(time.Second))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check node, func, ports, and versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Flags())
			if err != nil {
				return err
			}
			checks := doctor.Run(cfg)
			ok := true
			for _, c := range checks {
				mark := "ok "
				if !c.OK {
					mark = "ERR"
					ok = false
				}
				line := c.Detail
				if !c.OK {
					line = c.Error
				}
				fmt.Printf("%s  %-18s %s\n", mark, c.Name, line)
			}
			if !ok {
				os.Exit(1)
			}
			return nil
		},
	}
}
