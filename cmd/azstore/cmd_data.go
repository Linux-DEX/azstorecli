package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Linux-DEX/azstorecli/internal/config"
	"github.com/Linux-DEX/azstorecli/internal/funcs"
	"github.com/Linux-DEX/azstorecli/internal/keymap"
	"github.com/Linux-DEX/azstorecli/internal/seed"
)

func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot", Short: "Save, restore, and manage workspace archives"}
	cmd.AddCommand(
		&cobra.Command{
			Use: "save <name>", Short: "Snapshot the current workspace", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := loadStack(cmd)
				if err != nil {
					return err
				}
				defer s.Close()
				notes, _ := cmd.Flags().GetString("notes")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				snap, err := s.Snaps.Save(ctx, s.WS, args[0], notes, nil)
				if err != nil {
					return err
				}
				fmt.Printf("saved %s  %d bytes  %s\n", snap.ID, snap.SizeBytes, snap.Checksum)
				return nil
			},
		},
		&cobra.Command{
			Use: "restore <name>", Short: "Restore a snapshot (autosaves first)", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := loadStack(cmd)
				if err != nil {
					return err
				}
				defer s.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				return s.Snaps.Restore(ctx, s.WS, args[0], nil)
			},
		},
		snapshotListCmd(),
		&cobra.Command{
			Use: "export <name> <path>", Short: "Copy an archive to a path", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := loadStack(cmd)
				if err != nil {
					return err
				}
				defer s.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				return s.Snaps.Export(ctx, args[0], args[1])
			},
		},
		&cobra.Command{
			Use: "import <path>", Short: "Import a .tar.zst archive", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := loadStack(cmd)
				if err != nil {
					return err
				}
				defer s.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				snap, err := s.Snaps.Import(ctx, args[0])
				if err != nil {
					return err
				}
				fmt.Println("imported", snap.ID)
				return nil
			},
		},
		snapshotPruneCmd(),
	)
	cmd.Commands()[0].Flags().String("notes", "", "snapshot notes")
	return cmd
}

func snapshotListCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use: "list", Short: "List snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			list, err := s.Snaps.List()
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(list)
			}
			for _, snap := range list {
				fmt.Printf("%-40s  %8d  %s\n", snap.ID, snap.SizeBytes, snap.Name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return c
}

func snapshotPruneCmd() *cobra.Command {
	var older string
	c := &cobra.Command{
		Use: "prune", Short: "Delete snapshots older than a duration",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := time.ParseDuration(older)
			if err != nil {
				return err
			}
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			removed, err := s.Snaps.Prune(d)
			if err != nil {
				return err
			}
			for _, id := range removed {
				fmt.Println("removed", id)
			}
			return nil
		},
	}
	c.Flags().StringVar(&older, "older-than", "720h", "age cutoff (Go duration, 720h = 30d)")
	return c
}

func newSeedCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "seed", Short: "Apply or dump declarative fixtures"}
	apply := &cobra.Command{
		Use: "apply", Short: "Apply seed data through the SDK",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				path = seed.DefaultPath(s.Cfg.ProjectDir())
			}
			doc, err := seed.Load(path)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			res, err := doc.Apply(ctx, s.Clients)
			if err != nil {
				return err
			}
			fmt.Println(res.String())
			return nil
		},
	}
	apply.Flags().String("file", "", "seed file")
	dump := &cobra.Command{
		Use: "dump", Short: "Write live state as a seed document",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			out, _ := cmd.Flags().GetString("out")
			if out == "" {
				out = config.ProjectSeedDir(s.Cfg.ProjectDir())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			doc, err := seed.Dump(ctx, s.Clients, out)
			if err != nil {
				return err
			}
			fmt.Println(doc.Describe())
			return nil
		},
	}
	dump.Flags().String("out", "", "output directory")
	cmd.AddCommand(apply, dump)
	return cmd
}

func newBlobCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "blob", Short: "Headless blob operations"}
	var asJSON bool
	ls := &cobra.Command{
		Use: "ls <container>", Short: "List blobs", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			prefix, _ := cmd.Flags().GetString("prefix")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			page, err := s.Clients.ListBlobs(ctx, args[0], prefix, "", 500)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(page.Entries)
			}
			for _, e := range page.Entries {
				kind := "blob"
				if e.IsDir {
					kind = "dir "
				}
				fmt.Printf("%s  %10d  %s\n", kind, e.Size, e.Name)
			}
			return nil
		},
	}
	ls.Flags().String("prefix", "", "server-side prefix")
	ls.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	put := &cobra.Command{
		Use: "put <container> <local>", Short: "Upload a file", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				name = args[1]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			return s.Clients.Upload(ctx, args[0], name, args[1], nil)
		},
	}
	put.Flags().String("name", "", "blob name")
	get := &cobra.Command{
		Use: "get <container> <blob>", Short: "Download a blob", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			out, _ := cmd.Flags().GetString("out")
			if out == "" {
				out = args[1]
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, err = s.Clients.DownloadToFile(ctx, args[0], args[1], out)
			return err
		},
	}
	get.Flags().String("out", "", "destination path")
	rm := &cobra.Command{
		Use: "rm <container> <blob>", Short: "Delete a blob", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return s.Clients.DeleteBlob(ctx, args[0], args[1])
		},
	}
	cmd.AddCommand(ls, put, get, rm)
	return cmd
}

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "queue", Short: "Headless queue operations"}
	put := &cobra.Command{
		Use: "put <queue>", Short: "Enqueue a message", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			body, _ := cmd.Flags().GetString("body")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			id, err := s.Clients.EnqueueMessage(ctx, args[0], body, false, 0, 0)
			if err != nil {
				return err
			}
			fmt.Println(id)
			return nil
		},
	}
	put.Flags().String("body", "", "message body")
	peek := &cobra.Command{
		Use: "peek <queue>", Short: "Peek messages", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			n, _ := cmd.Flags().GetInt32("n")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			list, err := s.Clients.PeekMessages(ctx, args[0], n)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(list)
		},
	}
	peek.Flags().Int32("n", 32, "message count")
	cmd.AddCommand(put, peek)
	return cmd
}

func newFuncCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "func", Short: "Invoke and list functions"}
	var asJSON bool
	list := &cobra.Command{
		Use: "list", Short: "List discovered functions",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			found, err := funcs.Discover(s.Cfg.FunctionAppDir())
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(os.Stdout).Encode(found)
			}
			for _, f := range found {
				fmt.Printf("%-24s  %-16s  %s\n", f.Name, f.Trigger, f.URL)
			}
			return nil
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	invoke := &cobra.Command{
		Use: "invoke <name>", Short: "Invoke a function", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadStack(cmd)
			if err != nil {
				return err
			}
			defer s.Close()
			body, _ := cmd.Flags().GetString("body")
			if len(body) > 0 && body[0] == '@' {
				data, err := os.ReadFile(body[1:])
				if err != nil {
					return err
				}
				body = string(data)
			}
			client := funcs.NewClient(funcs.BaseURL(s.Cfg))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			resp := client.InvokeNonHTTP(ctx, args[0], body)
			if resp.Err != nil {
				return resp.Err
			}
			fmt.Println(resp.Status, resp.Duration)
			fmt.Println(resp.Body)
			return nil
		},
	}
	invoke.Flags().String("body", "", "request body, or @file")
	cmd.AddCommand(list, invoke)
	return cmd
}

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "keys", Short: "List and validate keybindings"}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "Print every action ID and its keys",
			RunE: func(cmd *cobra.Command, args []string) error {
				km, err := keymap.Load(config.GlobalKeymapPath())
				if err != nil {
					return err
				}
				for _, line := range km.ListIDs() {
					fmt.Println(line)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "check", Short: "Validate keymap.yaml",
			RunE: func(cmd *cobra.Command, args []string) error {
				_, err := keymap.Load(config.GlobalKeymapPath())
				if err != nil {
					return err
				}
				fmt.Println("keymap ok")
				return nil
			},
		},
	)
	return cmd
}
