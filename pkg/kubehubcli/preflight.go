package kubehubcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
)

type PreflightResult struct {
	StaticIPConfigured bool
	MACFixed           bool
	ClusterUpdated     bool
}

func RunPreflightChecks(ctx context.Context, client *v202607.Client, authHeader v202607.RequestEditorFn, opts *JoinOptions, clusterInfo *v202607.Cluster, info *HostInfo) (*PreflightResult, error) {
	result := &PreflightResult{}

	osConfig := DetectOSConfigurator(info.Distro)

	iface, _ := defaultRouteInterface()
	if iface == "" && len(info.HostIPs) > 0 {
		iface = findInterfaceForIP(info.HostIPs[0])
	}

	slog.Info("=== Pre-flight Checks ===")

	if iface != "" {
		if err := checkRandomMAC(iface, osConfig, result); err != nil {
			slog.Warn(fmt.Sprintf("  warning: MAC check: %v", err))
		}
	} else {
		slog.Info("  [SKIP] MAC check: could not determine primary interface")
	}

	if iface != "" {
		if err := checkStaticIP(iface, info, osConfig, result); err != nil {
			slog.Warn(fmt.Sprintf("  warning: static IP check: %v", err))
		}
	} else if len(info.HostIPs) > 0 {
		if err := checkStaticIP("", info, osConfig, result); err != nil {
			slog.Warn(fmt.Sprintf("  warning: static IP check: %v", err))
		}
	} else {
		slog.Info("  [SKIP] static IP: no IP found")
	}

	if clusterInfo != nil {
		if err := checkCIDRConflict(ctx, client, authHeader, opts, clusterInfo, info, result); err != nil {
			slog.Warn(fmt.Sprintf("  warning: CIDR conflict check: %v", err))
		}
	}

	return result, nil
}

func findInterfaceForIP(ipStr string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	target := net.ParseIP(ipStr)
	if target == nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.Equal(target) {
				return iface.Name
			}
		}
	}
	return ""
}

func checkRandomMAC(iface string, osConfig OSConfigurator, result *PreflightResult) error {
	if !hasRandomMAC(iface) {
		slog.Info(fmt.Sprintf("  [OK] MAC address is stable on %s", iface))
		return nil
	}

	slog.Warn(fmt.Sprintf("  [!] Interface %s uses a random MAC address", iface))
	slog.Warn("      Random MACs can change on reboot and break static IP binding.")
	slog.Warn("      Recommendation: disable random MAC for stability.")

	if promptConfirm("      Disable random MAC for %s?", iface) {
		if err := osConfig.DisableRandomMAC(iface); err != nil {
			return fmt.Errorf("disable random MAC: %w", err)
		}
		slog.Info("      Random MAC disabled. Reconnect the interface for changes to take effect.")
		result.MACFixed = true
	} else {
		slog.Info("      Skipped.")
	}
	return nil
}

func checkStaticIP(iface string, info *HostInfo, osConfig OSConfigurator, result *PreflightResult) error {
	ip := info.HostIPs[0]
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("invalid IP: %s", ip)
	}

	mac := ""
	if iface != "" {
		if netIface, err := net.InterfaceByName(iface); err == nil {
			mac = netIface.HardwareAddr.String()
		}
	}

	slog.Warn(fmt.Sprintf("  [!] This node uses DHCP (dynamic IP: %s)", ip))
	slog.Warn("      Static IP is required for Kubernetes nodes.")

	if promptConfirm("      Configure this host to keep IP %s as static?", ip) {
		gw, err := defaultGateway()
		if err != nil {
			gw = ip[:strings.LastIndex(ip, ".")+1] + "1"
			slog.Info(fmt.Sprintf("      (using assumed gateway: %s)", gw))
		}

		dnsServers := readNameservers(DetectResolvConfFile())
		if len(dnsServers) == 0 {
			dnsServers = []string{gw}
		}

		if iface != "" {
			if err := osConfig.ConfigureStaticIP(iface, ip, gw, dnsServers); err != nil {
				return fmt.Errorf("configure static IP: %w", err)
			}
			slog.Info("      Static IP configured. Reboot recommended.")
		} else {
			slog.Info("      No interface found for IP, skipping configuration.")
			slog.Info("      Please configure a static IP manually.")
		}

		if mac != "" {
			slog.Info(fmt.Sprintf("      For a more robust setup, reserve %s for MAC %s on your router.", ip, mac))
		}
		result.StaticIPConfigured = true
	} else {
		slog.Info("      Skipped.")
	}
	return nil
}

