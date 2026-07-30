package kubehubcli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func IsSystemdManaged() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

type HostHardware struct {
	CPUModel  string
	CPUCores  int
	MemoryMB  int64
	IsVirtual bool
}

type HostInfo struct {
	HostIPs    []string
	OS         string
	Distro     string
	Arch       string
	Kernel     string
	Hardware   *HostHardware
	Containerd *ContainerdInfo
	Crictl     *BinaryInfo
	Runc       *BinaryInfo
	Kubelet    *KubeletInfo
	Kubectl    *BinaryInfo
}

type ContainerdInfo struct {
	Version string
	State   string
}

type BinaryInfo struct {
	Version string
	Path    string
}

type KubeletInfo struct {
	Version   string
	State     string
	Bootstrap string
}

func DetectHost() (*HostInfo, error) {
	hostIPs, err := detectHostIPs()
	if err != nil {
		return nil, fmt.Errorf("detect host IPs: %w", err)
	}

	info := &HostInfo{
		HostIPs: hostIPs,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}

	if runtime.GOOS == "linux" {
		distro, err := detectLinuxDistro()
		if err == nil {
			info.Distro = distro
		}
	}

	if err := detectKernel(info); err != nil {
		return nil, fmt.Errorf("detect kernel: %w", err)
	}

	detectHardware(info)

	if err := detectContainerd(info); err != nil {
		return nil, fmt.Errorf("detect containerd: %w", err)
	}

	detectBinary(info, "crictl", &info.Crictl)
	detectBinary(info, "runc", &info.Runc)
	detectBinary(info, "kubectl", &info.Kubectl)
	detectKubelet(info)

	return info, nil
}

func detectHardware(info *HostInfo) {
	if runtime.GOOS != "linux" {
		return
	}

	hw := &HostHardware{}

	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		var cores int
		var model string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					model = strings.TrimSpace(parts[1])
				}
			}
			if strings.HasPrefix(line, "processor") {
				cores++
			}
		}
		hw.CPUModel = model
		hw.CPUCores = cores
	}

	memData, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(memData), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					var kb int64
					if _, err := fmt.Sscanf(parts[1], "%d", &kb); err == nil {
						hw.MemoryMB = kb / 1024
					}
				}
				break
			}
		}
	}

	dmiData, err := os.ReadFile("/sys/class/dmi/id/sys_vendor")
	if err == nil {
		vendor := strings.TrimSpace(string(dmiData))
		vendorLower := strings.ToLower(vendor)
		if vendorLower == "qemu" || vendorLower == "kvm" || vendorLower == "vmware" ||
			vendorLower == "virtualbox" || vendorLower == "xen" || vendorLower == "microsoft corporation" {
			hw.IsVirtual = true
		}
	}
	if !hw.IsVirtual {
		// Check for hypervisor flag in /proc/cpuinfo
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "flags") && strings.Contains(line, "hypervisor") {
				hw.IsVirtual = true
				break
			}
		}
	}

	info.Hardware = hw
}

func detectHostIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	var ips []string
	for _, iface := range ifaces {
		if shouldSkipInterface(iface) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip.String())
		}
	}

	return ips, nil
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.TCPAddr:
		return v.IP
	case *net.UDPAddr:
		return v.IP
	default:
		return nil
	}
}

func shouldSkipInterface(iface net.Interface) bool {
	name := iface.Name

	if strings.HasPrefix(name, "veth") ||
		strings.HasPrefix(name, "docker") ||
		strings.HasPrefix(name, "cni") ||
		strings.HasPrefix(name, "flannel") ||
		strings.HasPrefix(name, "cali") ||
		strings.HasPrefix(name, "weave") ||
		strings.HasPrefix(name, "bridge") ||
		strings.HasPrefix(name, "virbr") ||
		strings.HasPrefix(name, "vnet") ||
		strings.HasPrefix(name, "vbox") ||
		strings.HasPrefix(name, "vmnet") ||
		strings.HasPrefix(name, "utun") ||
		strings.HasPrefix(name, "awdl") ||
		strings.HasPrefix(name, "llw") {
		return true
	}

	if iface.Flags&net.FlagUp == 0 {
		return true
	}

	return false
}

func detectKernel(info *HostInfo) error {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	info.Kernel = strings.TrimSpace(string(output))
	return nil
}

func detectContainerd(info *HostInfo) error {
	cmd := exec.Command("containerd", "--version")
	output, err := cmd.Output()
	if err != nil {
		info.Containerd = nil
		return nil
	}

	version := strings.TrimSpace(string(output))

	cmd = exec.Command("systemctl", "is-active", "containerd")
	output, err = cmd.Output()
	state := "unknown"
	if err == nil {
		state = strings.TrimSpace(string(output))
	}

	info.Containerd = &ContainerdInfo{
		Version: version,
		State:   state,
	}

	return nil
}

func detectBinary(info *HostInfo, name string, dest **BinaryInfo) {
	path, err := exec.LookPath(name)
	if err != nil {
		*dest = nil
		return
	}

	var version string
	cmd := exec.Command(name, "--version")
	output, err := cmd.Output()
	if err != nil {
		*dest = &BinaryInfo{Path: path}
		return
	}

	version = strings.TrimSpace(string(output))
	*dest = &BinaryInfo{Version: version, Path: path}
}

func detectKubelet(info *HostInfo) {
	_, err := exec.LookPath("kubelet")
	if err != nil {
		info.Kubelet = nil
		return
	}

	cmd := exec.Command("kubelet", "--version")
	output, err := cmd.Output()
	version := "unknown"
	if err == nil {
		version = strings.TrimSpace(string(output))
	}

	cmd = exec.Command("systemctl", "is-active", "kubelet")
	state := "unknown"
	output, err = cmd.Output()
	if err == nil {
		state = strings.TrimSpace(string(output))
	}

	bootstrap := "no"
	if _, err := os.Stat("/var/lib/kubelet/pki/kubelet-client.crt"); err == nil {
		bootstrap = "yes"
	} else if _, err := os.Stat("/var/lib/kubelet/config.yaml"); err == nil {
		bootstrap = "yes"
	}

	info.Kubelet = &KubeletInfo{
		Version:   version,
		State:     state,
		Bootstrap: bootstrap,
	}
}

func ValidateNodeIP(ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			addrIP := addrIP(addr)
			if addrIP == nil || addrIP.IsLoopback() || addrIP.IsLinkLocalUnicast() {
				continue
			}

			if addrIP.Equal(parsed) {
				return nil
			}
		}
	}

	return fmt.Errorf("IP %s is not attached to any local interface", ip)
}

func detectLinuxDistro() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("read os-release: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, `"`)
			return id, nil
		}
	}

	return "", fmt.Errorf("could not determine distro")
}
