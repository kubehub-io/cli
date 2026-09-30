package main

import (
	"os"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

var verbose bool
var autoYes bool
var tokenValue string
var clientSecret string
var grantType string

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
			cfg.Token = tokenValue
			cfg.ClientSecret = clientSecret
			cfg.GrantType = grantType
		},
	}
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&autoYes, "yes", "y", false, "skip all confirmation prompts")
	rootCmd.PersistentFlags().StringVar(&tokenValue, "token", "", "Pass a bearer token directly (skips OAuth2 authentication)")
	rootCmd.PersistentFlags().StringVar(&clientSecret, "oidc-client-secret", "", "OIDC client secret (for client-credentials grant)")
	rootCmd.PersistentFlags().StringVar(&grantType, "grant-type", "", "OAuth2 grant type: auto, device-code, client-credential")
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
