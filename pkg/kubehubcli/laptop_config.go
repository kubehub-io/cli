package kubehubcli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jaypipes/ghw"
)

var logindSettings = []struct {
	key   string
	value string
}{
	{"HandleLidSwitch", "ignore"},
	{"HandleLidSwitchExternalPower", "ignore"},
	{"HandleLidSwitchDocked", "ignore"},
}

func isLaptop() bool {
	if chassis, err := ghw.Chassis(); err == nil && chassis != nil {
		typeDesc := strings.ToLower(strings.TrimSpace(chassis.TypeDescription))
		switch {
		case strings.Contains(typeDesc, "laptop"), strings.Contains(typeDesc, "notebook"),
			strings.Contains(typeDesc, "portable"), strings.Contains(typeDesc, "sub notebook"):
			return true
		case strings.Contains(typeDesc, "rack mount"), strings.Contains(typeDesc, "server"):
			return false
		}

		switch strings.TrimSpace(chassis.Type) {
		case "8", "9", "10", "14":
			return true
		case "23":
			return false
		}
	}

	return false
}

func applyLogindSettings(path string) error {
	for _, s := range logindSettings {
		pattern := fmt.Sprintf(`s|^#?%s=.*|%s=%s|`, s.key, s.key, s.value)
		if err := RunCmdCapture("sed", "-i", "-E", pattern, path); err != nil {
			return fmt.Errorf("set %s: %w", s.key, err)
		}
	}
	return nil
}

func configureLogind() error {
	slog.Info("--- Configuring logind (lid switch handling) ---")

	if err := applyLogindSettings("/etc/systemd/logind.conf"); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "restart", "systemd-logind"); err != nil {
		return fmt.Errorf("restart systemd-logind: %w", err)
	}

	slog.Info("logind configured, lid switch set to ignore")
	return nil
}