func checkCIDRConflict(ctx context.Context, client *v202607.Client, authHeader v202607.RequestEditorFn, opts *JoinOptions, clusterInfo *v202607.Cluster, info *HostInfo, result *PreflightResult) error {
	podCIDR, svcCIDR, _ := resolveClusterCIDRs(clusterInfo)

	hostNets := detectHostNetworks()

	allCIDRs := make([]*net.IPNet, 0, len(hostNets))
	for _, n := range hostNets {
		allCIDRs = append(allCIDRs, n.CIDR)
	}

	conflicts := describeCIDRConflicts(hostNets, podCIDR, svcCIDR)
	if len(conflicts) == 0 {
		return nil
	}

	slog.Warn(fmt.Sprintf("  [!] %s", strings.Join(conflicts, "; ")))

	var nodes []v202607.Node
	if opts != nil {
		listResp, listErr := client.ListNodes(ctx, opts.ClusterName, authHeader)
		if listErr == nil {
			if listResp.StatusCode == 200 {
				if decodeErr := json.NewDecoder(listResp.Body).Decode(&nodes); decodeErr != nil {
				}
			}
			listResp.Body.Close()
		}
	}

	if len(nodes) == 0 {
		newPodCIDR, newSvcCIDR, newDNSIP := PickNonConflictingCIDRs(allCIDRs)
		if newPodCIDR != "" {
			options := []string{
				fmt.Sprintf("Update cluster to PodCIDR=%s, ServiceCIDR=%s", newPodCIDR, newSvcCIDR),
				"Skip (continue with current CIDRs - may cause issues)",
			}
			choice := promptSelect("Choose how to resolve the conflict:", options)
			if choice == 0 {
				updateReq := v202607.ClusterRequest{
					Spec: &v202607.ClusterSpec{
						Network: &v202607.Network{
							PodCIDR:          &newPodCIDR,
							ServiceCIDR:      &newSvcCIDR,
							KubeDNSServiceIP: &newDNSIP,
						},
					},
				}
				updateResp, updateErr := client.UpdateCluster(ctx, opts.ClusterName, updateReq, authHeader)
				if updateErr != nil {
					return fmt.Errorf("update cluster network: %w", updateErr)
				}
				if updateResp.StatusCode != 200 && updateResp.StatusCode != 204 {
					errMsg := v202607.ParseErrorResponse(updateResp)
					updateResp.Body.Close()
					return fmt.Errorf("update cluster network: %s", errMsg)
				}
				updateResp.Body.Close()
				slog.Info(fmt.Sprintf("      Cluster updated: PodCIDR=%s, ServiceCIDR=%s", newPodCIDR, newSvcCIDR))
				result.ClusterUpdated = true
				return nil
			}
			slog.Info("      Skipped cluster update.")
			return nil
		}
		slog.Info("      No non-conflicting CIDRs available in fallback pool.")
	} else {
		slog.Info("      Cluster already has nodes; automatic CIDR update not possible.")
	}

	slog.Warn("      WARNING: Network conflict may cause connectivity issues for pods and services.")
	return nil
}

func describeCIDRConflicts(hostNets []hostNetwork, podCIDR, svcCIDR string) []string {
	sources := []struct {
		label string
		cidr  string
	}{
		{"Pod CIDR", podCIDR},
		{"Service CIDR", svcCIDR},
	}

	var conflicts []string
	for _, src := range sources {
		if net := findConflictingNetwork(hostNets, src.cidr); net != nil {
			conflicts = append(conflicts,
				fmt.Sprintf("%s (%s) conflicts with %s", src.label, src.cidr, net.Describe()))
		}
	}
	return conflicts
}

func findConflictingVirtualInterface(virtualCIDRs []*net.IPNet, podCIDR, svcCIDR string) string {
	_, podNet, _ := net.ParseCIDR(podCIDR)
	_, svcNet, _ := net.ParseCIDR(svcCIDR)
	for _, v := range virtualCIDRs {
		if podNet != nil && cidrOverlaps(v, podNet) {
			return findInterfaceForCIDR(v)
		}
		if svcNet != nil && cidrOverlaps(v, svcNet) {
			return findInterfaceForCIDR(v)
		}
	}
	return ""
}

func findInterfaceForCIDR(target *net.IPNet) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.String() == target.String() {
				return iface.Name
			}
		}
	}
	return ""
}

func checkExistingNodesNetwork(ctx context.Context, client *v202607.Client, authHeader v202607.RequestEditorFn, opts *JoinOptions, info *HostInfo) error {
	if opts == nil {
		return nil
	}

	hostCIDRs, err := DetectHostCIDRs()
	if err != nil {
		return fmt.Errorf("detect host CIDRs: %w", err)
	}

	listResp, listErr := client.ListNodes(ctx, opts.ClusterName, authHeader)
	if listErr != nil {
		return fmt.Errorf("list nodes: %w", listErr)
	}
	if listResp.StatusCode != 200 {
		listResp.Body.Close()
		return nil
	}

	var nodes []v202607.Node
	if decodeErr := json.NewDecoder(listResp.Body).Decode(&nodes); decodeErr != nil {
		listResp.Body.Close()
		return nil
	}
	listResp.Body.Close()

	if len(nodes) == 0 {
		return nil
	}

	var firstMismatchName, firstMismatchIP string
	for _, node := range nodes {
		if node.Spec == nil || node.Spec.Meta == nil || node.Spec.Meta.Ipv4 == nil {
			continue
		}
		nodeIP := net.ParseIP(*node.Spec.Meta.Ipv4)
		if nodeIP == nil {
			continue
		}
		matched := false
		for _, cidr := range hostCIDRs {
			if cidr.Contains(nodeIP) {
				matched = true
				break
			}
		}
		if matched {
			return nil
		}
		if firstMismatchName == "" {
			if node.Metadata != nil && node.Metadata.Name != nil {
				firstMismatchName = *node.Metadata.Name
			} else {
				firstMismatchName = "<unknown>"
			}
			firstMismatchIP = *node.Spec.Meta.Ipv4
		}
	}

	ourIP := ""
	if len(info.HostIPs) > 0 {
		ourIP = info.HostIPs[0]
	}

	return fmt.Errorf("existing node %s (%s) is not on the same host network as this node (%s)", firstMismatchName, firstMismatchIP, ourIP)
}
