package kubehubcli

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

var autoAccept bool

func SetAutoAccept(v bool) {
	autoAccept = v
}

func PromptConfirm(msg string, args ...any) {
	if autoAccept {
		slog.Info(fmt.Sprintf(msg+" [auto-accepted]", args...))
		return
	}
	fmt.Fprintf(os.Stdout, msg+" Press Enter to continue: ", args...)
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func promptSelect(msg string, options []string) int {
	if autoAccept {
		slog.Error("interactive selection required but -y flag is set; cannot auto-select")
		os.Exit(1)
	}
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

// AutoAccept returns true if the -y flag is set, indicating prompts should be
// skipped. Callers with inline stdin reads can use this to skip confirmation.
func AutoAccept() bool {
	return autoAccept
}
