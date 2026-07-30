package kubehubcli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeConfig(t *testing.T) {
	data, err := NodeConfigs.ReadFile("configassets/98-kubernetes.conf")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}
