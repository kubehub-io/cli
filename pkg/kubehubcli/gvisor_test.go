package kubehubcli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGvisorArch(t *testing.T) {
	arch, err := gvisorArch("amd64")
	require.NoError(t, err)
	require.Equal(t, "x86_64", arch)

	arch, err = gvisorArch("arm64")
	require.NoError(t, err)
	require.Equal(t, "aarch64", arch)

	_, err = gvisorArch("riscv64")
	require.Error(t, err)
}

func TestContainerdMajorVersion(t *testing.T) {
	tests := []struct {
		version string
		want    int
	}{
		{"containerd github.com/containerd/containerd v2.3.6 0d4bd6d0a1b0e0e2", 2},
		{"containerd github.com/containerd/containerd v1.7.27 7c2c8a2", 1},
		{"", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, containerdMajorVersion(tt.version), tt.version)
	}
}

func TestContainerdCRICPlugin(t *testing.T) {
	require.Equal(t, containerdCRICPluginV2, containerdCRICPlugin(nil))
	require.Equal(t, containerdCRICPluginV2, containerdCRICPlugin(&ContainerdInfo{Version: "containerd v2.3.6"}))
	require.Equal(t, containerdCRICPluginV1, containerdCRICPlugin(&ContainerdInfo{Version: "containerd v1.7.27"}))
}

func TestRenderRunscConfig(t *testing.T) {
	systemd := renderRunscConfig(true)
	require.Contains(t, systemd, `binary_name = "/usr/local/bin/runsc"`)
	require.Contains(t, systemd, "[runsc_config]")
	require.Contains(t, systemd, `systemd-cgroup = "true"`)

	noSystemd := renderRunscConfig(false)
	require.NotContains(t, noSystemd, "[runsc_config]")
}

func TestRenderContainerdGvisorRuntime(t *testing.T) {
	out := renderContainerdGvisorRuntime(containerdCRICPluginV2)

	require.Contains(t, out, gvisorConfigBeginMarker)
	require.Contains(t, out, gvisorConfigEndMarker)
	require.Contains(t, out, "[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runsc]")
	require.Contains(t, out, "runtime_type = 'io.containerd.runsc.v1'")
	require.Contains(t, out, "pod_annotations = ['dev.gvisor.*']")
	require.Contains(t, out, "TypeUrl = 'io.containerd.runsc.v1.options'")
	require.Contains(t, out, "ConfigPath = '/etc/containerd/runsc/config.toml'")

	v1 := renderContainerdGvisorRuntime(containerdCRICPluginV1)
	require.Contains(t, v1, "[plugins.'io.containerd.grpc.v1.cri'.containerd.runtimes.runsc]")
}

func TestVerifySHA512(t *testing.T) {
	const sum = "b55a3d22c8e01b8cb615dcf7b10022285f998f5ffcef787f8c0b4d9268435db2641f442cb6a0aa4e33574e5b4b446bce45ec5a81a2f2e3127520bb2753153bc6"

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "gvisor.tar.zstd")
	sumPath := archivePath + ".sha512"

	require.NoError(t, os.WriteFile(archivePath, []byte("hello gvisor"), 0644))
	require.NoError(t, os.WriteFile(sumPath, []byte(sum+"  gvisor.tar.zstd\n"), 0644))
	require.NoError(t, verifySHA512(archivePath, sumPath))

	require.NoError(t, os.WriteFile(archivePath, []byte("tampered"), 0644))
	require.Error(t, verifySHA512(archivePath, sumPath))

	require.NoError(t, os.WriteFile(sumPath, []byte("not-a-checksum\n"), 0644))
	require.Error(t, verifySHA512(archivePath, sumPath))

	require.NoError(t, os.WriteFile(sumPath, []byte(" \n"), 0644))
	require.Error(t, verifySHA512(archivePath, sumPath))
}

func TestGvisorInstalled(t *testing.T) {
	require.False(t, gvisorInstalled())
}
