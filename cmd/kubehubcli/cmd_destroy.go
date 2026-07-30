package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func destroyCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Reset this node and delete the cluster",
		Long: `Reset this node (removing all Kubernetes components) and delete the cluster from the API.

This is a destructive operation that will:
  - Remove all Kubernetes data and binaries from this host
  - Delete the node from the cluster
  - Delete the cluster itself
  - All workloads running on this node will be terminated.`,
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			if cluster == "" {
				errorExit("--cluster is required")
			}

			ctx := context.Background()

			client, token, err := getAuthenticatedClient(ctx, cfg)
			if err != nil {
				errorExit("%v", err)
			}

			authHeader := withBearerToken(token)

			getResp, err := client.GetCluster(ctx, cluster, authHeader)
			if err != nil {
				errorExit("get cluster: %v", err)
			}

			var clusterInfo v202607.Cluster
			if getResp.StatusCode == http.StatusOK {
				if err := json.NewDecoder(getResp.Body).Decode(&clusterInfo); err != nil {
					getResp.Body.Close()
					errorExit("decode cluster: %v", err)
				}
				getResp.Body.Close()
			} else {
				getResp.Body.Close()
				errorExit("cluster %s not found", cluster)
			}

			listResp, err := client.ListNodes(ctx, cluster, authHeader)
			if err != nil {
				errorExit("list nodes: %v", err)
			}

			var nodes []v202607.Node
			if listResp.StatusCode == http.StatusOK {
				if err := json.NewDecoder(listResp.Body).Decode(&nodes); err != nil {
					listResp.Body.Close()
					errorExit("decode nodes: %v", err)
				}
				listResp.Body.Close()
			} else {
				nonOKStatus := listResp.StatusCode
				errMsg := v202607.ParseErrorResponse(listResp)
				listResp.Body.Close()
				errorExit("list nodes failed (status %d): %s", nonOKStatus, errMsg)
			}

			hostname, _ := os.Hostname()

			var currentFound bool
			var otherNodeNames []string
			for _, n := range nodes {
				name := ""
				if n.Metadata != nil && n.Metadata.Name != nil {
					name = *n.Metadata.Name
				}
				if name == hostname {
					currentFound = true
				} else {
					otherNodeNames = append(otherNodeNames, name)
				}
			}

			if !currentFound {
				otherStr := ""
				for i, n := range otherNodeNames {
					if i > 0 {
						otherStr += ","
					}
					otherStr += n
				}
				errorExit("delete node %s from cluster before destroy cluster.", otherStr)
			}

			if len(otherNodeNames) > 0 {
				otherStr := ""
				for i, n := range otherNodeNames {
					if i > 0 {
						otherStr += ","
					}
					otherStr += n
				}
				errorExit("cannot delete cluster while other nodes (%s) exists, you need reset those nodes first.", otherStr)
			}

			resetOpts := &kubehubcli.ResetOptions{
				ClusterName:   cluster,
				ServerURL:     cfg.Server,
				OIDCIssuerURL: cfg.Issuer,
				OIDCClientID:  cfg.ClientID,
				Verbose:       verbose,
			}
			if err := kubehubcli.ResetNode(resetOpts); err != nil {
				errorExit("reset node: %v", err)
			}

			var clusterETag string
			if clusterInfo.Metadata != nil && clusterInfo.Metadata.Etag != nil {
				clusterETag = *clusterInfo.Metadata.Etag
			}

			slog.Info(fmt.Sprintf("--- Deleting cluster %s ---", cluster))
			deleteResp, err := client.DeleteCluster(ctx, cluster, authHeader, func(ctx context.Context, req *http.Request) error {
				if clusterETag != "" {
					req.Header.Set("If-Match", clusterETag)
				}
				return nil
			})
			if err != nil {
				errorExit("delete cluster request: %v", err)
			}
			deleteResp.Body.Close()
			if deleteResp.StatusCode == http.StatusOK || deleteResp.StatusCode == http.StatusNoContent {
				slog.Info("  Cluster deleted")
			} else {
				errorExit("delete cluster: %s", v202607.ParseErrorResponse(deleteResp))
			}

			slog.Info("Cluster destroyed.")
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}
