package kubehubcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/gogrlx/snack"
	"github.com/gogrlx/snack/detect"
	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
)

func RunCmd(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{name}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func RunCmdCapture(name string, args ...string) error {
	cmd := exec.Command("sudo", append([]string{name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w, output: %s", name, err, string(out))
	}
	return nil
}

func writeFileAsRoot(path string, data []byte, perm os.FileMode) error {
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := RunCmdCapture("chmod", fmt.Sprintf("%o", perm), path); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

type JoinOptions struct {
	ClusterName   string
	NodeIP        string
	OIDCIssuerURL string
	OIDCClientID  string
	ServerURL     string
	WaitMessage   string
	Verbose       bool
	Labels        map[string]string
	Annotations   map[string]string
}

func JoinCluster(opts *JoinOptions) error {
	ctx := context.Background()

	auth := NewAuthenticator(opts.OIDCIssuerURL, opts.OIDCClientID).WithVerbose(opts.Verbose)
	token, err := auth.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("authentication: %w", err)
	}

	client, err := v202607.NewClient(opts.ServerURL)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	authHeader := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	slog.Info("=== Checking Cluster ===")
	clusterResp, err := client.GetCluster(ctx, opts.ClusterName, authHeader)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}
	if clusterResp.StatusCode != http.StatusOK {
		errMsg := v202607.ParseErrorResponse(clusterResp)
		clusterResp.Body.Close()
		return fmt.Errorf("cluster %s not found: %s", opts.ClusterName, errMsg)
	}
	var clusterInfo v202607.Cluster
	if err := json.NewDecoder(clusterResp.Body).Decode(&clusterInfo); err != nil {
		clusterResp.Body.Close()
		return fmt.Errorf("decode cluster response: %w", err)
	}
	clusterResp.Body.Close()
	slog.Info(fmt.Sprintf("Cluster %s found", opts.ClusterName))

	return joinNodeToCluster(client, authHeader, opts, &clusterInfo)
}

func JoinClusterWithCluster(opts *JoinOptions, clusterInfo *v202607.Cluster) error {
	auth := NewAuthenticator(opts.OIDCIssuerURL, opts.OIDCClientID).WithVerbose(opts.Verbose)
	token, err := auth.Authenticate(context.Background())
	if err != nil {
		return fmt.Errorf("authentication: %w", err)
	}

	client, err := v202607.NewClient(opts.ServerURL)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	authHeader := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	return joinNodeToCluster(client, authHeader, opts, clusterInfo)
}

func joinNodeToCluster(client *v202607.Client, authHeader v202607.RequestEditorFn, opts *JoinOptions, clusterInfo *v202607.Cluster) error {
	slog.Info("=== Joining Cluster ===")
	slog.Info(fmt.Sprintf("Cluster: %s", opts.ClusterName))

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}
	slog.Info(fmt.Sprintf("Hostname: %s", hostname))

	info, err := DetectHost()
	if err != nil {
		return fmt.Errorf("detect host: %w", err)
	}

	if info.Kubelet != nil && info.Kubelet.Bootstrap == "yes" {
		return fmt.Errorf("host already joined to a cluster. Clean up first")
	}

	ctx := context.Background()

	preflight, err := RunPreflightChecks(ctx, client, authHeader, opts, clusterInfo, info)
	if err != nil {
		slog.Warn(fmt.Sprintf("warning: preflight checks: %v", err))
	} else {
		_ = preflight
	}

	if err := checkExistingNodesNetwork(ctx, client, authHeader, opts, info); err != nil {
		return fmt.Errorf("join check: %w", err)
	}

	nodeIP := opts.NodeIP
	if nodeIP != "" {
		slog.Info(fmt.Sprintf("Validating IP: %s", nodeIP))
		if err := ValidateNodeIP(nodeIP); err != nil {
			return fmt.Errorf("node IP validation: %w", err)
		}
		slog.Info("IP validated successfully")
	} else if len(info.HostIPs) > 0 {
		nodeIP = info.HostIPs[0]
	}

	getResp, err := client.GetNode(ctx, opts.ClusterName, hostname, authHeader)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	if getResp.StatusCode == http.StatusOK {
		var existingNode v202607.Node
		if err := json.NewDecoder(getResp.Body).Decode(&existingNode); err != nil {
			getResp.Body.Close()
			return fmt.Errorf("decode existing node: %w", err)
		}
		getResp.Body.Close()

		var existingIP string
		if existingNode.Spec != nil && existingNode.Spec.Meta != nil && existingNode.Spec.Meta.Ipv4 != nil {
			existingIP = *existingNode.Spec.Meta.Ipv4
		}

		if existingIP != nodeIP {
			return fmt.Errorf("node %s already exists with IP %s, cannot join with IP %s.", hostname, existingIP, nodeIP)
		}

		slog.Info("Node already registered with matching IP, skipping registration")
	} else if getResp.StatusCode == http.StatusNotFound {
		getResp.Body.Close()

		nodeReq := v202607.Node{
			Metadata: &v202607.NodeMetadata{
				Name: &hostname,
			},
			Spec: &v202607.NodeSpec{
				Os:   &info.OS,
				Arch: &info.Arch,
				Meta: &v202607.NodeInfo{
					Ipv4:   &nodeIP,
					Labels: ptrMap(opts.Labels),
				},
				Annotations: ptrMap(opts.Annotations),
				Hardware:    hostHardwareToV202607(info.Hardware),
			},
		}

		createResp, err := client.CreateNode(ctx, opts.ClusterName, nodeReq, authHeader)
		if err != nil {
			return fmt.Errorf("create node: %w", err)
		}

		if createResp.StatusCode != http.StatusOK {
			errMsg := v202607.ParseErrorResponse(createResp)
			createResp.Body.Close()
			return fmt.Errorf("create node failed: %s", errMsg)
		}
		createResp.Body.Close()

		slog.Info("Node created successfully!")
	} else {
		errMsg := v202607.ParseErrorResponse(getResp)
		getResp.Body.Close()
		return fmt.Errorf("get node failed: %s", errMsg)
	}

	var bootstrapSecret v202607.BootstrapSecretResponse
	{
		const maxAttempts = 30
		var lastErr error
		waitMsg := opts.WaitMessage
		if waitMsg == "" {
			waitMsg = "Retrying bootstrap"
		}

		for attempt := 0; attempt < maxAttempts; attempt++ {
			if attempt > 0 {
				wait := time.Duration(attempt*2) * time.Second
				slog.Info(fmt.Sprintf("%s in %v (attempt %d/%d)...", waitMsg, wait, attempt+1, maxAttempts))
				time.Sleep(wait)
			}

			bootstrapResp, err := client.BootstrapNode(ctx, opts.ClusterName, hostname, authHeader)
			if err != nil {
				lastErr = fmt.Errorf("bootstrap node: %w", err)
				continue
			}

			if bootstrapResp.StatusCode != http.StatusOK {
				bootstrapResp.Body.Close()
				lastErr = fmt.Errorf("bootstrap failed: %s", v202607.ParseErrorResponse(bootstrapResp))
				continue
			}

			if err := json.NewDecoder(bootstrapResp.Body).Decode(&bootstrapSecret); err != nil {
				bootstrapResp.Body.Close()
				lastErr = fmt.Errorf("decode bootstrap response: %w", err)
				continue
			}
			bootstrapResp.Body.Close()

			lastErr = nil
			break
		}

		if lastErr != nil {
			return lastErr
		}
	}

	slog.Info("Node bootstrapped successfully!")
	slog.Info(fmt.Sprintf("Cluster DNS: %s", ptrStr(bootstrapSecret.ClusterDNS)))

	podCIDR, serviceCIDR, kubeDNSDefaultIP := resolveClusterCIDRs(clusterInfo)

	componentVersion := resolveClusterComponentVersion(clusterInfo)
	controllerImage := fmt.Sprintf("registry.k8s.io/kube-controller-manager:%s", componentVersion)
	schedulerImage := fmt.Sprintf("registry.k8s.io/kube-scheduler:%s", componentVersion)

	if err := checkConnectivity(ptrStr(bootstrapSecret.ClusterDNS)); err != nil {
		slog.Warn(fmt.Sprintf("warning: connectivity check failed: %v", err))
	}

	if err := storeCerts(bootstrapSecret); err != nil {
		return fmt.Errorf("store certs: %w", err)
	}

	if isLaptop() && promptConfirm("Configure lid switch to keep node running when lid is closed?") {
		if err := configureLogind(); err != nil {
			return fmt.Errorf("configure logind: %w", err)
		}
	}

	fmt.Print("Installing kubernetes node requires some system config tweaks, press Enter to proceed: ")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}

	if err := applySysctl(); err != nil {
		return fmt.Errorf("apply sysctl: %w", err)
	}

	if err := generateKubeletConfig(hostname, ptrStr(bootstrapSecret.ClusterDNS), kubeDNSDefaultIP); err != nil {
		return fmt.Errorf("generate kubelet config: %w", err)
	}

	if err := generateSchedulerKubeconfig(ptrStr(bootstrapSecret.ClusterDNS)); err != nil {
		return fmt.Errorf("generate scheduler kubeconfig: %w", err)
	}

	if err := generateControllerManagerKubeconfig(ptrStr(bootstrapSecret.ClusterDNS)); err != nil {
		return fmt.Errorf("generate controller-manager kubeconfig: %w", err)
	}

	if err := createStaticPods(podCIDR, serviceCIDR, controllerImage, schedulerImage); err != nil {
		return fmt.Errorf("create static pods: %w", err)
	}

	if info.Runc == nil {
		if err := installRunc(); err != nil {
			return fmt.Errorf("install runc: %w", err)
		}
	} else {
		slog.Info("runc already installed, skipping installation")
	}

	if info.Containerd == nil {
		if err := installContainerd(); err != nil {
			return fmt.Errorf("install containerd: %w", err)
		}
	} else {
		slog.Info("containerd already installed, skipping installation")
	}

	if err := configureContainerd(info); err != nil {
		return fmt.Errorf("configure containerd: %w", err)
	}

	if err := installIscsiadm(); err != nil {
		return fmt.Errorf("install iscsiadm: %w", err)
	}

	if err := installCrictl(); err != nil {
		return fmt.Errorf("install crictl: %w", err)
	}

	if err := ensureContainerd(); err != nil {
		return fmt.Errorf("ensure containerd: %w", err)
	}

	if err := installKubelet(componentVersion, opts.Verbose); err != nil {
		return fmt.Errorf("install kubelet: %w", err)
	}

	if err := installKubectl(componentVersion, opts.Verbose); err != nil {
		return fmt.Errorf("install kubectl: %w", err)
	}

	if err := enableKubelet(); err != nil {
		return fmt.Errorf("enable kubelet: %w", err)
	}

	slog.Info("=== Join Complete ===")
	slog.Info("Kubelet has been started. Check status with: systemctl status kubelet")
	return nil
}

