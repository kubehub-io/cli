package kubehubcli

import (
	"bufio"
	"os"
	"strings"
)

const (
	resolvConfSystemdResolved = "/run/systemd/resolve/resolv.conf"
	resolvConfDefault         = "/etc/resolv.conf"
)

func DetectResolvConfFile() string {
	// If systemd-resolved provides a real resolv.conf, prefer it.
	// The default /etc/resolv.conf on such systems usually points to
	// 127.0.0.53 which is unreachable from pod network namespaces.
	if _, err := os.Stat(resolvConfSystemdResolved); err == nil {
		return resolvConfSystemdResolved
	}

	nameservers := readNameservers(resolvConfDefault)
	if len(nameservers) == 0 {
		return ""
	}

	for _, ns := range nameservers {
		if ns == "127.0.0.53" {
			return resolvConfDefault
		}
	}

	return resolvConfDefault
}

func readNameservers(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var nss []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				nss = append(nss, fields[1])
			}
		}
	}
	return nss
}
