package kubehubcli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/stretchr/testify/require"
)

func testNode(name, ip string) v202607.Node {
	return v202607.Node{
		Metadata: &v202607.NodeMetadata{Name: &name},
		Spec: &v202607.NodeSpec{
			Meta: &v202607.NodeInfo{Ipv4: &ip},
		},
	}
}

func TestCheckNodeIPConflicts(t *testing.T) {
	nodes := []v202607.Node{
		testNode("node1", "10.0.0.1"),
		testNode("node2", "10.0.0.2"),
	}

	require.NoError(t, checkNodeIPConflicts(nodes, "10.0.0.3"))

	err := checkNodeIPConflicts(nodes, "10.0.0.2")
	require.ErrorContains(t, err, "node2 already uses IP 10.0.0.2")

	duplicate := append(nodes, testNode("node3", "10.0.0.1"))
	err = checkNodeIPConflicts(duplicate, "10.0.0.4")
	require.ErrorContains(t, err, "nodes node1 and node3 share the same IP 10.0.0.1")

	err = checkNodeIPConflicts([]v202607.Node{{}}, "10.0.0.1")
	require.NoError(t, err)
}

func TestCheckExistingNodesReachable(t *testing.T) {
	origProbe := probeKubelet
	t.Cleanup(func() { probeKubelet = origProbe })

	var probed []string
	probeKubelet = func(ip string) error {
		probed = append(probed, ip)
		return nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]v202607.Node{
			testNode("node1", "10.0.0.1"),
			testNode("node2", "10.0.0.2"),
			testNode("node3", "10.0.0.3"),
		})
	}))
	defer srv.Close()

	client, err := v202607.NewClient(srv.URL)
	require.NoError(t, err)

	auth := func(ctx context.Context, req *http.Request) error { return nil }
	require.NoError(t, checkExistingNodesReachable(context.Background(), client, auth, "pp2", "node1"))
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3"}, probed)
}

func TestCheckExistingNodesReachableRejectsUnreachableNode(t *testing.T) {
	origProbe := probeKubelet
	t.Cleanup(func() { probeKubelet = origProbe })
	probeKubelet = func(ip string) error {
		if ip == "10.0.0.3" {
			return errors.New("connection refused")
		}
		return nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]v202607.Node{
			testNode("node1", "10.0.0.1"),
			testNode("node2", "10.0.0.2"),
			testNode("node3", "10.0.0.3"),
		})
	}))
	defer srv.Close()

	client, err := v202607.NewClient(srv.URL)
	require.NoError(t, err)

	auth := func(ctx context.Context, req *http.Request) error { return nil }
	err = checkExistingNodesReachable(context.Background(), client, auth, "pp2", "node1")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "cannot reach existing node node3 (10.0.0.3) on port 10250"))
}

func TestCheckExistingNodesReachableNoPreviousNodes(t *testing.T) {
	origProbe := probeKubelet
	t.Cleanup(func() { probeKubelet = origProbe })

	probed := false
	probeKubelet = func(ip string) error {
		probed = true
		return nil
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]v202607.Node{testNode("node1", "10.0.0.1")})
	}))
	defer srv.Close()

	client, err := v202607.NewClient(srv.URL)
	require.NoError(t, err)

	auth := func(ctx context.Context, req *http.Request) error { return nil }
	require.NoError(t, checkExistingNodesReachable(context.Background(), client, auth, "pp2", "node1"))
	require.False(t, probed)
}
