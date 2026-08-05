package kubehubcli

import (
	"fmt"
	"testing"

	"github.com/gogrlx/snack"
	"github.com/stretchr/testify/require"
)

func TestNodeConfig(t *testing.T) {
	data, err := NodeConfigs.ReadFile("configassets/98-kubernetes.conf")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func withEUID(t *testing.T, euid int) {
	t.Helper()
	orig := currentEUID
	t.Cleanup(func() { currentEUID = orig })
	currentEUID = func() int { return euid }
}

func TestSudoArgsWhenRoot(t *testing.T) {
	withEUID(t, 0)
	require.Equal(t, []string{"mkdir", "-p", "/etc/kubernetes"}, sudoArgs("mkdir", "-p", "/etc/kubernetes"))
	require.Equal(t, []string{"mkdir"}, sudoArgs("mkdir"))
}

func TestSudoArgsWhenNonRoot(t *testing.T) {
	withEUID(t, 1000)
	require.Equal(t, []string{"sudo", "mkdir", "-p", "/etc/kubernetes"}, sudoArgs("mkdir", "-p", "/etc/kubernetes"))
	require.Equal(t, []string{"sudo", "mkdir"}, sudoArgs("mkdir"))
}

func TestSnackOptionsWhenRoot(t *testing.T) {
	withEUID(t, 0)
	require.Len(t, snackOptions(), 0)
	require.Len(t, snackOptions(snack.WithAssumeYes()), 1)
}

func TestSnackOptionsWhenNonRoot(t *testing.T) {
	withEUID(t, 1000)
	require.Len(t, snackOptions(), 1)
}

func TestReloadSysctlBusyBoxFallback(t *testing.T) {
	orig := execSysctl
	t.Cleanup(func() { execSysctl = orig })

	var calls [][]string
	execSysctl = func(args ...string) error {
		calls = append(calls, args)
		if args[1] == "--system" {
			return fmt.Errorf("sysctl: unrecognized option: system")
		}
		return nil
	}

	require.NoError(t, reloadSysctl("/etc/sysctl.d/98-kubernetes.conf"))
	require.Equal(t, [][]string{
		{"sysctl", "--system"},
		{"sysctl", "-p", "/etc/sysctl.d/98-kubernetes.conf"},
	}, calls)
}

func TestReloadSysctlProcps(t *testing.T) {
	orig := execSysctl
	t.Cleanup(func() { execSysctl = orig })

	var calls [][]string
	execSysctl = func(args ...string) error {
		calls = append(calls, args)
		return nil
	}

	require.NoError(t, reloadSysctl("/etc/sysctl.d/98-kubernetes.conf"))
	require.Equal(t, [][]string{{"sysctl", "--system"}}, calls)
}

func TestReloadSysctlBusyBoxNoFiles(t *testing.T) {
	orig := execSysctl
	t.Cleanup(func() { execSysctl = orig })

	execSysctl = func(args ...string) error {
		return fmt.Errorf("sysctl: unrecognized option: system")
	}

	require.NoError(t, reloadSysctl())
}