func resolveClusterComponentVersion(clusterInfo *v202607.Cluster) string {
	if clusterInfo.Status != nil && clusterInfo.Status.ControlPlane != nil && clusterInfo.Status.ControlPlane.Components != nil {
		apiSrv := clusterInfo.Status.ControlPlane.Components.KubeApiserver
		if apiSrv != nil && apiSrv.Version != nil && *apiSrv.Version != "" {
			return *apiSrv.Version
		}
	}
	if clusterInfo.Status != nil && clusterInfo.Status.ControlPlaneComponents != nil {
		apiSrv := clusterInfo.Status.ControlPlaneComponents.KubeApiserver
		if apiSrv != nil && apiSrv.Version != nil && *apiSrv.Version != "" {
			return *apiSrv.Version
		}
	}
	return ""
}

func resolveClusterCIDRs(clusterInfo *v202607.Cluster) (podCIDR, svcCIDR, dnsIP string) {
	podCIDR = "10.10.0.0/16"
	svcCIDR = "10.20.0.0/16"
	dnsIP = "10.20.0.10"
	if clusterInfo.Spec != nil && clusterInfo.Spec.Network != nil {
		if clusterInfo.Spec.Network.PodCIDR != nil && *clusterInfo.Spec.Network.PodCIDR != "" {
			podCIDR = *clusterInfo.Spec.Network.PodCIDR
		}
		if clusterInfo.Spec.Network.ServiceCIDR != nil && *clusterInfo.Spec.Network.ServiceCIDR != "" {
			svcCIDR = *clusterInfo.Spec.Network.ServiceCIDR
		}
		if clusterInfo.Spec.Network.KubeDNSServiceIP != nil && *clusterInfo.Spec.Network.KubeDNSServiceIP != "" {
			dnsIP = *clusterInfo.Spec.Network.KubeDNSServiceIP
		}
	}
	return
}

