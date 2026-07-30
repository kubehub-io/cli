package main

import (
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func resetCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset this node, removing all Kubernetes components (kubeadm reset equivalent)",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			if cluster == "" {
				cmd.Help()
				return
			}
			opts := &kubehubcli.ResetOptions{
				ClusterName:   cluster,
				ServerURL:     cfg.Server,
				OIDCIssuerURL: cfg.Issuer,
				OIDCClientID:  cfg.ClientID,
				Verbose:       verbose,
			}
			if err := kubehubcli.ResetNode(opts); err != nil {
				errorExit("%v", err)
			}
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}
