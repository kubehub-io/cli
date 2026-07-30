package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func nodeCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Node operations",
	}
	cmd.AddCommand(nodeJoinCmd(cfg))
	cmd.AddCommand(nodeInfoCmd(cfg))
	cmd.AddCommand(nodeReconcileCmd(cfg))
	cmd.AddCommand(nodeDeleteCmd(cfg))
	return cmd
}

func nodeJoinCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join",
		Short: "Join this host to a cluster",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			nodeIP, _ := cmd.Flags().GetString("node-ip")
			if cluster == "" {
				cmd.Help()
				os.Exit(1)
			}

			opts := &kubehubcli.JoinOptions{
				ClusterName:   cluster,
				NodeIP:        nodeIP,
				ServerURL:     cfg.Server,
				OIDCIssuerURL: cfg.Issuer,
				OIDCClientID:  cfg.ClientID,
				WaitMessage:   "Waiting for node registration",
				Verbose:       verbose,
				Labels:        getMapFlag(cmd, "label"),
				Annotations:   getMapFlag(cmd, "annotation"),
			}

			if err := kubehubcli.JoinCluster(opts); err != nil {
				errorExit("%v", err)
			}
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.Flags().String("node-ip", "", "Node IP address (auto-detected if not set)")
	cmd.Flags().StringArray("label", nil, "Node label (key=value, can be specified multiple times)")
	cmd.Flags().StringArray("annotation", nil, "Node annotation (key=value, can be specified multiple times)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}

func nodeInfoCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show node information",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			node, _ := cmd.Flags().GetString("node")
			if cluster == "" {
				cmd.Help()
				os.Exit(1)
			}

			ctx := context.Background()

			client, token, err := getAuthenticatedClient(ctx, cfg)
			if err != nil {
				errorExit("%v", err)
			}

			authHeader := withBearerToken(token)

			var resp *http.Response
			if node != "" {
				resp, err = client.GetNode(ctx, cluster, node, authHeader)
			} else {
				resp, err = client.ListNodes(ctx, cluster, authHeader)
			}
			if err != nil {
				errorExit("get node: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errorExit("%s", string(body))
			}

			if node != "" {
				var nodeInfo v202607.Node
				if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
					errorExit("decode response: %v", err)
				}
				slog.Info("=== Node Information ===")
				if nodeInfo.Metadata != nil {
					slog.Info(fmt.Sprintf("Name: %s", *nodeInfo.Metadata.Name))
				}
				if nodeInfo.Spec != nil {
					if nodeInfo.Spec.Os != nil {
						slog.Info(fmt.Sprintf("OS: %s", *nodeInfo.Spec.Os))
					}
					if nodeInfo.Spec.Arch != nil {
						slog.Info(fmt.Sprintf("Arch: %s", *nodeInfo.Spec.Arch))
					}
					if nodeInfo.Spec.Meta != nil {
						slog.Info(fmt.Sprintf("IP: %s", *nodeInfo.Spec.Meta.Ipv4))
					}
				}
			} else {
				var nodes []v202607.Node
				if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
					errorExit("decode response: %v", err)
				}
				slog.Info("=== Nodes ===")
				for _, n := range nodes {
					name := ""
					os := ""
					arch := ""
					if n.Metadata != nil && n.Metadata.Name != nil {
						name = *n.Metadata.Name
					}
					if n.Spec != nil {
						if n.Spec.Os != nil {
							os = *n.Spec.Os
						}
						if n.Spec.Arch != nil {
							arch = *n.Spec.Arch
						}
					}
					slog.Info(fmt.Sprintf("- %s (OS: %s, Arch: %s)", name, os, arch))
				}
			}
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.Flags().String("node", "", "Node name (optional, lists all if not specified)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}

func nodeDeleteCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a node from the cluster",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			node, _ := cmd.Flags().GetString("node")
			if cluster == "" || node == "" {
				cmd.Help()
				os.Exit(1)
			}

			fmt.Printf("Are you sure you want to delete node %q from cluster %q? [y/N]: ", node, cluster)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(answer)
			if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
				slog.Info("Deletion cancelled")
				return
			}

			ctx := context.Background()

			client, token, err := getAuthenticatedClient(ctx, cfg)
			if err != nil {
				errorExit("%v", err)
			}

			authHeader := withBearerToken(token)

			resp, err := client.DeleteNode(ctx, cluster, node, authHeader)
			if err != nil {
				errorExit("delete node: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(resp.Body)
				errorExit("%s", string(body))
			}

			slog.Info(fmt.Sprintf("Node %q deleted from cluster %q", node, cluster))
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.Flags().String("node", "", "Node name (required)")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("node")
	return cmd
}

func nodeReconcileCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile node state",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			node, _ := cmd.Flags().GetString("node")
			if cluster == "" || node == "" {
				cmd.Help()
				os.Exit(1)
			}

			ctx := context.Background()

			client, token, err := getAuthenticatedClient(ctx, cfg)
			if err != nil {
				errorExit("%v", err)
			}

			authHeader := withBearerToken(token)

			resp, err := client.GetNode(ctx, cluster, node, authHeader)
			if err != nil {
				errorExit("get node: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errorExit("%s", string(body))
			}

			var nodeInfo v202607.Node
			if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
				errorExit("decode response: %v", err)
			}

			nodeRequest := v202607.Node{
				Metadata: &v202607.NodeMetadata{
					Name: nodeInfo.Metadata.Name,
				},
				Spec: &v202607.NodeSpec{
					Os:   nodeInfo.Spec.Os,
					Arch: nodeInfo.Spec.Arch,
				},
			}

			putResp, err := client.UpdateNode(ctx, cluster, node, nodeRequest, authHeader)
			if err != nil {
				errorExit("put node: %v", err)
			}
			defer putResp.Body.Close()

			if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(putResp.Body)
				errorExit("%s", string(body))
			}

			slog.Info(fmt.Sprintf("Node %s reconciled successfully", node))
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.Flags().String("node", "", "Node name (required)")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("node")
	return cmd
}
