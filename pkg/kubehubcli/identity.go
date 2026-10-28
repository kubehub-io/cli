package kubehubcli

import (
	"fmt"
	"os"
	"strings"
)

// LocalNodeName returns the name this host uses when it registers as a cluster
// node. Join and every day-2 operation that changes or removes the local host
// resolve the node name the same way so they always target the node backed by
// this machine.
func LocalNodeName() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("get hostname: %w", err)
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "", fmt.Errorf("hostname is empty")
	}
	return hostname, nil
}
