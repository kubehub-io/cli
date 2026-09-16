package main

import (
	"os"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

var verbose bool
var autoYes bool

func main() {
	cfg, err := kubehubcli.LoadConfig()
	if err != nil {
		errorExit("loading config: %v", err)
	}

	rootCmd := &cobra.Command{
		Use:   "kubehubcli",
		Short: "Nest control utility for host operations",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			kubehubcli.SetAutoAccept(autoYes)
		},
	}
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&autoYes, "yes", "y", false, "skip all confirmation prompts")
	rootCmd.AddCommand(setupCmd(cfg))
	rootCmd.AddCommand(resetCmd(cfg))
	rootCmd.AddCommand(destroyCmd(cfg))
	rootCmd.AddCommand(inspectCmd())
	rootCmd.AddCommand(clusterCmd(cfg))
	rootCmd.AddCommand(nodeCmd(cfg))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
