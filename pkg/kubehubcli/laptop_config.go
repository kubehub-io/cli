package kubehubcli

import (
	"fmt"
	"log/slog"
)

var logindSettings = []struct {
	key   string
	value string
}{
	{"HandleLidSwitch", "ignore"},
	{"HandleLidSwitchExternalPower", "ignore"},
	{"HandleLidSwitchDocked", "ignore"},
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
