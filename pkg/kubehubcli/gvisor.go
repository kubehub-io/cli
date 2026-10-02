package kubehubcli

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var containerdVersionRe = regexp.MustCompile(`\bv(\d+)\.(\d+)`)

// gvisorArch maps the Go architecture onto the architecture name used by the
// gVisor release bucket.
func gvisorArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("gVisor does not publish builds for %s", goarch)
	}
}

// containerdMajorVersion extracts the major version from the output of
// `containerd --version`, e.g. "containerd github.com/containerd/containerd
// v2.3.6 <sha>". It returns 0 when the version cannot be determined.
func containerdMajorVersion(version string) int {
	m := containerdVersionRe.FindStringSubmatch(version)
	if m == nil {
		return 0
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return major
}

// containerdCRICPlugin returns the containerd CRI plugin name for the detected
// containerd. containerd 2.x moved the runtime tables under the new CRI plugin.
func containerdCRICPlugin(info *ContainerdInfo) string {
	if info != nil && containerdMajorVersion(info.Version) == 1 {
		return containerdCRICPluginV1
	}
	return containerdCRICPluginV2
}

// gvisorInstalled reports whether runsc, the containerd shim and the gVisor
// sidecars are all present.
func gvisorInstalled() bool {
	for _, path := range []string{RunscBinaryPath, RunscShimBinaryPath, RunscSentryBinaryPath} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

// installGvisor downloads the latest gVisor release and installs runsc, the
// containerd shim and the gvisor-bin/ sidecars into /usr/local/bin.
// See https://gvisor.dev/docs/user_guide/install/
func installGvisor() error {
	slog.Info("--- Installing gVisor ---")

	if gvisorInstalled() {
		slog.Info("gVisor already installed, skipping installation")
		return nil
	}

	arch, err := gvisorArch(runtime.GOARCH)
	if err != nil {
		return err
	}

	archive := "gvisor.tar.zstd"
	tarFilter := "--zstd"
	if _, err := exec.LookPath("zstd"); err != nil {
		slog.Warn("warning: zstd not found, falling back to gvisor.tar.bz2")
		archive = "gvisor.tar.bz2"
		tarFilter = "-j"
	}

	baseURL := fmt.Sprintf("%s/%s", GvisorReleaseURLBase, arch)
	archivePath := filepath.Join("/tmp", archive)
	checksumPath := archivePath + ".sha512"

	slog.Info(fmt.Sprintf("Downloading %s from %s", archive, baseURL))

	if err := RunCmdCapture("curl", "-fsSL", baseURL+"/"+archive, "-o", archivePath); err != nil {
		return fmt.Errorf("download gvisor: %w", err)
	}

	if err := RunCmdCapture("curl", "-fsSL", baseURL+"/"+archive+".sha512", "-o", checksumPath); err != nil {
		return fmt.Errorf("download gvisor checksum: %w", err)
	}

	if err := verifySHA512(archivePath, checksumPath); err != nil {
		return fmt.Errorf("verify gvisor archive: %w", err)
	}

	if err := RunCmd("mkdir", "-p", GvisorBinDir); err != nil {
		return fmt.Errorf("create %s: %w", GvisorBinDir, err)
	}

	// The tarball unpacks runsc, containerd-shim-runsc-v1 and gvisor-bin/
	// sidecars at the top level. They have to land in the same directory:
	// runsc resolves gvisor-bin/ relative to its own binary.
	if err := RunCmdCapture("tar", tarFilter, "-xf", archivePath, "-C", GvisorBinDir); err != nil {
		return fmt.Errorf("extract gvisor: %w", err)
	}

	for _, path := range []string{RunscBinaryPath, RunscShimBinaryPath, filepath.Join(GvisorBinDir, "gvisor-bin")} {
		if err := RunCmdCapture("chmod", "-R", "0755", path); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
	}

	for _, path := range []string{archivePath, checksumPath} {
		if err := os.Remove(path); err != nil {
			slog.Warn(fmt.Sprintf("warning: remove %s: %v", path, err))
		}
	}

	output, err := exec.Command(RunscBinaryPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify runsc: %w", err)
	}
	slog.Info(fmt.Sprintf("gVisor installed: %s", strings.TrimSpace(string(output))))

	return nil
}

// verifySHA512 checks the file at archivePath against the checksum published in
// the `sha512sum` formatted file at sumPath.
func verifySHA512(archivePath, sumPath string) error {
	raw, err := os.ReadFile(sumPath)
	if err != nil {
		return fmt.Errorf("read checksum: %w", err)
	}

	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return fmt.Errorf("empty checksum file %s", sumPath)
	}

	want := fields[0]
	if len(want) != sha512.Size*2 {
		return fmt.Errorf("unexpected checksum %q in %s", want, sumPath)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha512 mismatch for %s: got %s, want %s", archivePath, got, want)
	}

	return nil
}

// installGvisorRuntime installs gVisor and registers the runsc runtime handler
// with containerd. It does not restart containerd; callers that need the change
// live must call ensureContainerd afterwards.
func installGvisorRuntime(info *HostInfo) error {
	if err := installGvisor(); err != nil {
		return fmt.Errorf("install gvisor: %w", err)
	}

	if err := configureContainerdGvisor(info); err != nil {
		return fmt.Errorf("configure gvisor: %w", err)
	}

	return nil
}

// InstallGvisor installs gVisor on this host and registers the runsc runtime
// handler with containerd, restarting containerd so the handler is live. It is
// the day-2 entry point used when a node is already joined to a cluster.
func InstallGvisor() error {
	info, err := DetectHost()
	if err != nil {
		return fmt.Errorf("detect host: %w", err)
	}

	if info.Containerd == nil {
		return fmt.Errorf("containerd is not installed on this host, cannot register the runsc runtime handler")
	}

	if err := installGvisorRuntime(info); err != nil {
		return err
	}

	if err := ensureContainerd(); err != nil {
		return fmt.Errorf("ensure containerd: %w", err)
	}

	slog.Info(fmt.Sprintf("gVisor runtime handler %q is registered, enable it per workload with a RuntimeClass", GvisorRuntimeHandler))

	return nil
}

// configureContainerdGvisor registers the runsc runtime handler with containerd
// and writes the runsc shim configuration it points at.
// See https://gvisor.dev/docs/user_guide/containerd/quick_start/
func configureContainerdGvisor(info *HostInfo) error {
	slog.Info("--- Configuring gVisor runtime in containerd ---")

	plugin := containerdCRICPlugin(info.Containerd)

	if err := RunCmd("mkdir", "-p", filepath.Dir(RunscConfigPath)); err != nil {
		return fmt.Errorf("create runsc config dir: %w", err)
	}

	if err := writeFileAsRoot(RunscConfigPath, []byte(renderRunscConfig(IsSystemdManaged())), 0644); err != nil {
		return fmt.Errorf("write runsc config: %w", err)
	}
	slog.Info(fmt.Sprintf("runsc configuration written to %s", RunscConfigPath))

	if err := backupContainerdConfig(); err != nil {
		return err
	}

	// Drop a previously managed block so re-running join stays idempotent.
	if err := RunCmdCapture("sed", "-i", gvisorConfigRangeRegex, ContainerdConfigPath); err != nil {
		return fmt.Errorf("remove previous gvisor config: %w", err)
	}

	section := renderContainerdGvisorRuntime(plugin)
	if err := appendFileAsRoot(ContainerdConfigPath, []byte(section)); err != nil {
		return fmt.Errorf("append gvisor config: %w", err)
	}

	slog.Info(fmt.Sprintf("containerd runtime handler %q configured via %s", GvisorRuntimeHandler, plugin))

	return nil
}

// renderRunscConfig builds the shim configuration read by
// containerd-shim-runsc-v1. Values under [runsc_config] are passed to runsc as
// --flag=value, see https://gvisor.dev/docs/user_guide/containerd/configuration/
func renderRunscConfig(systemdManaged bool) string {
	var b strings.Builder

	b.WriteString("# Managed by kubehubcli, do not edit.\n")
	fmt.Fprintf(&b, "binary_name = %q\n", RunscBinaryPath)
	fmt.Fprintf(&b, "root = %q\n", RunscStateDir)
	b.WriteString("log_level = \"info\"\n")

	if systemdManaged {
		b.WriteString("\n[runsc_config]\n  systemd-cgroup = \"true\"\n")
	}

	return b.String()
}

// renderContainerdGvisorRuntime builds the containerd config block that makes
// runsc available to Kubernetes through the RuntimeClass handler.
func renderContainerdGvisorRuntime(plugin string) string {
	runtimePath := fmt.Sprintf("plugins.'%s'.containerd.runtimes.%s", plugin, GvisorRuntimeHandler)

	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(gvisorConfigBeginMarker + "\n")
	b.WriteString("# gVisor sandbox runtime, see https://gvisor.dev/docs/user_guide/containerd/quick_start/\n")
	fmt.Fprintf(&b, "[%s]\n", runtimePath)
	fmt.Fprintf(&b, "  runtime_type = '%s'\n", GvisorRuntimeType)
	b.WriteString("  pod_annotations = ['dev.gvisor.*']\n")
	fmt.Fprintf(&b, "  [%s.options]\n", runtimePath)
	fmt.Fprintf(&b, "    TypeUrl = '%s'\n", GvisorRuntimeOptionsURL)
	fmt.Fprintf(&b, "    ConfigPath = '%s'\n", RunscConfigPath)
	b.WriteString(gvisorConfigEndMarker + "\n")

	return b.String()
}

// backupContainerdConfig keeps the pristine containerd config around. The
// backup is only created once so repeated joins cannot overwrite the original.
func backupContainerdConfig() error {
	if _, err := os.Stat(ContainerdConfigBackupPath); err == nil {
		return nil
	}

	if err := RunCmdCapture("cp", "-a", ContainerdConfigPath, ContainerdConfigBackupPath); err != nil {
		return fmt.Errorf("backup %s: %w", ContainerdConfigPath, err)
	}

	slog.Info(fmt.Sprintf("containerd config backed up to %s", ContainerdConfigBackupPath))
	return nil
}
