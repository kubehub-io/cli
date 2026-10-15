package kubehubcli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSmolvmArch(t *testing.T) {
	arch, err := smolvmArch("amd64")
	require.NoError(t, err)
	require.Equal(t, "x86_64", arch)

	arch, err = smolvmArch("arm64")
	require.NoError(t, err)
	require.Equal(t, "arm64", arch)

	_, err = smolvmArch("riscv64")
	require.Error(t, err)
}

func TestSmolvmArchiveName(t *testing.T) {
	name, err := smolvmArchiveName("amd64")
	require.NoError(t, err)
	require.Equal(t, "smolvm-1.28.1-linux-x86_64.tar.gz", name)

	name, err = smolvmArchiveName("arm64")
	require.NoError(t, err)
	require.Equal(t, "smolvm-1.28.1-linux-arm64.tar.gz", name)

	_, err = smolvmArchiveName("riscv64")
	require.Error(t, err)
}

func TestParseSHA256Sum(t *testing.T) {
	manifest := "ef1147b798791b564b44bbaa69783df81bedcd151fe620a25be42b5290b51727  smolvm-1.28.1-darwin-arm64.tar.gz\n" +
		"02b4fb02e9eaf85e22e7e13eb26643db34bddd5e43e8d38a1e82b864da5914d5  smolvm-1.28.1-linux-arm64.tar.gz\n"

	sums := parseSHA256Sum(manifest)
	require.Len(t, sums, 2)
	require.Equal(t, "02b4fb02e9eaf85e22e7e13eb26643db34bddd5e43e8d38a1e82b864da5914d5", sums["smolvm-1.28.1-linux-arm64.tar.gz"])
	require.NotContains(t, sums, "smolvm-1.28.1-linux-x86_64.tar.gz")

	// Binary mode marks the name with a leading asterisk.
	binary := parseSHA256Sum("aa  *smolvm-1.28.1-linux-x86_64.tar.gz\n")
	require.Contains(t, binary, "smolvm-1.28.1-linux-x86_64.tar.gz")

	require.Empty(t, parseSHA256Sum(""))
	require.Empty(t, parseSHA256Sum("garbage\n"))
}

func TestVerifySHA256(t *testing.T) {
	const sum = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

	dir := t.TempDir()
	path := filepath.Join(dir, "smolvm-1.28.1-linux-x86_64.tar.gz")

	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))
	require.NoError(t, verifySHA256(path, sum))

	require.NoError(t, os.WriteFile(path, []byte("tampered"), 0644))
	require.Error(t, verifySHA256(path, sum))

	require.Error(t, verifySHA256(path, "not-a-checksum"))
	require.Error(t, verifySHA256(filepath.Join(dir, "missing"), sum))
}

func TestRenderContainerdSmolvmRuntime(t *testing.T) {
	out := renderContainerdSmolvmRuntime(containerdCRICPluginV2)

	require.Contains(t, out, smolvmConfigBeginMarker)
	require.Contains(t, out, smolvmConfigEndMarker)
	require.Contains(t, out, "[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.smolvm]")
	require.Contains(t, out, "runtime_type = 'io.containerd.smolvm.v2'")
	require.Contains(t, out, "sandboxer = 'podsandbox'")

	v1 := renderContainerdSmolvmRuntime(containerdCRICPluginV1)
	require.Contains(t, v1, "[plugins.'io.containerd.grpc.v1.cri'.containerd.runtimes.smolvm]")
}

func TestSmolvmInstalled(t *testing.T) {
	require.False(t, smolvmDistInstalled())
	require.False(t, smolvmShimInstalled())
}
