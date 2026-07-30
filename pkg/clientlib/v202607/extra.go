package v202607

// Extra fields returned by the API beyond the swagger spec.

// ControlPlaneStatus describes the cluster control plane components.
type ControlPlaneStatus struct {
	Components *ControlPlaneComponents `json:"components,omitempty"`
}

// ControlPlaneComponents holds version info for each control plane component.
type ControlPlaneComponents struct {
	Konnectivity   *ComponentInfo `json:"konnectivity,omitempty"`
	KubeApiserver  *ComponentInfo `json:"kubeApiserver,omitempty"`
	KubeController *ComponentInfo `json:"kubeController,omitempty"`
	KubeScheduler  *ComponentInfo `json:"kubeScheduler,omitempty"`
	Etcd           *ComponentInfo `json:"etcd,omitempty"`
	CoreDns        *ComponentInfo `json:"coreDns,omitempty"`
}

// ComponentInfo describes a single control plane component.
type ComponentInfo struct {
	ID      *string `json:"id,omitempty"`
	Version *string `json:"version,omitempty"`
	Type    *string `json:"type,omitempty"`
}