func storeCerts(resp v202607.BootstrapSecretResponse) error {
	slog.Info("--- Storing certificates ---")

	if err := RunCmd("mkdir", "-p", PKIPath); err != nil {
		return err
	}
	if err := RunCmd("mkdir", "-p", KubeletPKIPath); err != nil {
		return err
	}

	if resp.CaCert != nil && *resp.CaCert != "" {
		data, err := base64.StdEncoding.DecodeString(*resp.CaCert)
		if err != nil {
			return fmt.Errorf("decode ca cert: %w", err)
		}
		if err := writeFileAsRoot(filepath.Join(KubeletPKIPath, "ca.crt"), data, 0644); err != nil {
			return fmt.Errorf("write ca.crt: %w", err)
		}
		if err := writeFileAsRoot(filepath.Join(PKIPath, "ca.crt"), data, 0644); err != nil {
			return fmt.Errorf("write ca.crt to pki: %w", err)
		}
	}

	if resp.CertPairs != nil {
		saveCertPair := func(name string, pair *v202607.CertPair) error {
			if pair == nil {
				return nil
			}
			if pair.TlsCrt != nil {
				data, err := base64.StdEncoding.DecodeString(*pair.TlsCrt)
				if err != nil {
					return fmt.Errorf("decode %s crt: %w", name, err)
				}
				crtPath := filepath.Join(KubeletPKIPath, name+".crt")
				if err := writeFileAsRoot(crtPath, data, 0644); err != nil {
					return fmt.Errorf("write %s.crt: %w", name, err)
				}
			}
			if pair.TlsKey != nil {
				data, err := base64.StdEncoding.DecodeString(*pair.TlsKey)
				if err != nil {
					return fmt.Errorf("decode %s key: %w", name, err)
				}
				keyPath := filepath.Join(KubeletPKIPath, name+".key")
				if err := writeFileAsRoot(keyPath, data, 0600); err != nil {
					return fmt.Errorf("write %s.key: %w", name, err)
				}
			}
			return nil
		}

		for _, pair := range []struct {
			name   string
			cert   *v202607.CertPair
			pkiDir bool
		}{
			{"kubelet", resp.CertPairs.Kubelet, false},
			{"kube-scheduler", resp.CertPairs.Scheduler, true},
			{"kube-controller-manager", resp.CertPairs.ControllerManager, true},
		} {
			if err := saveCertPair(pair.name, pair.cert); err != nil {
				return err
			}
			if pair.pkiDir {
				if pair.cert != nil {
					if err := copyFile(filepath.Join(KubeletPKIPath, pair.name+".crt"), filepath.Join(PKIPath, pair.name+".crt")); err != nil {
						return fmt.Errorf("copy cert: %w", err)
					}
					if err := copyFile(filepath.Join(KubeletPKIPath, pair.name+".key"), filepath.Join(PKIPath, pair.name+".key")); err != nil {
						return fmt.Errorf("copy key: %w", err)
					}
					if err := RunCmdCapture("chmod", "600", filepath.Join(PKIPath, pair.name+".key")); err != nil {
						return fmt.Errorf("chmod key: %w", err)
					}
				}
			}
		}
	}

	return nil
}

