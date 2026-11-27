package kubehubcli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKubeletExecStartUsesLocalBinaryAndNodeLabels(t *testing.T) {
	execStart := kubeletExecStart(map[string]string{
		"runtime/gvisor": "enabled",
		"zone":           "test",
	})

	require.Contains(t, execStart, "/usr/local/bin/kubelet")
	require.Contains(t, execStart, "--kubeconfig="+KubeletKubeconfigPath)
	require.Contains(t, execStart, "--config="+KubeletConfigPath)
	require.Contains(t, execStart, "--node-labels=runtime/gvisor=enabled,zone=test")
	require.NotContains(t, execStart, "$KUBELET_")
}

func TestKubeletDropInOverridesPackageManagedDropIns(t *testing.T) {
	execStart := kubeletExecStart(nil)
	dropIn := renderKubeletDropIn(execStart)

	require.Contains(t, dropIn, "EnvironmentFile=")
	require.Contains(t, dropIn, "Environment=KUBELET_KUBECONFIG_ARGS=")
	require.Contains(t, dropIn, "Environment=KUBELET_KUBEADM_ARGS=")
	require.Contains(t, dropIn, "ExecStart=\nExecStart="+execStart)

	lines := strings.Split(dropIn, "\n")
	var resetIndex, execIndex int
	for i, line := range lines {
		if line == "ExecStart=" {
			resetIndex = i
		}
		if strings.HasPrefix(line, "ExecStart=/usr/local/bin/kubelet") {
			execIndex = i
		}
	}
	require.Less(t, resetIndex, execIndex, "drop-in must reset the inherited ExecStart before setting it")
	require.True(t, strings.HasSuffix(KubeletServiceDropInPath, "/99-kubehubcli.conf"))
}

func TestKubeletServiceTemplateUsesRenderedExecStart(t *testing.T) {
	rendered, err := renderTemplateFromEmbed(NodeConfigs, "configassets/kubelet.service", map[string]string{
		"ExecStart": "kubelet-test-command",
	})
	require.NoError(t, err)
	require.Contains(t, string(rendered), "ExecStart=kubelet-test-command")
}
