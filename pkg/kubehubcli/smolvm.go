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
// artifacts it loads are in place.
func smolvmShimInstalled() bool {
	paths := []string{SmolvmShimBinaryPath}

	for _, artifact := range smolvmRuntimeArtifacts {
		paths = append(paths, filepath.Join(SmolvmDataDir, artifact))
	}

	// The artifacts only count as installed once the libraries the engine
	// dlopen()s are actually in the copied tree.
	for _, lib := range smolvmRuntimeLibraries {
		paths = append(paths, filepath.Join(SmolvmDataDir, "lib", lib))
	}

	for _, path := range paths {
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

// smolvmRuntimeArtifacts are the pieces of the distribution the shim loads at
// pod-sandbox creation, copied out of SmolvmDir into SmolvmDataDir. libkrun
// (and libkrunfw, which the guest kernel needs) are dlopen'ed by the shim and
// by the boot process it spawns, agent-rootfs is the guest image the agent
// runs in. Nothing boots without them.
var smolvmRuntimeArtifacts = []string{"lib", "agent-rootfs"}

// smolvmRuntimeLibraries are the libraries the engine dlopen()s out of
// smolvmRuntimeArtifacts' lib directory.
var smolvmRuntimeLibraries = []string{"libkrun.so", "libkrunfw.so"}

// installSmolvmShim installs the containerd shim and the runtime artifacts it
// loads, then points containerd's environment at them. The release ships a
// shell installer for the same job, but it only prints the containerd
// registration instead of applying it, so the install is done here.
// See https://github.com/smol-machines/smolvm/blob/main/docs/kubernetes.md
func installSmolvmShim() error {
	if smolvmShimInstalled() {
		slog.Info("smolvm containerd shim already installed, skipping installation")

		// The drop-in is cheap to rewrite and is the only thing tying the
		// artifacts to containerd, so refresh it even on the skip path: it is
		// what makes a re-run recover from a dropped-in uninstall.
		return configureSmolvmShimEnv()
	}

	if _, err := os.Stat("/dev/kvm"); err != nil {
		slog.Warn("warning: /dev/kvm is missing, pods using the smolvm runtime will not start")
	}

	shimSource := filepath.Join(SmolvmDir, "containerd-shim-smolvm-v2")

	if _, err := os.Stat(shimSource); err != nil {
		return fmt.Errorf("no containerd shim at %s: %w", shimSource, err)
	}

	for _, artifact := range smolvmRuntimeArtifacts {
		if _, err := os.Stat(filepath.Join(SmolvmDir, artifact)); err != nil {
			return fmt.Errorf("no %s in the smolvm distribution at %s: %w", artifact, SmolvmDir, err)
		}
	}

	if err := RunCmd("mkdir", "-p", SmolvmDataDir); err != nil {
		return fmt.Errorf("create %s: %w", SmolvmDataDir, err)
	}

	// cp -a, not a plain copy: agent-rootfs is mostly symlinks into /bin and its
	// entries carry the modes the guest expects, so it has to be reproduced
	// as-is rather than dereferenced.
	for _, artifact := range smolvmRuntimeArtifacts {
		source := filepath.Join(SmolvmDir, artifact)
		target := filepath.Join(SmolvmDataDir, artifact)

		if err := RunCmdCapture("rm", "-rf", target); err != nil {
			return fmt.Errorf("remove %s: %w", target, err)
		}

		if err := RunCmdCapture("cp", "-a", source, target); err != nil {
			return fmt.Errorf("copy %s: %w", source, err)
		}

		slog.Info(fmt.Sprintf("%s installed to %s", artifact, target))
	}

	// Write beside the destination and rename over it. containerd can still have
	// the shim mapped from a running pod, and overwriting it in place fails on
	// that; a rename swaps the file atomically instead.
	if err := RunCmdCapture("install", "-Dm755", shimSource, SmolvmShimBinaryPath+".tmp"); err != nil {
		return fmt.Errorf("install %s: %w", SmolvmShimBinaryPath, err)
	}

	if err := RunCmdCapture("mv", "-f", SmolvmShimBinaryPath+".tmp", SmolvmShimBinaryPath); err != nil {
		return fmt.Errorf("install %s: %w", SmolvmShimBinaryPath, err)
	}

	slog.Info(fmt.Sprintf("smolvm containerd shim installed to %s", SmolvmShimBinaryPath))

	return configureSmolvmShimEnv()
}

// configureSmolvmShimEnv writes the systemd drop-in that exports the runtime
// artifact paths to containerd and, through it, to the shim. The shim lives in
// /usr/local/bin, which is none of the places the engine looks for libkrun or
// the guest rootfs on its own, so without this every pod sandbox fails to boot.
func configureSmolvmShimEnv() error {
	if !IsSystemdManaged() {
		slog.Warn(fmt.Sprintf("warning: systemd not detected, set SMOLVM_AGENT_ROOTFS=%s and SMOLVM_LIB_DIR=%s in the environment containerd runs in",
			filepath.Join(SmolvmDataDir, "agent-rootfs"), filepath.Join(SmolvmDataDir, "lib")))

		return nil
	}

	if err := RunCmd("mkdir", "-p", filepath.Dir(smolvmDropInPath)); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(smolvmDropInPath), err)
	}

	// Rewritten whole every run: it is kubehubcli's own file, and the only way
	// a re-run can converge is by replacing whatever is there.
	if err := writeFileAsRoot(smolvmDropInPath, []byte(renderContainerdSmolvmEnv()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", smolvmDropInPath, err)
	}

	// The restart that follows this picks the drop-in up.
	if err := RunCmdCapture("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemd daemon-reload: %w", err)
	}

	// Read the unit back rather than trusting the path. systemd merges the .d
	// directories across the whole unit load path, so seeing our file in
	// `systemctl cat` is what proves the drop-in applies here, whether
	// containerd.service itself came from /usr/lib, /usr/local/lib or /run.
	out, err := runCmdOutput("systemctl", "cat", "containerd")
	if err != nil {
		return fmt.Errorf("containerd is not a systemd unit, set SMOLVM_AGENT_ROOTFS=%s and SMOLVM_LIB_DIR=%s in the environment it runs in: %w",
			filepath.Join(SmolvmDataDir, "agent-rootfs"), filepath.Join(SmolvmDataDir, "lib"), err)
	}

	if !strings.Contains(out, smolvmDropInPath) {
		return fmt.Errorf("%s is not part of the containerd unit, the shim will not find the runtime artifacts", smolvmDropInPath)
	}

	slog.Info(fmt.Sprintf("smolvm runtime environment written to %s", smolvmDropInPath))

	return nil
}

// renderContainerdSmolvmEnv builds the systemd drop-in telling the smolvm
// engine where its runtime artifacts live. containerd execs the shim rather
// than loading it as a library, so the shim inherits this environment.
func renderContainerdSmolvmEnv() string {
	var b strings.Builder

	b.WriteString("# Managed by kubehubcli, do not edit.\n")
	b.WriteString("# Read by the smolvm engine at pod-sandbox creation.\n")
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "Environment=SMOLVM_AGENT_ROOTFS=%s\n", filepath.Join(SmolvmDataDir, "agent-rootfs"))
	fmt.Fprintf(&b, "Environment=SMOLVM_LIB_DIR=%s\n", filepath.Join(SmolvmDataDir, "lib"))

	return b.String()
}

// configureContainerdSmolvm registers the smolvm runtime handler with
// containerd. The block matches the one the release's installer prints.
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
