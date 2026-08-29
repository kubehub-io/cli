package main

import (
	"log/slog"
	"os"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

var verbose bool

func main() {
	setupLogging()

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

// setupLogging configures the default slog logger. Output uses a readable
// timestamp and human-friendly level so errors are easy to scan.
func setupLogging() {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
	})
	slog.SetDefault(slog.New(handler))
}