func applySysctl() error {
	slog.Info("--- Applying sysctl settings ---")

	data, err := NodeConfigs.ReadFile("configassets/98-kubernetes.conf")
	if err != nil {
		return fmt.Errorf("read sysctl config: %w", err)
	}

	if err := writeFileAsRoot("/etc/sysctl.d/98-kubernetes.conf", data, 0644); err != nil {
		return err
	}

	if err := RunCmdCapture("sysctl", "--system"); err != nil {
		return err
	}

	slog.Info("sysctl settings applied")
	return nil
}

func generateKubeletConfig(hostname, clusterFQDN, kubeDNSIP string) error {
	slog.Info("--- Generating kubelet config ---")

	if err := RunCmd("mkdir", "-p", KubeletPKIPath); err != nil {
		return err
	}

	data := map[string]string{
		"KubeDNSIP":        kubeDNSIP,
		"ClusterDomain":    ClusterDomain,
		"StaticPodPath":    StaticPodPath,
		"ContainerdSocket": "/run/containerd/containerd.sock",
		"CAFile":           filepath.Join(KubeletPKIPath, "ca.crt"),
		"KubeletCertFile":  filepath.Join(KubeletPKIPath, "kubelet.crt"),
		"KubeletKeyFile":   filepath.Join(KubeletPKIPath, "kubelet.key"),
		"ResolvConfFile":   DetectResolvConfFile(),
	}

	if err := executeTemplateFromEmbed(NodeConfigs, "configassets/kubelet-config.yaml", KubeletConfigPath, data); err != nil {
		return fmt.Errorf("write kubelet.conf: %w", err)
	}

	kubeconfigData := struct {
		ClusterName string
		Server      string
		CAFile      string
		CertFile    string
		KeyFile     string
	}{
		ClusterName: "local",
		Server:      fmt.Sprintf("https://%s:6443", clusterFQDN),
		CAFile:      filepath.Join(KubeletPKIPath, "ca.crt"),
		CertFile:    filepath.Join(KubeletPKIPath, "kubelet.crt"),
		KeyFile:     filepath.Join(KubeletPKIPath, "kubelet.key"),
	}

	if err := executeTemplateFromEmbed(NodeConfigs, "configassets/kubelet.kubeconfig", KubeletKubeconfigPath, kubeconfigData); err != nil {
		return fmt.Errorf("write kubelet.kubeconfig: %w", err)
	}

	return nil
}

