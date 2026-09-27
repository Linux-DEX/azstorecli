package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.Flags())
		},
	}
	bindConfigFlags(root.PersistentFlags())

	root.AddCommand(
		newInitCmd(),
		newUpCmd(),
		newDownCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newSnapshotCmd(),
		newSeedCmd(),
		newBlobCmd(),
		newQueueCmd(),
		newFuncCmd(),
		newKeysCmd(),
	)
	return root
}

func bindConfigFlags(fs *pflag.FlagSet) {
	fs.String("ui.theme", "", "theme name")
	fs.String("azurite.runtime", "", "npx | global | docker | path")
	fs.Int("azurite.blobPort", 0, "Azurite blob port")
	fs.Int("azurite.queuePort", 0, "Azurite queue port")
	fs.Int("azurite.tablePort", 0, "Azurite table port")
	fs.Int("functions.port", 0, "Functions host port")
}
