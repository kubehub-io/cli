package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func clusterCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster operations",
	}
	cmd.AddCommand(clusterInfoCmd(cfg))
	cmd.AddCommand(clusterReconcileCmd(cfg))
	return cmd
}

func clusterInfoCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show cluster information",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
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

			resp, err := client.GetCluster(ctx, cluster, authHeader)
			if err != nil {
				errorExit("get cluster: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errorExit("%s", string(body))
			}

			var clusterInfo v202607.Cluster
			if err := json.NewDecoder(resp.Body).Decode(&clusterInfo); err != nil {
				errorExit("decode response: %v", err)
			}

			slog.Info("=== Cluster Information ===")
			if clusterInfo.Metadata.Name != nil {
				slog.Info(fmt.Sprintf("Name: %s", *clusterInfo.Metadata.Name))
			}

			if clusterInfo.Spec.Region != nil {
				slog.Info(fmt.Sprintf("Region: %s", *clusterInfo.Spec.Region))
			}
			if clusterInfo.Status != nil && clusterInfo.Status.PublicDns != nil {
				slog.Info(fmt.Sprintf("Public DNS: %s", *clusterInfo.Status.PublicDns))
			}

			if clusterInfo.Spec != nil && clusterInfo.Spec.Network != nil {
				slog.Info("=== Network ===")
				if clusterInfo.Spec.Network.PodCIDR != nil {
					slog.Info(fmt.Sprintf("Pod CIDR: %s", *clusterInfo.Spec.Network.PodCIDR))
				}
				if clusterInfo.Spec.Network.ServiceCIDR != nil {
					slog.Info(fmt.Sprintf("Service CIDR: %s", *clusterInfo.Spec.Network.ServiceCIDR))
				}
			}
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}

func clusterReconcileCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile cluster state",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
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

			resp, err := client.GetCluster(ctx, cluster, authHeader)
			if err != nil {
				errorExit("get cluster: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errorExit("%s", string(body))
			}

			var clusterInfo v202607.Cluster
			if err := json.NewDecoder(resp.Body).Decode(&clusterInfo); err != nil {
				errorExit("decode response: %v", err)
			}

			clusterRequest := v202607.ClusterRequest{
				Metadata: clusterInfo.Metadata,
				Spec:     clusterInfo.Spec,
			}

			var clusterETag string
			if clusterInfo.Metadata != nil && clusterInfo.Metadata.Etag != nil {
				clusterETag = *clusterInfo.Metadata.Etag
			}

			putResp, err := client.UpdateCluster(ctx, cluster, clusterRequest, authHeader, func(ctx context.Context, req *http.Request) error {
				if clusterETag != "" {
					req.Header.Set("If-Match", clusterETag)
				}
				return nil
			})
			if err != nil {
				errorExit("put cluster: %v", err)
			}
			defer putResp.Body.Close()

			if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(putResp.Body)
				errorExit("%s", string(body))
			}

			slog.Info(fmt.Sprintf("Cluster %s reconciled successfully", cluster))
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.MarkFlagRequired("cluster")
	return cmd
}