func generateComponentKubeconfig(name, clusterFQDN string) error {
	slog.Info(fmt.Sprintf("--- Generating %s kubeconfig ---", name))

	data := struct {
		ClusterName string
		Server      string
		CAFile      string
		CertFile    string
		KeyFile     string
	}{
		ClusterName: "local",
		Server:      fmt.Sprintf("https://%s:6443", clusterFQDN),
		CAFile:      filepath.Join(PKIPath, "ca.crt"),
		CertFile:    filepath.Join(PKIPath, fmt.Sprintf("kube-%s.crt", name)),
		KeyFile:     filepath.Join(PKIPath, fmt.Sprintf("kube-%s.key", name)),
	}

	tmpl := fmt.Sprintf("configassets/%s.kubeconfig", name)
	dest := fmt.Sprintf("/etc/kubernetes/%s.conf", name)

	if err := executeTemplateFromEmbed(NodeConfigs, tmpl, dest, data); err != nil {
		return fmt.Errorf("write %s.conf: %w", name, err)
	}

	return nil
}

func generateSchedulerKubeconfig(clusterFQDN string) error {
	return generateComponentKubeconfig("scheduler", clusterFQDN)
}

func generateControllerManagerKubeconfig(clusterFQDN string) error {
	return generateComponentKubeconfig("controller-manager", clusterFQDN)
}

