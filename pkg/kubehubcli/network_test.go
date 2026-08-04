package kubehubcli

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, ipn, err := net.ParseCIDR(s)
	require.NoError(t, err, "parse cidr %s", s)
	return ipn
}

func hostNet(iface, cidr string, virtual bool) hostNetwork {
	_, ipn, _ := net.ParseCIDR(cidr)
	return hostNetwork{Iface: iface, CIDR: ipn, Virtual: virtual}
}

func TestDetectHostNetworkDescribe(t *testing.T) {
	require.Equal(t, "host network (10.146.166.0/24)", hostNet("enp5s0", "10.146.166.0/24", false).Describe())
	require.Equal(t, "multipass network (10.0.0.0/8)", hostNet("multipass", "10.0.0.0/8", true).Describe())
	require.Equal(t, "cni0 network (10.10.5.0/24)", hostNet("cni0", "10.10.5.0/24", true).Describe())
	require.Equal(t, "somename", hostNetwork{Iface: "somename"}.Describe())
}

func TestDescribeCIDRConflicts_NoConflict(t *testing.T) {
	hostNets := []hostNetwork{hostNet("enp5s0", "10.146.166.0/24", false)}
	require.Empty(t, describeCIDRConflicts(hostNets, "10.10.0.0/16", "10.20.0.0/16"))
}

func TestDescribeCIDRConflicts_HostNetwork(t *testing.T) {
	hostNets := []hostNetwork{hostNet("enp5s0", "10.146.166.0/24", false)}
	conflicts := describeCIDRConflicts(hostNets, "10.146.0.0/16", "10.20.0.0/16")
	require.Len(t, conflicts, 1)
	require.Equal(t, "Pod CIDR (10.146.0.0/16) conflicts with host network (10.146.166.0/24)", conflicts[0])
}

func TestDescribeCIDRConflicts_VirtualBridge(t *testing.T) {
	hostNets := []hostNetwork{hostNet("multipass", "10.0.0.0/8", true)}
	conflicts := describeCIDRConflicts(hostNets, "10.10.0.0/16", "10.0.10.0/16")
	require.Len(t, conflicts, 2)
	require.Contains(t, conflicts[0], "Pod CIDR (10.10.0.0/16) conflicts with multipass network (10.0.0.0/8)")
	require.Contains(t, conflicts[1], "Service CIDR (10.0.10.0/16) conflicts with multipass network (10.0.0.0/8)")
}

func TestDescribeCIDRConflicts_PodAndServiceDifferentSources(t *testing.T) {
	hostNets := []hostNetwork{
		hostNet("enp5s0", "10.146.166.0/24", false),
		hostNet("cni0", "10.20.0.0/24", true),
	}
	conflicts := describeCIDRConflicts(hostNets, "10.146.0.0/16", "10.20.0.0/16")
	require.Len(t, conflicts, 2)
	require.Equal(t, "Pod CIDR (10.146.0.0/16) conflicts with host network (10.146.166.0/24)", conflicts[0])
	require.Equal(t, "Service CIDR (10.20.0.0/16) conflicts with cni0 network (10.20.0.0/24)", conflicts[1])
}

func TestHostCIDRConflicts_NoConflict(t *testing.T) {
	// The reported scenario: pod 10.10.0.0/16 and service 10.20.0.0/16
	// vs a host on 10.146.166.0/24 -> genuinely no overlap.
	host := []*net.IPNet{mustCIDR(t, "10.146.166.0/24")}

	require.False(t, HostCIDRConflicts(host, "10.10.0.0/16"),
		"pod CIDR must not conflict with 10.146.166.0/24")
	require.False(t, HostCIDRConflicts(host, "10.20.0.0/16"),
		"service CIDR must not conflict with 10.146.166.0/24")
}

