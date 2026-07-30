package main

import (
	"fmt"
	"log/slog"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func inspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect host information",
		Run: func(cmd *cobra.Command, args []string) {
			info, err := kubehubcli.DetectHost()
			if err != nil {
				errorExit("%v", err)
			}

			slog.Info("=== Host Information ===")
			slog.Info(fmt.Sprintf("Host IPs: %v", info.HostIPs))
			slog.Info(fmt.Sprintf("OS: %s", info.OS))
			if info.Distro != "" {
				slog.Info(fmt.Sprintf("Distro: %s", info.Distro))
			}
			slog.Info(fmt.Sprintf("Arch: %s", info.Arch))
			slog.Info(fmt.Sprintf("Kernel: %s", info.Kernel))

			slog.Info("=== Container Runtime ===")
			if info.Containerd != nil {
				slog.Info(fmt.Sprintf("Containerd: %s (%s)", info.Containerd.Version, info.Containerd.State))
			} else {
				slog.Info("Containerd: not installed")
			}

			if info.Crictl != nil {
				slog.Info(fmt.Sprintf("Crictl: %s", info.Crictl.Version))
			} else {
				slog.Info(fmt.Sprintf("Crictl: not installed (will install %s)", kubehubcli.CrictlVersion))
			}

			if info.Runc != nil {
				slog.Info(fmt.Sprintf("Runc: %s", info.Runc.Version))
			} else {
				slog.Info("Runc: not installed (usually comes with containerd)")
			}

			slog.Info("=== Kubernetes CLI ===")
			if info.Kubectl != nil {
				slog.Info(fmt.Sprintf("Kubectl: %s", info.Kubectl.Version))
			} else {
				slog.Info("Kubectl: not installed (will install)")
			}

			slog.Info("=== Kubelet ===")
			if info.Kubelet != nil {
				slog.Info(fmt.Sprintf("Kubelet: %s (state: %s)", info.Kubelet.Version, info.Kubelet.State))
				if info.Kubelet.Bootstrap == "yes" {
					slog.Warn("This host appears to already be joined to a cluster!")
					slog.Warn("  - Found existing kubelet config or PKI at /var/lib/kubelet/")
					slog.Warn("  - Joining may cause conflicts or break existing cluster membership")
					slog.Warn("  - If you want to re-join, first clean up: systemctl stop kubelet && rm -rf /var/lib/kubelet/pki /etc/kubernetes")
				}
			} else {
				slog.Info("Kubelet: not installed (will install)")
			}
		},
	}
}
