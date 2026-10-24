package kubehubcli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinPlanWriteToFormatsSectionsAndWrapsText(t *testing.T) {
	plan := JoinPlan{
		Network: []JoinPlanItem{{
			Summary:     "Random MAC → permanent MAC on enp0s3",
			Description: "Random MAC addresses can change after reboot and are not suitable for a server.",
		}},
		System:   []JoinPlanItem{{Summary: "Apply Kubernetes kernel settings"}},
		Software: []JoinPlanItem{{Summary: "crictl v1.33.0"}},
	}

	var output strings.Builder
	require.NoError(t, plan.writeTo(&output))
	text := output.String()
	for _, expected := range []string{
		"NODE JOIN PLAN",
		"NETWORK SETUP",
		"Random MAC → permanent MAC on enp0s3",
		"suitable for a server.",
		"SYSTEM SETUP",
		"SOFTWARE INSTALL",
		"crictl v1.33.0",
	} {
		require.Contains(t, text, expected)
	}

	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		require.Equal(t, 70, len([]rune(line)), "plan line should align with the border: %q", line)
	}
}
