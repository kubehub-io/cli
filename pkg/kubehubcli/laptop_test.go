package kubehubcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigureLogind(t *testing.T) {
	src := filepath.Join("..", "testdata", "logind.conf")
	data, err := os.ReadFile(src)
	require.NoError(t, err)

	tmp := filepath.Join(t.TempDir(), "logind.conf")
	require.NoError(t, os.WriteFile(tmp, data, 0644))

	require.NoError(t, applyLogindSettings(tmp))

	modified, err := os.ReadFile(tmp)
	require.NoError(t, err)

	for _, s := range logindSettings {
		found := false
		for _, line := range strings.Split(string(modified), "\n") {
			if !strings.Contains(line, s.key+"=") {
				continue
			}
			found = true
			require.Equal(t, s.key+"="+s.value, strings.TrimSpace(line),
				"line with %s has wrong value", s.key)
			require.False(t, strings.HasPrefix(strings.TrimSpace(line), "#"),
				"line with %s is still commented", s.key)
		}
		require.True(t, found, "no line containing %s= found", s.key)
	}
}
