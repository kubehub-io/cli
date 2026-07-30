package kubehubcli

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
)

type OSConfigurator interface {
	Name() string
	ResolvConfPath() string
	DataRoot() string
	ConfigureStaticIP(iface, ip, gateway string, dnsServers []string) error
	DisableRandomMAC(iface string) error
}

func DetectOSConfigurator(distro string) OSConfigurator {
	switch distro {
	case "ubuntu":
		return &ubuntuConfigurator{}
	case "debian":
		return &debianConfigurator{}
	case "fedora", "centos", "rhel":
		return &fedoraConfigurator{}
	default:
		return &ubuntuConfigurator{}
	}
}

type ubuntuConfigurator struct{}

func (u *ubuntuConfigurator) Name() string { return "ubuntu" }

func (u *ubuntuConfigurator) ResolvConfPath() string {
	return DetectResolvConfFile()
}

func (u *ubuntuConfigurator) DataRoot() string {
	return "/var/lib"
}

func (u *ubuntuConfigurator) ConfigureStaticIP(iface, ip, gateway string, dnsServers []string) error {
	netplanDir := "/etc/netplan"

	prefix := detectPrefix(iface, ip)
	ipCIDR := fmt.Sprintf("%s/%d", ip, prefix)

	cfg := fmt.Sprintf(`network:
  version: 2
  renderer: networkd
  ethernets:
    %s:
      dhcp4: false
      addresses:
        - %s
      routes:
        - to: default
          via: %s
      nameservers:
        addresses:
`, iface, ipCIDR, gateway)
	for _, dns := range dnsServers {
		if strings.Contains(dns, "%") {
			continue
		}
		cfg += fmt.Sprintf("          - %s\n", dns)
	}

	if err := writeFileAsRoot(filepath.Join(netplanDir, "99-kubehub-static.yaml"), []byte(cfg), 0600); err != nil {
		return fmt.Errorf("write netplan config: %w", err)
	}
	if err := RunCmdCapture("netplan", "apply"); err != nil {
		return fmt.Errorf("apply netplan: %w", err)
	}
	return nil
}

func (u *ubuntuConfigurator) DisableRandomMAC(iface string) error {
	nmDir := "/etc/NetworkManager/conf.d"
	cfg := fmt.Sprintf(`[connection]
ethernet.cloned-mac-address=permanent
wifi.cloned-mac-address=permanent
`)
	if iface != "" {
		cfg = fmt.Sprintf(`[connection]
match-device=interface-name:%s
ethernet.cloned-mac-address=permanent
wifi.cloned-mac-address=permanent
`, iface)
	}

	if err := writeFileAsRoot(filepath.Join(nmDir, "99-kubehub-disable-random-mac.conf"), []byte(cfg), 0644); err != nil {
		return fmt.Errorf("write NetworkManager config: %w", err)
	}
	if err := RunCmdCapture("systemctl", "try-reload-or-restart", "NetworkManager"); err != nil {
		return fmt.Errorf("reload NetworkManager: %w", err)
	}
	return nil
}

type debianConfigurator struct{}

func (d *debianConfigurator) Name() string { return "debian" }

func (d *debianConfigurator) ResolvConfPath() string {
	return DetectResolvConfFile()
}

func (d *debianConfigurator) DataRoot() string {
	// Debian often keeps /srv as a separate large partition. Check if it exists.
	if srvHasSpace() {
		return "/srv"
	}
	return "/var/lib"
}

func srvHasSpace() bool {
	cmd := exec.Command("df", "--output=target", "/srv")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	mount := strings.TrimSpace(string(out))
	lines := strings.Split(mount, "\n")
	if len(lines) < 2 {
		return false
	}
	return lines[1] == "/srv"
}

func (d *debianConfigurator) ConfigureStaticIP(iface, ip, gateway string, dnsServers []string) error {
	prefix := detectPrefix(iface, ip)
	m := net.CIDRMask(prefix, 32)
	netmask := fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3])
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`
auto %s
iface %s inet static
    address %s
    netmask %s
    gateway %s
`, iface, iface, ip, netmask, gateway))
	for _, dns := range dnsServers {
		if strings.Contains(dns, "%") {
			continue
		}
		sb.WriteString(fmt.Sprintf("    dns-nameservers %s\n", dns))
	}

	if err := RunCmdCapture("mkdir", "-p", "/etc/network/interfaces.d"); err != nil {
		return fmt.Errorf("create interfaces.d: %w", err)
	}

	if err := writeFileAsRoot("/etc/network/interfaces.d/99-kubehub-static", []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write interfaces config: %w", err)
	}
	return nil
}

