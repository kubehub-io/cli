package kubehubcli

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func promptConfirm(msg string, args ...any) bool {
	fmt.Fprintf(os.Stdout, msg+" (yes/no): ", args...)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	input := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(input, "yes") || strings.EqualFold(input, "y")
}

func promptSelect(msg string, options []string) int {
	slog.Info(msg)
	for i, opt := range options {
		slog.Info(fmt.Sprintf("  [%d] %s", i+1, opt))
	}
	fmt.Fprintf(os.Stdout, "Choose an option (1-%d): ", len(options))

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return -1
	}
	input := strings.TrimSpace(scanner.Text())
	var choice int
	if _, err := fmt.Sscanf(input, "%d", &choice); err != nil {
		return -1
	}
	if choice < 1 || choice > len(options) {
		return -1
	}
	return choice - 1
}