func createStaticPods(podCIDR, serviceCIDR, controllerImage, schedulerImage string) error {
	slog.Info("--- Creating static pods ---")

	if err := RunCmd("mkdir", "-p", StaticPodPath); err != nil {
		return err
	}

	cmData := map[string]any{
		"Image":              controllerImage,
		"KubeconfigPath":     "/etc/kubernetes/controller-manager.conf",
		"RootCAFile":         filepath.Join(PKIPath, "ca.crt"),
		"ClusterSigningCert": filepath.Join(PKIPath, "ca.crt"),
		"ClusterSigningKey":  filepath.Join(PKIPath, "ca.key"),
		"cluster": map[string]any{
			"network": map[string]any{
				"podCIDR":     podCIDR,
				"serviceCIDR": serviceCIDR,
			},
		},
	}

	if err := executeTemplateFromEmbed(StaticPods, "podassets/controller-manager.yaml", filepath.Join(StaticPodPath, "controller-manager.yaml"), cmData); err != nil {
		return fmt.Errorf("write controller-manager.yaml: %w", err)
	}

	schedData := struct {
		Image          string
		KubeconfigPath string
	}{
		Image:          schedulerImage,
		KubeconfigPath: "/etc/kubernetes/scheduler.conf",
	}

	if err := executeTemplateFromEmbed(StaticPods, "podassets/scheduler.yaml", filepath.Join(StaticPodPath, "scheduler.yaml"), schedData); err != nil {
		return fmt.Errorf("write scheduler.yaml: %w", err)
	}

	return nil
}

func installBinary(name, version string, verbose bool) error {
	slog.Info(fmt.Sprintf("--- Installing %s ---", name))

	versions := map[string]string{
		"linux/amd64": version,
		"linux/arm64": version,
	}

	key := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	v, ok := versions[key]
	if !ok {
		return fmt.Errorf("no %s version for %s", name, key)
	}

	url := fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/%s/%s", v, runtime.GOARCH, name)
	dest := fmt.Sprintf("/usr/local/bin/%s", name)

	if verbose {
		slog.Info(fmt.Sprintf("Downloading %s from: %s", name, url))
	}

	if err := RunCmdCapture("curl", "-fsSL", url, "-o", dest); err != nil {
		return fmt.Errorf("downloading %s from %s: %w", name, url, err)
	}

	if err := RunCmdCapture("chmod", "755", dest); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}

	return nil
}

func installKubelet(version string, verbose bool) error {
	return installBinary("kubelet", version, verbose)
}

func installKubectl(version string, verbose bool) error {
	return installBinary("kubectl", version, verbose)
}

func ensureContainerd() error {
	slog.Info("--- Ensuring containerd is enabled and started ---")

	if err := RunCmdCapture("systemctl", "enable", "containerd"); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "restart", "containerd"); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "is-active", "containerd"); err != nil {
		return fmt.Errorf("containerd not running: %w", err)
	}

	slog.Info("containerd is enabled and running")
	return nil
}

func installIscsiadm() error {
	slog.Info("--- Installing iscsiadm ---")

	if _, err := exec.LookPath("iscsiadm"); err == nil {
		slog.Info("iscsiadm already installed, skipping")
		return nil
	}

	mgr, err := detect.Default()
	if err != nil {
		return fmt.Errorf("detect package manager: %w", err)
	}

	slog.Info(fmt.Sprintf("Detected package manager: %s", mgr.Name()))

	pkgName := "open-iscsi"
	switch mgr.Name() {
	case "dnf", "yum", "rpm":
		pkgName = "iscsi-initiator-utils"
	}

	if _, err := mgr.Install(context.Background(), snack.Targets(pkgName), snack.WithSudo(), snack.WithAssumeYes()); err != nil {
		return fmt.Errorf("install %s: %w", pkgName, err)
	}

	slog.Info("iscsiadm installed successfully")
	return nil
}

func installRunc() error {
	slog.Info("--- Installing runc ---")

	mgr, err := detect.Default()
	if err != nil {
		slog.Warn(fmt.Sprintf("warning: detect package manager: %v, will install manually", err))
		return installRuncManual()
	}

	slog.Info(fmt.Sprintf("Detected package manager: %s", mgr.Name()))

	installed, err := mgr.IsInstalled(context.Background(), "runc")
	if err != nil {
		slog.Warn(fmt.Sprintf("warning: check runc installation: %v", err))
	}
	if installed {
		version, err := mgr.Version(context.Background(), "runc")
		if err == nil {
			slog.Info(fmt.Sprintf("runc %s installed via package manager", version))
			if isRuncVersionSufficient(version) {
				slog.Info("runc version is sufficient")
				return nil
			}
			slog.Warn(fmt.Sprintf("runc %s is too old, removing and installing manually", version))
			if _, err := mgr.Remove(context.Background(), snack.Targets("runc"), snack.WithSudo(), snack.WithAssumeYes()); err != nil {
				slog.Warn(fmt.Sprintf("warning: remove old runc: %v", err))
			}
			return installRuncManual()
		}
	}

	slog.Info("runc not installed, installing manually")
	return installRuncManual()
}