func (d *debianConfigurator) DisableRandomMAC(iface string) error {
	u := &ubuntuConfigurator{}
	return u.DisableRandomMAC(iface)
}

type fedoraConfigurator struct{}

func (f *fedoraConfigurator) Name() string { return "fedora" }

func (f *fedoraConfigurator) ResolvConfPath() string {
	return DetectResolvConfFile()
}

func (f *fedoraConfigurator) DataRoot() string {
	return "/var/lib"
}

func (f *fedoraConfigurator) ConfigureStaticIP(iface, ip, gateway string, dnsServers []string) error {
	var filteredDNS []string
	for _, dns := range dnsServers {
		if !strings.Contains(dns, "%") {
			filteredDNS = append(filteredDNS, dns)
		}
	}
	dnsServers = filteredDNS

	if _, err := exec.LookPath("nmcli"); err == nil {
		prefix := detectPrefix(iface, ip)
		nmArgs := []string{
			"nmcli", "connection", "modify", iface,
			"ipv4.addresses", fmt.Sprintf("%s/%d", ip, prefix),
			"ipv4.gateway", gateway,
		}
		nmArgs = append(nmArgs, "ipv4.dns", strings.Join(dnsServers, " "))
		nmArgs = append(nmArgs, "ipv4.method", "manual")
		if err := RunCmdCapture(nmArgs[0], nmArgs[1:]...); err != nil {
			return fmt.Errorf("nmcli: %w", err)
		}
		if err := RunCmdCapture("nmcli", "connection", "up", iface); err != nil {
			return fmt.Errorf("nmcli: %w", err)
		}
		return nil
	}

	cfg := fmt.Sprintf(`[connection]
id=%s
type=ethernet
interface-name=%s
autoconnect=true

[ipv4]
method=manual
addresses=%s/%d
gateway=%s
dns=%s
`, iface, iface, ip, detectPrefix(iface, ip), gateway, strings.Join(dnsServers, ";"))

	nmDir := "/etc/NetworkManager/system-connections"
	if err := RunCmdCapture("mkdir", "-p", nmDir); err != nil {
		return fmt.Errorf("create NM connections dir: %w", err)
	}
	if err := writeFileAsRoot(filepath.Join(nmDir, "99-kubehub-static.nmconnection"), []byte(cfg), 0600); err != nil {
		return fmt.Errorf("write NM keyfile: %w", err)
	}
	if err := RunCmdCapture("systemctl", "reload", "NetworkManager"); err != nil {
		return fmt.Errorf("reload NetworkManager: %w", err)
	}
	return nil
}

func (f *fedoraConfigurator) DisableRandomMAC(iface string) error {
	u := &ubuntuConfigurator{}
	return u.DisableRandomMAC(iface)
}

func defaultRouteInterface() (string, error) {
	routes, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", fmt.Errorf("get default route: %w", err)
	}
	fields := strings.Fields(string(routes))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no default route interface found")
}

func defaultGateway() (string, error) {
	routes, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", fmt.Errorf("get default route: %w", err)
	}
	fields := strings.Fields(string(routes))
	if len(fields) >= 3 {
		return fields[2], nil
	}
	return "", fmt.Errorf("no default gateway found")
}

func hasRandomMAC(iface string) bool {
	mac, err := net.InterfaceByName(iface)
	if err != nil {
		return false
	}
	raw := mac.HardwareAddr
	if len(raw) == 0 {
		return false
	}
	return raw[0]&0x02 != 0
}

func detectPrefix(iface, ip string) int {
	ief, err := net.InterfaceByName(iface)
	if err != nil {
		return 24
	}
	addrs, err := ief.Addrs()
	if err != nil {
		return 24
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ipnet.IP.Equal(net.ParseIP(ip)) {
			ones, _ := ipnet.Mask.Size()
			return ones
		}
	}
	return 24
}
