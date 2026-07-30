package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func setupCmd(cfg *kubehubcli.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create a cluster and join this node to it",
		Run: func(cmd *cobra.Command, args []string) {
			cluster, _ := cmd.Flags().GetString("cluster")
			region, _ := cmd.Flags().GetString("region")
			nodeIP, _ := cmd.Flags().GetString("node-ip")
			if cluster == "" || region == "" {
				cmd.Help()
				os.Exit(1)
			}

			info, err := kubehubcli.DetectHost()
			if err != nil {
				errorExit("detect host: %v", err)
			}

			if info.Kubelet != nil && info.Kubelet.Bootstrap == "yes" {
				errorExit("host already joined to a cluster. Run 'kubehubcli reset' first")
			}

			ctx := context.Background()

			client, token, err := getAuthenticatedClient(ctx, cfg)
			if err != nil {
				errorExit("%v", err)
			}

			authHeader := withBearerToken(token)

			slog.Info("=== Creating Cluster ===")
			slog.Info(fmt.Sprintf("Name: %s", cluster))
			slog.Info(fmt.Sprintf("Region: %s", region))

			clusterReq := v202607.ClusterRequest{
				Metadata: &v202607.ClusterMetadata{Name: &cluster},
				Spec:     &v202607.ClusterSpec{Region: &region},
			}

			createResp, err := client.CreateCluster(ctx, clusterReq, authHeader)
			if err != nil {
				errorExit("create cluster: %v", err)
			}

			if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
				errMsg := v202607.ParseErrorResponse(createResp)
				createResp.Body.Close()
				errorExit("create cluster: %s", errMsg)
			}
			createResp.Body.Close()

			slog.Info("Cluster created successfully!")

			slog.Info("Waiting for cluster to be ready...")
			var clusterInfo v202607.Cluster
			clusterReady := false
			deadline := time.Now().Add(10 * time.Minute)
			for time.Now().Before(deadline) {
				getResp, err := client.GetCluster(ctx, cluster, authHeader)
				if err != nil {
					slog.Warn(fmt.Sprintf("get cluster: %v", err))
					time.Sleep(15 * time.Second)
					continue
				}

				if getResp.StatusCode == http.StatusOK {
					if err := json.NewDecoder(getResp.Body).Decode(&clusterInfo); err == nil {
						if clusterInfo.Status != nil && clusterInfo.Status.PublicDns != nil && *clusterInfo.Status.PublicDns != "" {
							getResp.Body.Close()
							slog.Info(fmt.Sprintf("PublicDNS: %s", *clusterInfo.Status.PublicDns))
							clusterReady = true
							break
						}
					}
				}
				getResp.Body.Close()

				slog.Info("Cluster not ready yet, waiting 15 seconds...")
				time.Sleep(15 * time.Second)
			}

			if !clusterReady {
				errorExit("timed out waiting for cluster to become ready")
			}

			hostCIDRs, err := kubehubcli.DetectHostCIDRs()
			if err != nil {
				slog.Warn(fmt.Sprintf("detect host CIDRs: %v", err))
			} else {
				defaultPodCIDR := "10.10.0.0/16"
				defaultSvcCIDR := "10.20.0.0/16"
				needsUpdate := kubehubcli.HostCIDRConflicts(hostCIDRs, defaultPodCIDR) || kubehubcli.HostCIDRConflicts(hostCIDRs, defaultSvcCIDR)

				if needsUpdate {
					podCIDR, svcCIDR, dnsIP := kubehubcli.PickNonConflictingCIDRs(hostCIDRs)
					if podCIDR != "" {
						slog.Info("Host network CIDR conflicts with default cluster network, updating cluster network...")
						slog.Info(fmt.Sprintf("  PodCIDR: %s -> %s", defaultPodCIDR, podCIDR))
						slog.Info(fmt.Sprintf("  ServiceCIDR: %s -> %s", defaultSvcCIDR, svcCIDR))

						updateReq := v202607.ClusterRequest{
							Spec: &v202607.ClusterSpec{
								Network: &v202607.Network{
									PodCIDR:          &podCIDR,
									ServiceCIDR:      &svcCIDR,
									KubeDNSServiceIP: &dnsIP,
								},
							},
						}
						updateResp, err := client.UpdateCluster(ctx, cluster, updateReq, authHeader)
						if err != nil {
							errorExit("update cluster network: %v", err)
						}
						if updateResp.StatusCode != http.StatusOK && updateResp.StatusCode != http.StatusNoContent {
							errMsg := v202607.ParseErrorResponse(updateResp)
							updateResp.Body.Close()
							errorExit("update cluster network: %s", errMsg)
						}
						updateResp.Body.Close()
						slog.Info("Cluster network updated successfully!")
					}
				}
			}

			opts := &kubehubcli.JoinOptions{
				ClusterName:   cluster,
				NodeIP:        nodeIP,
				ServerURL:     cfg.Server,
				OIDCIssuerURL: cfg.Issuer,
				OIDCClientID:  cfg.ClientID,
				WaitMessage:   "Waiting for cluster ready",
				Verbose:       verbose,
				Labels:        getMapFlag(cmd, "label"),
				Annotations:   getMapFlag(cmd, "annotation"),
			}

			if err := kubehubcli.JoinClusterWithCluster(opts, &clusterInfo); err != nil {
				errorExit("%v", err)
			}
		},
	}
	cmd.Flags().String("cluster", "", "Cluster name (required)")
	cmd.Flags().String("region", "", "Cluster region (required)")
	cmd.Flags().String("node-ip", "", "Node IP address (auto-detected if not set)")
	cmd.Flags().StringArray("label", nil, "Node label (key=value, can be specified multiple times)")
	cmd.Flags().StringArray("annotation", nil, "Node annotation (key=value, can be specified multiple times)")
	cmd.MarkFlagRequired("cluster")
	cmd.MarkFlagRequired("region")
	return cmd
}