func isRuncVersionSufficient(version string) bool {
	var major, minor int
	fmt.Sscanf(version, "%d.%d", &major, &minor)
	return major > 1 || (major == 1 && minor >= 2)
}

func installRuncManual() error {
	slog.Info("--- Installing runc manually ---")

	runcVersion := RuncVersion
	url := fmt.Sprintf("https://github.com/opencontainers/runc/releases/download/v%s/runc.%s", runcVersion, runtime.GOARCH)

	slog.Info(fmt.Sprintf("Downloading runc %s", runcVersion))

	if err := RunCmdCapture("curl", "-fL", url, "-o", "/tmp/runc"); err != nil {
		return fmt.Errorf("download runc: %w", err)
	}

	if err := RunCmdCapture("install", "-m", "0755", "/tmp/runc", "/usr/local/sbin/runc"); err != nil {
		return fmt.Errorf("install runc binary: %w", err)
	}

	if err := os.Remove("/tmp/runc"); err != nil {
		slog.Warn(fmt.Sprintf("warning: remove /tmp/runc: %v", err))
	}

	cmd := exec.Command("runc", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify runc: %w", err)
	}
	slog.Info(fmt.Sprintf("runc installed: %s", string(output)))

	return nil
}

func installContainerd() error {
	slog.Info("--- Installing containerd ---")

	version := ContainerdVersion
	arch := runtime.GOARCH

	tarball := fmt.Sprintf("containerd-%s-linux-%s.tar.gz", version, arch)
	url := fmt.Sprintf("https://github.com/containerd/containerd/releases/download/v%s/%s", version, tarball)

	slog.Info(fmt.Sprintf("Downloading containerd %s", version))

	if err := RunCmdCapture("curl", "-fsSL", url, "-o", "/tmp/"+tarball); err != nil {
		return fmt.Errorf("download containerd: %w", err)
	}

	if err := RunCmdCapture("tar", "-C", "/usr/local", "-xzf", "/tmp/"+tarball); err != nil {
		return fmt.Errorf("extract containerd: %w", err)
	}

	if err := os.Remove("/tmp/" + tarball); err != nil {
		slog.Warn(fmt.Sprintf("warning: remove /tmp/%s: %v", tarball, err))
	}

	if err := RunCmdCapture("mkdir", "-p", "/etc/containerd"); err != nil {
		return fmt.Errorf("create /etc/containerd: %w", err)
	}

	if err := RunCmdCapture("sh", "-c", "/usr/local/bin/containerd config default > /etc/containerd/config.toml"); err != nil {
		return fmt.Errorf("generate default config: %w", err)
	}

	serviceURL := fmt.Sprintf("https://raw.githubusercontent.com/containerd/containerd/v%s/containerd.service", version)

	if err := RunCmdCapture("mkdir", "-p", "/usr/local/lib/systemd/system"); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}

	if err := RunCmdCapture("curl", "-fsSL", serviceURL, "-o", "/usr/local/lib/systemd/system/containerd.service"); err != nil {
		return fmt.Errorf("download containerd.service: %w", err)
	}

	if err := RunCmdCapture("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}

	if err := RunCmdCapture("systemctl", "enable", "--now", "containerd"); err != nil {
		return fmt.Errorf("enable containerd: %w", err)
	}

	return nil
}

func configureContainerd(info *HostInfo) error {
	slog.Info("--- Configuring containerd ---")

	osConfig := DetectOSConfigurator(info.Distro)
	dataRoot := osConfig.DataRoot()
	containerdRoot := dataRoot + "/containerd"

	if containerdRoot != "/var/lib/containerd" {
		if err := RunCmd("mkdir", "-p", containerdRoot); err != nil {
			return fmt.Errorf("create containerd root: %w", err)
		}
		pattern := fmt.Sprintf(`s|root = "/var/lib/containerd"|root = %q|`, containerdRoot)
		if err := RunCmdCapture("sed", "-i", pattern, ContainerdConfigPath); err != nil {
			return fmt.Errorf("set containerd root: %w", err)
		}
		slog.Info(fmt.Sprintf("containerd root set to %s", containerdRoot))
	}

	if IsSystemdManaged() {
		if err := RunCmdCapture("sed", "-i", "s/SystemdCgroup = false/SystemdCgroup = true/", ContainerdConfigPath); err != nil {
			return fmt.Errorf("set SystemdCgroup: %w", err)
		}
		slog.Info("systemd detected, SystemdCgroup set to true")
	}

	return nil
}

