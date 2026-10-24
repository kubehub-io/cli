package kubehubcli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type JoinPlan struct {
	Network  []JoinPlanItem
	System   []JoinPlanItem
	Software []JoinPlanItem
}

type JoinPlanItem struct {
	Summary     string
	Description string
}

func (p JoinPlan) writeTo(w io.Writer) error {
	sections := []struct {
		title string
		items []JoinPlanItem
	}{
		{"NETWORK SETUP", p.Network},
		{"SYSTEM SETUP", p.System},
		{"SOFTWARE INSTALL", p.Software},
	}
	width := 68
	if _, err := fmt.Fprintf(w, "\n╭%s╮\n│%s│\n", repeat("─", width), center("NODE JOIN PLAN", width)); err != nil {
		return err
	}
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "├%s┤\n│  %-*s│\n", repeat("─", width), width-2, section.title); err != nil {
			return err
		}
		for _, item := range section.items {
			if err := writePlanText(w, "│  • ", item.Summary, width+1); err != nil {
				return err
			}
			if item.Description != "" {
				if err := writePlanText(w, "│    ", item.Description, width+1); err != nil {
					return err
				}
			}
		}
	}
	if _, err := fmt.Fprintf(w, "╰%s╯\n", repeat("─", width)); err != nil {
		return err
	}
	return nil
}

func repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

func center(s string, width int) string {
	padding := width - len([]rune(s))
	left := padding / 2
	right := padding - left
	return repeat(" ", left) + s + repeat(" ", right)
}

func writePlanText(w io.Writer, prefix, text string, width int) error {
	words := strings.Fields(text)
	line := prefix
	prefixWidth := len([]rune(prefix))
	lineWidth := prefixWidth
	continuation := "│" + strings.Repeat(" ", prefixWidth-1)
	for _, word := range words {
		wordWidth := len([]rune(word))
		if lineWidth > prefixWidth && lineWidth+1+wordWidth > width {
			if _, err := fmt.Fprintf(w, "%-*s│\n", width, line); err != nil {
				return err
			}
			line = continuation + word
			lineWidth = prefixWidth + wordWidth
			continue
		}
		if lineWidth > prefixWidth {
			line += " "
			lineWidth++
		}
		line += word
		lineWidth += wordWidth
	}
	_, err := fmt.Fprintf(w, "%-*s│\n", width, line)
	return err
}

func confirmJoinPlan(plan JoinPlan) error {
	if err := plan.writeTo(os.Stdout); err != nil {
		return fmt.Errorf("print join plan: %w", err)
	}
	if AutoAccept() {
		fmt.Fprintln(os.Stdout, "Proceeding (--yes).")
		return nil
	}
	fmt.Fprint(os.Stdout, "Press Enter to apply this plan and join the cluster, or Ctrl+C to cancel: ")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	return nil
}

func newJoinPlan(info *HostInfo, opts *JoinOptions, preflight *PreflightResult, laptop bool) JoinPlan {
	plan := JoinPlan{}
	if preflight != nil {
		plan.Network = append(plan.Network, preflight.NetworkPlan...)
	}

	plan.System = append(plan.System,
		JoinPlanItem{Summary: "Apply Kubernetes kernel settings", Description: "/etc/sysctl.d/98-kubernetes.conf"},
		JoinPlanItem{Summary: "Write kubelet and control-plane configuration", Description: "/var/lib/kubelet config and PKI; /etc/kubernetes kubeconfigs and manifests"},
		JoinPlanItem{Summary: "Configure containerd and CRI", Description: "/etc/containerd/config.toml and /etc/crictl.yaml"},
		JoinPlanItem{Summary: "Enable and start kubelet", Description: "/etc/systemd/system/kubelet.service; create /opt/cni/bin if needed"},
	)
	if laptop {
		plan.System = append(plan.System, JoinPlanItem{
			Summary:     "Keep the node running when the lid is closed",
			Description: "Set systemd-logind lid switch actions to ignore",
		})
	}

	if info == nil {
		return plan
	}
	plan.Software = append(plan.Software, JoinPlanItem{
		Summary:     "Ensure runc is installed and up to date",
		Description: "Install or update only if the host version is missing or too old",
	})
	if info.Containerd == nil {
		plan.Software = append(plan.Software, JoinPlanItem{Summary: "containerd " + ContainerdVersion})
	}
	if !hasCommand("iscsiadm") {
		plan.Software = append(plan.Software, JoinPlanItem{Summary: "iSCSI utilities (iscsiadm)"})
	}
	plan.Software = append(plan.Software,
		JoinPlanItem{Summary: "crictl " + CrictlVersion},
		JoinPlanItem{Summary: "kubelet and kubectl", Description: "Kubernetes version selected by the cluster"},
	)
	if opts != nil && opts.InstallGvisor {
		plan.Software = append(plan.Software, JoinPlanItem{Summary: "gVisor runtime (runsc)"})
	}
	return plan
}
