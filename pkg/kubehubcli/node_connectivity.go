package kubehubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
)

const kubeletPort = 10250

// probeKubelet checks that the kubelet API on nodeIP is reachable using the
// bootstrap client certificate issued for this node. It is a package variable
// so tests can exercise the join checks without touching the network.
var probeKubelet = func(ip string) error {
	target := fmt.Sprintf("https://%s/healthz", net.JoinHostPort(ip, strconv.Itoa(kubeletPort)))
	return RunCmdCapture(
		"curl",
		"-sS",
		"-k",
		"--connect-timeout", "5",
		"--max-time", "10",
		"--cert", filepath.Join(KubeletPKIPath, "kubelet.crt"),
		"--key", filepath.Join(KubeletPKIPath, "kubelet.key"),
		target,
	)
}

// checkNodeIPConflicts rejects joins that would give two nodes the same IP.
func checkNodeIPConflicts(nodes []v202607.Node, newIP string) error {
	if parsed := net.ParseIP(newIP); parsed != nil {
		newIP = parsed.String()
	}
	seen := make(map[string]string, len(nodes))
	for _, node := range nodes {
		ip := nodeIPv4(node)
		if ip == "" {
			continue
		}
		name := nodeName(node)
		if other, ok := seen[ip]; ok {
			return fmt.Errorf("nodes %s and %s share the same IP %s; each node must have a unique IP", other, name, ip)
		}
		seen[ip] = name
		if newIP != "" && ip == newIP {
			return fmt.Errorf("node %s already uses IP %s; each node must have a unique IP", name, ip)
		}
	}
	return nil
}

// checkExistingNodesReachable verifies that this node can reach the kubelet on
// every node that is already part of the cluster. Existing nodes are filtered
// by the same hostname that join registers, so the newly registered node does
// not probe itself.
func checkExistingNodesReachable(ctx context.Context, client *v202607.Client, authHeader v202607.RequestEditorFn, clusterName, localName string) error {
	listResp, err := client.ListNodes(ctx, clusterName, authHeader)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		return fmt.Errorf("list nodes: %s", v202607.ParseErrorResponse(listResp))
	}

	var nodes []v202607.Node
	if err := json.NewDecoder(listResp.Body).Decode(&nodes); err != nil {
		return fmt.Errorf("decode nodes: %w", err)
	}

	var existing []v202607.Node
	for _, node := range nodes {
		if nodeName(node) == localName {
			continue
		}
		existing = append(existing, node)
	}
	if len(existing) == 0 {
		return nil
	}

	slog.Info("=== Checking Existing Node Connectivity ===")
	for _, node := range existing {
		ip := nodeIPv4(node)
		name := nodeName(node)
		if ip == "" {
			slog.Warn(fmt.Sprintf("  [SKIP] node %s has no IP address", name))
			continue
		}
		if err := probeKubelet(ip); err != nil {
			return fmt.Errorf("cannot reach existing node %s (%s) on port %d: %w", name, ip, kubeletPort, err)
		}
		slog.Info(fmt.Sprintf("  [OK] node %s (%s) reachable on port %d", name, ip, kubeletPort))
	}
	return nil
}

func nodeName(node v202607.Node) string {
	if node.Metadata == nil || node.Metadata.Name == nil {
		return "<unknown>"
	}
	return *node.Metadata.Name
}

func nodeIPv4(node v202607.Node) string {
	if node.Spec == nil || node.Spec.Meta == nil || node.Spec.Meta.Ipv4 == nil {
		return ""
	}
	ip := net.ParseIP(*node.Spec.Meta.Ipv4)
	if ip == nil {
		return ""
	}
	return ip.String()
}