func installCrictl() error {
	slog.Info("--- Installing crictl ---")

	versions := map[string]string{
		"linux/amd64": CrictlVersion,
		"linux/arm64": CrictlVersion,
	}

	key := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	version, ok := versions[key]
	if !ok {
		return fmt.Errorf("no crictl version for %s", key)
	}

	url := fmt.Sprintf("https://github.com/kubernetes-sigs/cri-tools/releases/download/%s/crictl-%s-%s-%s.tar.gz",
		version, version, runtime.GOOS, runtime.GOARCH)

	if err := RunCmdCapture("curl", "-fsSL", url, "-o", "/tmp/crictl.tar.gz"); err != nil {
		return err
	}

	if err := RunCmdCapture("tar", "-xzf", "/tmp/crictl.tar.gz", "-C", "/usr/local/bin"); err != nil {
		return err
	}

	if err := os.Remove("/tmp/crictl.tar.gz"); err != nil {
		slog.Warn(fmt.Sprintf("warning: remove /tmp/crictl.tar.gz: %v", err))
	}

	data, err := NodeConfigs.ReadFile("configassets/crictl.yaml")
	if err != nil {
		return fmt.Errorf("read crictl config: %w", err)
	}

	if err := writeFileAsRoot("/etc/crictl.yaml", data, 0644); err != nil {
		return err
	}

	return nil
}

func enableKubelet() error {
	slog.Info("--- Enabling and starting kubelet ---")

	data, err := NodeConfigs.ReadFile("configassets/kubelet.service")
	if err != nil {
		return fmt.Errorf("read kubelet.service: %w", err)
	}

	if err := writeFileAsRoot("/etc/systemd/system/kubelet.service", data, 0644); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "daemon-reload"); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "enable", "kubelet"); err != nil {
		return err
	}

	if err := RunCmdCapture("systemctl", "start", "kubelet"); err != nil {
		return err
	}

	return nil
}

func executeTemplateFromEmbed(fsys any, templatePath, outputPath string, data interface{}) error {
	tmplData, err := fsys.(interface{ ReadFile(string) ([]byte, error) }).ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", templatePath, err)
	}

	tmpl, err := template.New("").Parse(string(tmplData))
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return writeFileAsRoot(outputPath, buf.Bytes(), 0644)
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func copyFile(src, dst string) error {
	return RunCmdCapture("cp", src, dst)
}

func checkConnectivity(clusterDNS string) error {
	if clusterDNS == "" {
		return nil
	}

	slog.Info("--- Checking connectivity ---")

	target := net.JoinHostPort(clusterDNS, "6443")
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.Dial("tcp", target)
	if err != nil {
		return fmt.Errorf("cannot reach cluster API at %s: %w", target, err)
	}
	conn.Close()
	slog.Info(fmt.Sprintf("Cluster API at %s is reachable", target))

	return nil
}

func ptrMap(m map[string]string) *map[string]string {
	if len(m) == 0 {
		return nil
	}
	return &m
}

func hostHardwareToV202607(hw *HostHardware) *v202607.HardwareInfo {
	if hw == nil {
		return nil
	}

	cpuModel := hw.CPUModel
	cores := hw.CPUCores
	memMB := int(hw.MemoryMB)
	isVirtual := hw.IsVirtual

	return &v202607.HardwareInfo{
		Cpus: &[]v202607.CPU{{
			Model: &cpuModel,
			Cores: &cores,
		}},
		Memory: &v202607.Memory{
			TotalInMb: &memMB,
		},
		IsVirtual: &isVirtual,
	}
}
