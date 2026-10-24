package kubehubcli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type OSConfigurator interface {
	Name() string
	ResolvConfPath() string
	DataRoot() string
	ConfigureStaticIP(iface, ip, gateway string, dnsServers []string) error
	CanConfigureStaticIP(iface string) bool
	DisableRandomMAC(iface string) error
	CanDisableRandomMAC() bool
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
	if isWirelessInterface(iface) {
		if !hasCommand("nmcli") {
			return fmt.Errorf("wireless interface %s requires NetworkManager (nmcli) for static IP configuration", iface)
		}
		// A previous run may have written an ethernet definition for this
		// wireless device, which netplan rejects as a device type change.
		_ = RunCmdCapture("rm", "-f", "/etc/netplan/99-kubehub-static.yaml")
		return configureStaticIPWithNetworkManager(iface, ip, gateway, dnsServers)
	}

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

func (u *ubuntuConfigurator) CanConfigureStaticIP(iface string) bool {
	if isWirelessInterface(iface) {
		return hasCommand("nmcli")
	}
	return true
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

	if err := RunCmdCapture("mkdir", "-p", nmDir); err != nil {
		return fmt.Errorf("create NetworkManager config directory: %w", err)
	}
	if err := writeFileAsRoot(filepath.Join(nmDir, "99-kubehub-disable-random-mac.conf"), []byte(cfg), 0644); err != nil {
		return fmt.Errorf("write NetworkManager config: %w", err)
	}
	if err := RunCmdCapture("systemctl", "try-reload-or-restart", "NetworkManager"); err != nil {
		return fmt.Errorf("reload NetworkManager: %w", err)
	}
	return nil
}

func (u *ubuntuConfigurator) CanDisableRandomMAC() bool {
	return networkManagerAvailable()
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
	if isWirelessInterface(iface) {
		if !hasCommand("nmcli") {
			return fmt.Errorf("wireless interface %s requires NetworkManager (nmcli) for static IP configuration", iface)
		}
		return configureStaticIPWithNetworkManager(iface, ip, gateway, dnsServers)
	}

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

func (d *debianConfigurator) CanConfigureStaticIP(iface string) bool {
	u := &ubuntuConfigurator{}
	return u.CanConfigureStaticIP(iface)
}

func (d *debianConfigurator) DisableRandomMAC(iface string) error {
	u := &ubuntuConfigurator{}
	return u.DisableRandomMAC(iface)
}

func (d *debianConfigurator) CanDisableRandomMAC() bool {
	u := &ubuntuConfigurator{}
	return u.CanDisableRandomMAC()
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

	return configureStaticIPWithNetworkManager(iface, ip, gateway, dnsServers)
}

func (f *fedoraConfigurator) CanConfigureStaticIP(iface string) bool {
	u := &ubuntuConfigurator{}
	return u.CanConfigureStaticIP(iface)
}

func (f *fedoraConfigurator) DisableRandomMAC(iface string) error {
	u := &ubuntuConfigurator{}
	return u.DisableRandomMAC(iface)
}

func (f *fedoraConfigurator) CanDisableRandomMAC() bool {
	u := &ubuntuConfigurator{}
	return u.CanDisableRandomMAC()
}

// configureStaticIPWithNetworkManager applies a static IPv4 address to an
// existing NetworkManager connection. This preserves the credentials of an
// already-configured Wi-Fi connection, which netplan cannot do without the
// access point passphrase.
func configureStaticIPWithNetworkManager(iface, ip, gateway string, dnsServers []string) error {
	prefix := detectPrefix(iface, ip)

	if hasCommand("nmcli") {
		conn := iface
		if out, err := exec.Command("nmcli", "-g", "GENERAL.CONNECTION", "device", "show", iface).Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" && name != "--" {
				conn = name
			}
		}
		if err := RunCmdCapture("nmcli", "connection", "modify", conn,
			"ipv4.addresses", fmt.Sprintf("%s/%d", ip, prefix),
			"ipv4.gateway", gateway,
			"ipv4.dns", strings.Join(dnsServers, " "),
			"ipv4.method", "manual",
		); err != nil {
			return fmt.Errorf("nmcli: %w", err)
		}
		if err := RunCmdCapture("nmcli", "connection", "up", conn); err != nil {
			return fmt.Errorf("nmcli: %w", err)
		}
		return nil
	}

	if isWirelessInterface(iface) {
		return fmt.Errorf("wireless interface %s requires nmcli for static IP configuration", iface)
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
`, iface, iface, ip, prefix, gateway, strings.Join(dnsServers, ";"))

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

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func networkManagerAvailable() bool {
	if hasCommand("nmcli") || hasCommand("NetworkManager") {
		return true
	}
	_, err := os.Stat("/etc/NetworkManager")
	return err == nil
}

func isWirelessInterface(iface string) bool {
	if iface == "" {
		return false
	}
	for _, name := range []string{"wireless", "phy80211"} {
		if _, err := os.Stat(filepath.Join("/sys/class/net", iface, name)); err == nil {
			return true
		}
	}
	return false
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
