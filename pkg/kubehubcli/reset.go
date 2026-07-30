package kubehubcli

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
)

type ResetOptions struct {
	ClusterName   string
	ServerURL     string
	OIDCIssuerURL string
	OIDCClientID  string
	Verbose       bool
}

func ResetNode(opts *ResetOptions) error {
	slog.Warn("[WARNING] This command will remove Kubernetes components from this host.")
	slog.Warn("This includes:")
	slog.Warn("  - All Kubernetes data (PKI, certificates, kubelet config)")
	slog.Warn("  - Static pod manifests (controller-manager, scheduler)")
	slog.Warn("  - The kubelet, kubectl, crictl binaries and their configuration")
	slog.Warn("  - Sysctl kernel parameter tweaks")
	slog.Warn("  - The containerd service will be disabled")
	slog.Warn("")
	slog.Warn("IMPORTANT: Any application workloads (pods) running on this node")
	slog.Warn("will be TERMINATED. Please back up any critical data first.")
	slog.Warn("")

	info, err := DetectHost()
	if err != nil {
		slog.Warn(fmt.Sprintf("warning: failed to detect host state: %v", err))
	} else {
		slog.Info("=== Current Host State ===")
		if info.Kubelet != nil {
			slog.Info(fmt.Sprintf("  Kubelet:     %s (%s)", info.Kubelet.Version, info.Kubelet.State))
			if info.Kubelet.Bootstrap == "yes" {
				slog.Info("  Node status: joined to a cluster")
			} else {
				slog.Info("  Node status: not joined")
			}
		} else {
			slog.Info("  Kubelet:     not installed")
		}
		if info.Containerd != nil {
			slog.Info(fmt.Sprintf("  Containerd:  %s (%s)", info.Containerd.Version, info.Containerd.State))
		} else {
			slog.Info("  Containerd:  not installed")
		}
		slog.Info("")
	}

	if info != nil && info.Kubelet != nil {
		slog.Warn("WARNING: This node may be running workloads for one or more clusters.")
		slog.Warn("Resetting this node will cause service disruption.")
		slog.Warn("If you need to preserve application data, back it up before proceeding.")
		slog.Warn("")
	}

	fmt.Fprint(os.Stdout, "Are you sure you want to proceed? (type 'yes' to confirm): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		slog.Info("Aborted.")
		return nil
	}
	input := strings.TrimSpace(scanner.Text())
	if !strings.EqualFold(input, "yes") {
		slog.Info("Aborted.")
		return nil
	}
	slog.Info("")

	hostname, _ := os.Hostname()
	if hostname != "" {
		slog.Info(fmt.Sprintf("--- Deleting node %s from cluster %s ---", hostname, opts.ClusterName))

		ctx := context.Background()
		auth := NewAuthenticator(opts.OIDCIssuerURL, opts.OIDCClientID).WithVerbose(opts.Verbose)
		token, err := auth.Authenticate(ctx)
		if err != nil {
			slog.Warn(fmt.Sprintf("  warning: authentication failed: %v", err))
		} else {
			client, err := v202607.NewClient(opts.ServerURL)
			if err != nil {
				slog.Warn(fmt.Sprintf("  warning: create client: %v", err))
			} else {
				authHeader := func(ctx context.Context, req *http.Request) error {
					req.Header.Set("Authorization", "Bearer "+token)
					return nil
				}
				resp, err := client.DeleteNode(ctx, opts.ClusterName, hostname, authHeader)
				if err != nil {
					slog.Warn(fmt.Sprintf("  warning: delete node request: %v", err))
				} else {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
						slog.Info("  Node deleted from cluster")
					} else {
						slog.Warn(fmt.Sprintf("  warning: delete node returned %s", http.StatusText(resp.StatusCode)))
					}
				}
			}
		}
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"stop kubelet", func() error {
			RunCmd("systemctl", "stop", "kubelet")
			return nil
		}},
		{"stop all containers", func() error {
			RunCmdCapture("crictl", "rm", "-af")
			return nil
		}},
		{"remove crictl binary", func() error {
			return RunCmd("rm", "-f", "/usr/local/bin/crictl")
		}},
		{"remove crictl config", func() error {
			return RunCmd("rm", "-f", "/etc/crictl.yaml")
		}},
		{"disable kubelet", func() error {
			RunCmd("systemctl", "disable", "kubelet")
			return nil
		}},
		{"remove kubelet systemd unit", func() error {
			return RunCmd("rm", "-f", "/etc/systemd/system/kubelet.service")
		}},
		{"remove kubelet binary", func() error {
			return RunCmd("rm", "-f", "/usr/local/bin/kubelet")
		}},
		{"remove kubectl binary", func() error {
			return RunCmd("rm", "-f", "/usr/local/bin/kubectl")
		}},
		{"remove kubernetes data", func() error {
			if err := RunCmd("rm", "-rf", "/var/lib/kubelet/"); err != nil {
				return err
			}
			return RunCmd("rm", "-rf", "/etc/kubernetes/")
		}},
		{"remove sysctl config", func() error {
			return RunCmd("rm", "-f", "/etc/sysctl.d/98-kubernetes.conf")
		}},
		{"reload sysctl", func() error {
			return RunCmd("sysctl", "--system")
		}},
		{"reload systemd", func() error {
			return RunCmd("systemctl", "daemon-reload")
		}},
		{"disable containerd", func() error {
			RunCmd("systemctl", "disable", "containerd")
			return nil
		}},
	}

	for _, s := range steps {
		if err := s.run(); err != nil {
			slog.Error(fmt.Sprintf("  %-30s FAILED", s.name), "error", err)
		} else {
			slog.Info(fmt.Sprintf("  %-30s OK", s.name))
		}
	}

	slog.Info("Node reset complete.")
	return nil
}
