package kubehubcli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// smolvmArch maps the Go architecture onto the architecture name used by the
// smolvm release assets.
func smolvmArch(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("smolvm does not publish builds for %s", goarch)
	}
}

// smolvmArchiveName is the name of the release asset holding the Linux
// distribution, e.g. smolvm-1.28.1-linux-x86_64.tar.gz.
func smolvmArchiveName(goarch string) (string, error) {
	arch, err := smolvmArch(goarch)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("smolvm-%s-linux-%s.tar.gz", SmolvmVersion, arch), nil
}

// smolvmDistInstalled reports whether the smolvm distribution is unpacked and
// the wrapper is linked onto PATH.
func smolvmDistInstalled() bool {
	for _, path := range []string{
		SmolvmBinaryPath,
		filepath.Join(SmolvmDir, "smolvm-bin"),
		filepath.Join(SmolvmDir, "lib", "libkrun.so"),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

// smolvmShimInstalled reports whether the containerd shim and the runtime
// artifacts install-k8s-runtime.sh copies out of the distribution are in place.
func smolvmShimInstalled() bool {
	for _, path := range []string{
		SmolvmShimBinaryPath,
		filepath.Join(SmolvmDataDir, "lib", "libkrun.so"),
		filepath.Join(SmolvmDataDir, "agent-rootfs"),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

// installSmolvm downloads the smolvm release and unpacks the distribution into
// SmolvmDir, linking the wrapper onto PATH.
// See https://github.com/smol-machines/smolvm#install
func installSmolvm() error {
	slog.Info("--- Installing smolvm ---")

	if smolvmDistInstalled() {
		slog.Info("smolvm already installed, skipping installation")
		return nil
	}

	archive, err := smolvmArchiveName(runtime.GOARCH)
	if err != nil {
		return err
	}

	archivePath := filepath.Join("/tmp", archive)
	sumsPath := archivePath + ".sha256"
	stagingDir := filepath.Join("/tmp", "smolvm-"+SmolvmVersion)

	baseURL := fmt.Sprintf("%s/v%s", SmolvmReleaseURLBase, SmolvmVersion)

	slog.Info(fmt.Sprintf("Downloading %s from %s", archive, baseURL))

	if err := RunCmdCapture("curl", "-fsSL", baseURL+"/"+archive, "-o", archivePath); err != nil {
		return fmt.Errorf("download smolvm: %w", err)
	}

	if err := RunCmdCapture("curl", "-fsSL", baseURL+"/checksums.sha256", "-o", sumsPath); err != nil {
		return fmt.Errorf("download smolvm checksums: %w", err)
	}

	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		return fmt.Errorf("read smolvm checksums: %w", err)
	}

	want, ok := parseSHA256Sum(string(raw))[archive]
	if !ok {
		return fmt.Errorf("no sha256 for %s in the v%s checksum manifest", archive, SmolvmVersion)
	}

	if err := verifySHA256(archivePath, want); err != nil {
		return fmt.Errorf("verify smolvm archive: %w", err)
	}

	// Unpack into a staging directory and swap it in, so an interrupted run
	// cannot leave a half-written distribution behind. The archive carries a
	// single smolvm-<version>-linux-<arch>/ top level directory to strip.
	if err := RunCmd("mkdir", "-p", stagingDir); err != nil {
		return fmt.Errorf("create %s: %w", stagingDir, err)
	}

	if err := RunCmdCapture("tar", "-xzf", archivePath, "-C", stagingDir, "--strip-components=1"); err != nil {
		return fmt.Errorf("extract smolvm: %w", err)
	}

	// The wrapper and the shim are executed straight out of the distribution,
	// so make sure they kept their exec bits.
	if err := RunCmdCapture("chmod", "-R", "u+rwX,go+rX", stagingDir); err != nil {
		return fmt.Errorf("chmod %s: %w", stagingDir, err)
	}

	if err := RunCmd("rm", "-rf", SmolvmDir); err != nil {
		return fmt.Errorf("remove %s: %w", SmolvmDir, err)
	}

	if err := RunCmd("mv", stagingDir, SmolvmDir); err != nil {
		return fmt.Errorf("install %s: %w", SmolvmDir, err)
	}

	// The wrapper, not smolvm-bin, is the supported entry point: it puts
	// lib/ on the library path and points smolvm at the bundled agent rootfs.
	if err := RunCmd("ln", "-sfn", filepath.Join(SmolvmDir, "smolvm"), SmolvmBinaryPath); err != nil {
		return fmt.Errorf("link %s: %w", SmolvmBinaryPath, err)
	}

	for _, path := range []string{archivePath, sumsPath} {
		if err := os.Remove(path); err != nil {
			slog.Warn(fmt.Sprintf("warning: remove %s: %v", path, err))
		}
	}

	output, err := exec.Command(SmolvmBinaryPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify smolvm: %w", err)
	}
	slog.Info(fmt.Sprintf("smolvm installed: %s", strings.TrimSpace(string(output))))

	return nil
}

// parseSHA256Sum reads a `sha256sum` manifest into a file name to digest map.
// The binary-mode marker in front of a name is ignored.
func parseSHA256Sum(manifest string) map[string]string {
	sums := map[string]string{}

	for _, line := range strings.Split(manifest, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}

	return sums
}

// verifySHA256 checks the file at path against want, a hex sha256 digest.
func verifySHA256(path, want string) error {
	if len(want) != sha256.Size*2 {
		return fmt.Errorf("unexpected checksum %q", want)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", path, got, want)
	}

	return nil
}

// installSmolvmShim installs the containerd shim and the runtime artifacts it
// loads with the script the release ships. The script deliberately leaves
// /etc/containerd/config.toml to the operator, configureContainerdSmolvm
// writes that part.
// See https://github.com/smol-machines/smolvm/blob/main/docs/kubernetes.md
func installSmolvmShim() error {
	if smolvmShimInstalled() {
		slog.Info("smolvm containerd shim already installed, skipping installation")
		return nil
	}

	if _, err := os.Stat("/dev/kvm"); err != nil {
		slog.Warn("warning: /dev/kvm is missing, pods using the smolvm runtime will not start")
	}

	script := filepath.Join(SmolvmDir, "kubernetes", "install-k8s-runtime.sh")

	if err := RunCmd(script,
		"--shim", filepath.Join(SmolvmDir, "containerd-shim-smolvm-v2"),
		"--runtime-dir", SmolvmDir,
	); err != nil {
		return fmt.Errorf("install smolvm shim: %w", err)
	}

	return nil
}

// configureContainerdSmolvm registers the smolvm runtime handler with
// containerd. The block matches the one install-k8s-runtime.sh prints.
func configureContainerdSmolvm(info *HostInfo) error {
	slog.Info("--- Configuring smolvm runtime in containerd ---")

	plugin := containerdCRICPlugin(info.Containerd)

	if err := backupContainerdConfig(); err != nil {
		return err
	}

	// Drop a previously managed block so re-running join stays idempotent.
	if err := RunCmdCapture("sed", "-i", smolvmConfigRangeRegex, ContainerdConfigPath); err != nil {
		return fmt.Errorf("remove previous smolvm config: %w", err)
	}

	section := renderContainerdSmolvmRuntime(plugin)
	if err := appendFileAsRoot(ContainerdConfigPath, []byte(section)); err != nil {
		return fmt.Errorf("append smolvm config: %w", err)
	}

	slog.Info(fmt.Sprintf("containerd runtime handler %q configured via %s", SmolvmRuntimeHandler, plugin))

	return nil
}

// renderContainerdSmolvmRuntime builds the containerd config block that makes
// the smolvm shim available to Kubernetes through the RuntimeClass handler.
// smolvm ships a shim v2, so the CRI plugin launches it as the sandboxer.
func renderContainerdSmolvmRuntime(plugin string) string {
	runtimePath := fmt.Sprintf("plugins.'%s'.containerd.runtimes.%s", plugin, SmolvmRuntimeHandler)

	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(smolvmConfigBeginMarker + "\n")
	b.WriteString("# smolvm microVM sandbox runtime, see https://github.com/smol-machines/smolvm/blob/main/docs/kubernetes.md\n")
	fmt.Fprintf(&b, "[%s]\n", runtimePath)
	fmt.Fprintf(&b, "  runtime_type = '%s'\n", SmolvmRuntimeType)
	fmt.Fprintf(&b, "  sandboxer = '%s'\n", SmolvmSandboxer)
	b.WriteString(smolvmConfigEndMarker + "\n")

	return b.String()
}

// installSmolvmRuntime installs smolvm, its containerd shim and registers the
// smolvm runtime handler. It does not restart containerd; callers that need the
// change live must call ensureContainerd afterwards.
func installSmolvmRuntime(info *HostInfo) error {
	if err := installSmolvm(); err != nil {
		return fmt.Errorf("install smolvm: %w", err)
	}

	if err := installSmolvmShim(); err != nil {
		return err
	}

	if err := configureContainerdSmolvm(info); err != nil {
		return fmt.Errorf("configure smolvm: %w", err)
	}

	return nil
}

// InstallSmolvm installs smolvm on this host and registers the smolvm runtime
// handler with containerd, restarting containerd so the handler is live. It is
// the day-2 entry point used when a node is already joined to a cluster.
func InstallSmolvm() error {
	info, err := DetectHost()
	if err != nil {
		return fmt.Errorf("detect host: %w", err)
	}

	if info.Containerd == nil {
		return fmt.Errorf("containerd is not installed on this host, cannot register the smolvm runtime handler")
	}

	if err := installSmolvmRuntime(info); err != nil {
		return err
	}

	if err := ensureContainerd(); err != nil {
		return fmt.Errorf("ensure containerd: %w", err)
	}

	return nil
}

// SmolvmRuntimeClassHint logs how to opt workloads into the smolvm runtime.
// Callers emit it at the end of a successful run so it lands directly above
// that command's completion message.
func SmolvmRuntimeClassHint() {
	slog.Info("smolvm is installed. Enable it per workload with a RuntimeClass:")
	slog.Info(fmt.Sprintf("  handler: %s, runtimeClassName: %s", SmolvmRuntimeHandler, SmolvmRuntimeHandler))
	slog.Info("  restrict it to nodes that have it: kubectl label node <node> smolvm-runtime=true")
}