func TestHostCIDRConflicts_Overlap(t *testing.T) {
	host := []*net.IPNet{
		mustCIDR(t, "10.10.0.0/16"),
		mustCIDR(t, "192.168.1.0/24"),
	}

	cases := []struct {
		cidr    string
		want    bool
		message string
	}{
		{"10.10.0.0/16", true, "exact same network must conflict"},
		{"10.10.5.0/24", true, "subnet inside host CIDR must conflict"},
		{"10.20.0.0/16", false, "distinct /16 must not conflict"},
		{"192.168.1.10/32", true, "host address within host network must conflict"},
		{"172.16.0.0/12", false, "unrelated private range must not conflict"},
	}

	for _, tc := range cases {
		t.Run(tc.cidr, func(t *testing.T) {
			require.Equal(t, tc.want, HostCIDRConflicts(host, tc.cidr), tc.message)
		})
	}
}

func TestHostCIDRConflicts_BidirectionalOverlap(t *testing.T) {
	// cidrOverlaps checks both directions: a host /24 fully inside a cluster
	// /8, and a host /16 fully surrounding a cluster /24.
	require.True(t, HostCIDRConflicts([]*net.IPNet{mustCIDR(t, "10.0.0.0/8")}, "10.20.0.0/16"))
	require.True(t, HostCIDRConflicts([]*net.IPNet{mustCIDR(t, "10.20.0.0/16")}, "10.20.0.0/24"))
}

func TestHostCIDRConflicts_InvalidCIDR(t *testing.T) {
	require.False(t, HostCIDRConflicts([]*net.IPNet{mustCIDR(t, "10.0.0.0/8")}, "not-a-cidr"))
}

func TestHostCIDRConflicts_IgnoresNoHosts(t *testing.T) {
	require.False(t, HostCIDRConflicts(nil, "10.10.0.0/16"))
}

func TestPickNonConflictingCIDRs(t *testing.T) {
	// A host on 10.146.166.0/24 presents no conflict; the first fallback pool
	// (172.19/172.20) should be selected.
	pod, svc, dns := PickNonConflictingCIDRs([]*net.IPNet{mustCIDR(t, "10.146.166.0/24")})
	require.Equal(t, "172.19.0.0/16", pod)
	require.Equal(t, "172.20.0.0/16", svc)
	require.Equal(t, "172.20.0.10", dns)
}

func TestPickNonConflictingCIDRs_Exhausted(t *testing.T) {
	// Host occupies every fallback network -> no option is returned.
	host := []*net.IPNet{
		mustCIDR(t, "172.19.0.0/16"),
		mustCIDR(t, "172.20.0.0/16"),
		mustCIDR(t, "172.21.0.0/16"),
		mustCIDR(t, "172.22.0.0/16"),
		mustCIDR(t, "10.100.0.0/16"),
		mustCIDR(t, "10.200.0.0/16"),
	}
	pod, svc, dns := PickNonConflictingCIDRs(host)
	require.Equal(t, "", pod)
	require.Equal(t, "", svc)
	require.Equal(t, "", dns)
}

func TestPickNonConflictingCIDRs_SkipsConflicting(t *testing.T) {
	// If the first fallback pod CIDR is taken, it must move to the next pool.
	host := []*net.IPNet{mustCIDR(t, "172.19.0.0/16")}
	pod, svc, _ := PickNonConflictingCIDRs(host)
	require.Equal(t, "172.21.0.0/16", pod)
	require.Equal(t, "172.22.0.0/16", svc)
}

func TestIsVirtualInterface(t *testing.T) {
	require.True(t, isVirtualInterface("cni0"))
	require.True(t, isVirtualInterface("docker0"))
	require.True(t, isVirtualInterface("incusbr0"))
	require.True(t, isVirtualInterface("br-test"))
	require.True(t, isVirtualInterface("veth1234"))
	require.True(t, isVirtualInterface("flannel.1"))
	require.False(t, isVirtualInterface("enp5s0"))
	require.False(t, isVirtualInterface("eth0"))
	require.False(t, isVirtualInterface("wlan0"))
}