package kubehubcli

import (
	"fmt"
	"net"
	"strings"
)

var fallbackNetworks = []struct {
	PodCIDR  string
	SvcCIDR  string
	DNSIP    string
}{
	{PodCIDR: "172.19.0.0/16", SvcCIDR: "172.20.0.0/16", DNSIP: "172.20.0.10"},
	{PodCIDR: "172.21.0.0/16", SvcCIDR: "172.22.0.0/16", DNSIP: "172.22.0.10"},
	{PodCIDR: "10.100.0.0/16", SvcCIDR: "10.200.0.0/16", DNSIP: "10.200.0.10"},
}

var virtualPrefixes = []string{
	"veth", "docker", "cni", "flannel", "cali",
	"weave", "bridge", "virbr", "vnet", "vbox",
	"vmnet", "utun", "awdl", "llw", "incus",
	"lxc", "lxd", "br-", "tap", "tun",
}

func isVirtualInterface(name string) bool {
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func DetectHostCIDRs() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var cidrs []*net.IPNet
	for _, iface := range ifaces {
		if isVirtualInterface(iface.Name) {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			cidrs = append(cidrs, ipnet)
		}
	}

	return cidrs, nil
}

func DetectAllHostCIDRs() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var cidrs []*net.IPNet
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			cidrs = append(cidrs, ipnet)
		}
	}

	return cidrs, nil
}

func DetectVirtualHostCIDRs() ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var cidrs []*net.IPNet
	for _, iface := range ifaces {
		if !isVirtualInterface(iface.Name) {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			cidrs = append(cidrs, ipnet)
		}
	}

	return cidrs, nil
}

func cidrOverlaps(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func HostCIDRConflicts(hostCIDRs []*net.IPNet, cidrStr string) bool {
	_, cidrNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return false
	}
	for _, hc := range hostCIDRs {
		if cidrOverlaps(hc, cidrNet) {
			return true
		}
	}
	return false
}

func PickNonConflictingCIDRs(hostCIDRs []*net.IPNet) (podCIDR, svcCIDR, dnsIP string) {
	for _, fb := range fallbackNetworks {
		if !HostCIDRConflicts(hostCIDRs, fb.PodCIDR) && !HostCIDRConflicts(hostCIDRs, fb.SvcCIDR) {
			return fb.PodCIDR, fb.SvcCIDR, fb.DNSIP
		}
	}
	return "", "", ""
}
