package main

import (
	"os"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

var verbose bool

func main() {
	cfg, err := kubehubcli.LoadConfig()
	if err != nil {
		errorExit("loading config: %v", err)
	}

	rootCmd := &cobra.Command{
		Use:   "kubehubcli",
		Short: "Nest control utility for host operations",
	}
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
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
