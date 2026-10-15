package kubehubcli

const (
	ContainerdVersion     = "2.3.6"
	RuncVersion           = "1.2.6"
	CrictlVersion         = "v1.33.0"
	ClusterDomain         = "cluster.local"
	StaticPodPath         = "/etc/kubernetes/manifests"
	KubeletConfigPath     = "/var/lib/kubelet/config.yaml"
	KubeletKubeconfigPath = "/var/lib/kubelet/kubeconfig"
	PKIPath               = "/etc/kubernetes/pki"
	KubeletPKIPath        = "/var/lib/kubelet/pki"
	ContainerdConfigPath  = "/etc/containerd/config.toml"

	ContainerdConfigBackupPath = ContainerdConfigPath + ".kubehubcli.bak"

	// GvisorReleaseURLBase is the gVisor release bucket. The path suffix is the
	// gVisor architecture name, see gvisorArch.
	GvisorReleaseURLBase = "https://storage.googleapis.com/gvisor/releases/release/latest"

	// GvisorBinDir is where the gVisor release tarball is unpacked to. runsc
	// resolves gvisor-bin/ relative to its own binary, so these must stay
	// together.
	GvisorBinDir = "/usr/local/bin"

	RunscBinaryPath         = GvisorBinDir + "/runsc"
	RunscShimBinaryPath     = GvisorBinDir + "/containerd-shim-runsc-v1"
	RunscSentryBinaryPath   = GvisorBinDir + "/gvisor-bin/gvisor_sentry"
	RunscConfigPath         = "/etc/containerd/runsc/config.toml"
	RunscStateDir           = "/run/containerd/runsc"
	GvisorRuntimeHandler    = "runsc"
	GvisorRuntimeType       = "io.containerd.runsc.v1"
	GvisorRuntimeOptionsURL = "io.containerd.runsc.v1.options"

	// SmolvmVersion is the smolvm release kubehubcli installs.
	SmolvmVersion = "1.28.1"

	// SmolvmReleaseURLBase holds the smolvm release downloads. The checksum
	// manifest for every archive of a release is published next to them under
	// the same tag.
	SmolvmReleaseURLBase = "https://github.com/smol-machines/smolvm/releases/download"

	// SmolvmDir is where the smolvm distribution is unpacked. The `smolvm`
	// wrapper resolves smolvm-bin, lib/ and agent-rootfs/ relative to its own
	// location, so the whole distribution has to stay together.
	SmolvmDir = "/opt/smolvm"

	// SmolvmBinaryPath is the wrapper linked onto PATH. It is the supported
	// entry point, it sets the library path smolvm-bin needs.
	SmolvmBinaryPath = "/usr/local/bin/smolvm"

	SmolvmShimBinaryPath = "/usr/local/bin/containerd-shim-smolvm-v2"

	// SmolvmDataDir is where install-k8s-runtime.sh lays down the artifacts the
	// shim loads at runtime.
	SmolvmDataDir = "/var/lib/smolvm"

	SmolvmRuntimeHandler = "smolvm"
	SmolvmRuntimeType    = "io.containerd.smolvm.v2"
	SmolvmSandboxer      = "podsandbox"

	// containerd CRI plugin names differ between containerd 1.x and 2.x.
	containerdCRICPluginV1 = "io.containerd.grpc.v1.cri"
	containerdCRICPluginV2 = "io.containerd.cri.v1.runtime"

	// Markers delimiting the blocks kubehubcli manages in containerdConfigPath.
	gvisorConfigBeginMarker = "# BEGIN kubehubcli gvisor"
	gvisorConfigEndMarker   = "# END kubehubcli gvisor"
	gvisorConfigRangeRegex  = "/^# BEGIN kubehubcli gvisor$/,/^# END kubehubcli gvisor$/d"

	smolvmConfigBeginMarker = "# BEGIN kubehubcli smolvm"
	smolvmConfigEndMarker   = "# END kubehubcli smolvm"
	smolvmConfigRangeRegex  = "/^# BEGIN kubehubcli smolvm$/,/^# END kubehubcli smolvm$/d"
)
