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

// DetectPrimaryHostCIDRs returns only the IPv4 subnets of the network the node
// actually routes traffic on: the interface that owns hostIP, or, failing that,
// the interface that carries the default route. It intentionally ignores
// transient virtual/CNI/bridge interfaces (docker0, cni0, incusbr0, tailscale,
// etc.) which can come and go between boots and have addresses that spuriously
// overlap cluster pod/service CIDRs.
func DetectPrimaryHostCIDRs(hostIP string) ([]*net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var targets []net.Interface
	if parsed := net.ParseIP(hostIP); parsed != nil {
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
				if ipnet.IP.Equal(parsed) {
					targets = append(targets, iface)
				}
			}
		}
	}

	if len(targets) == 0 {
		if routeIface, err := defaultRouteInterface(); err == nil && routeIface != "" {
			for _, iface := range ifaces {
				if iface.Name == routeIface {
					targets = append(targets, iface)
				}
			}
		}
	}

	var cidrs []*net.IPNet
	for _, iface := range targets {
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

type hostNetwork struct {
	Iface   string
	CIDR    *net.IPNet
	Virtual bool
}

func (h hostNetwork) Describe() string {
	if h.CIDR == nil {
		return h.Iface
	}
	if h.Virtual {
		return fmt.Sprintf("%s network (%s)", h.Iface, h.CIDR)
	}
	return fmt.Sprintf("host network (%s)", h.CIDR)
}

// detectHostNetworks lists every up IPv4 network on the host, tagged with the
// owning interface and whether that interface is virtual/bridge. This powers
// precise conflict attribution (e.g. "host network (10.146.166.0/24)" vs
// "cni0 network (10.0.0.0/8)").
func detectHostNetworks() []hostNetwork {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var nets []hostNetwork
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
			nets = append(nets, hostNetwork{
				Iface:   iface.Name,
				CIDR:    ipnet,
				Virtual: isVirtualInterface(iface.Name),
			})
		}
	}
	return nets
}

// findConflictingNetwork returns the host network that overlaps targetCIDR, or
// nil if none does.
func findConflictingNetwork(hostNets []hostNetwork, targetCIDR string) *hostNetwork {
	_, cidrNet, err := net.ParseCIDR(targetCIDR)
	if err != nil {
		return nil
	}
	for i := range hostNets {
		if hostNets[i].CIDR != nil && cidrOverlaps(hostNets[i].CIDR, cidrNet) {
			return &hostNets[i]
		}
	}
	return nil
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
