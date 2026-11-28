package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	v202611 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202611"
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
			disableGvisor, _ := cmd.Flags().GetBool("disable-gvisor")
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
				Token:         cfg.Token,
				WaitMessage:   "Waiting for node registration",
				Verbose:       verbose,
				DeviceMode:    deviceMode,
				InstallGvisor: !disableGvisor,
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
	cmd.Flags().Bool("disable-gvisor", false, "Do not install gVisor (runsc) or register the runsc containerd runtime handler")
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
				var nodeInfo v202611.Node
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
				var nodes []v202611.Node
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
	cmd.Flags().StringArray("label", nil, "Node label filter (key=value; can be repeated)")
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

			kubehubcli.PromptConfirm("Are you sure you want to delete node %q from cluster %q?", node, cluster)

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
	cmd.Flags().StringArray("label", nil, "Node label (key=value to ensure, -key to remove; can be repeated)")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("node")
	return cmd
}

// Host-side seams so tests can exercise reconcile without touching the host.
var (
	installGvisorOnHost     = kubehubcli.InstallGvisor
	detectHostOnNode        = kubehubcli.DetectHost
	ensureRuncOnNode        = kubehubcli.EnsureRunc
	refreshContainerdOnNode = kubehubcli.RefreshContainerd
	refreshCrictlOnNode     = kubehubcli.RefreshCrictl
	refreshConfigOnNode     = kubehubcli.RefreshNodeConfiguration
)

func nodeReconcileCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile node software and state",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			node, _ := cmd.Flags().GetString("node")
			disableGvisor, _ := cmd.Flags().GetBool("disable-gvisor")
			installGvisor := !disableGvisor
			labelChanges := getMapFlag(cmd, "label")
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

			var nodeInfo v202611.Node
			if err := json.NewDecoder(resp.Body).Decode(&nodeInfo); err != nil {
				errorExit("decode response: %v", err)
			}

			clusterResp, err := client.GetCluster(ctx, cluster, authHeader)
			if err != nil {
				errorExit("get cluster: %v", err)
			}
			defer clusterResp.Body.Close()
			if clusterResp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(clusterResp.Body)
				errorExit("%s", string(body))
			}
			var clusterInfo v202611.Cluster
			if err := json.NewDecoder(clusterResp.Body).Decode(&clusterInfo); err != nil {
				errorExit("decode cluster response: %v", err)
			}
			kubeletVersion := kubehubcli.ResolveClusterComponentVersion(&clusterInfo)
			if kubeletVersion == "" {
				errorExit("cluster API server version is unavailable")
			}

			info, err := detectHostOnNode()
			if err != nil {
				errorExit("detect host: %v", err)
			}
			plan := kubehubcli.NewReconcilePlan(info, installGvisor, kubeletVersion)
			if err := kubehubcli.ConfirmReconcilePlan(plan); err != nil {
				errorExit("%v", err)
			}

			if err := ensureRuncOnNode(info); err != nil {
				errorExit("ensure runc: %v", err)
			}
			if err := refreshCrictlOnNode(info); err != nil {
				errorExit("refresh crictl: %v", err)
			}
			if err := refreshContainerdOnNode(info); err != nil {
				errorExit("refresh containerd: %v", err)
			}
			if installGvisor {
				if err := installGvisorOnHost(); err != nil {
					errorExit("%v", err)
				}
			}

			var existingLabels map[string]string
			if nodeInfo.Spec != nil && nodeInfo.Spec.Meta != nil && nodeInfo.Spec.Meta.Labels != nil {
				existingLabels = *nodeInfo.Spec.Meta.Labels
			}
			labels := kubehubcli.MergeNodeLabels(existingLabels, labelChanges, installGvisor)
			if err := refreshConfigOnNode(ctx, client, authHeader, cluster, node, labels); err != nil {
				errorExit("refresh config: %v", err)
			}
			var nodeMeta *v202611.NodeInfo
			if nodeInfo.Spec != nil && nodeInfo.Spec.Meta != nil {
				nodeMeta = &v202611.NodeInfo{Ipv4: nodeInfo.Spec.Meta.Ipv4, Labels: &labels}
			} else {
				nodeMeta = &v202611.NodeInfo{Labels: &labels}
			}
			nodeRequest := v202611.Node{
				Metadata: &v202611.NodeMetadata{
					Name: nodeInfo.Metadata.Name,
				},
				Spec: &v202611.NodeSpec{
					Os:   nodeInfo.Spec.Os,
					Arch: nodeInfo.Spec.Arch,
					Meta: nodeMeta,
				},
			}

			var nodeETag string
			if nodeInfo.Metadata != nil && nodeInfo.Metadata.Etag != nil {
				nodeETag = *nodeInfo.Metadata.Etag
			}

			putResp, err := client.UpdateNode(ctx, cluster, node, nodeRequest, authHeader, func(ctx context.Context, req *http.Request) error {
				if nodeETag != "" {
					req.Header.Set("If-Match", nodeETag)
				}
				return nil
			})
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
	cmd.Flags().Bool("disable-gvisor", false, "Do not install gVisor (runsc) on this host or register the runsc containerd runtime handler (day-2)")
	cmd.Flags().StringArray("label", nil, "Node label (key=value to ensure, -key to remove; can be repeated)")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("node")
	return cmd
}
