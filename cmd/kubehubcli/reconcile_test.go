package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
)

func withTestHome(t *testing.T) string {
	t.Helper()
	oldHome := os.Getenv("HOME")
	dir := t.TempDir()
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })
	return dir
}

func writeCachedToken(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "kubehubcli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"access_token":  "test-access-token",
		"token_type":    "Bearer",
		"refresh_token": "test-refresh-token",
		"expiry":        time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClusterReconcileSendsETagHeader(t *testing.T) {
	withTestHome(t)
	writeCachedToken(t)

	const etag = `W/"cluster-etag-123"`

	var gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"name": "pp2", "etag": etag},
				"spec":     map[string]any{"region": "us-east"},
			})
		case http.MethodPut:
			gotIfMatch = r.Header.Get("If-Match")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cmd := clusterReconcileCmd(&kubehubcli.Config{Server: srv.URL})
	cmd.SetArgs([]string{"--cluster", "pp2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotIfMatch != etag {
		t.Fatalf("If-Match header = %q, want %q", gotIfMatch, etag)
	}
}

func TestNodeReconcileSendsETagHeader(t *testing.T) {
	withTestHome(t)
	writeCachedToken(t)

	const etag = `"node-etag-456"`
	var gotIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"name": "node1", "etag": etag},
				"spec":     map[string]any{"os": "ubuntu", "arch": "amd64"},
			})
		case http.MethodPut:
			gotIfMatch = r.Header.Get("If-Match")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	cmd := nodeReconcileCmd(&kubehubcli.Config{Server: srv.URL})
	cmd.SetArgs([]string{"--cluster", "pp2", "--node", "node1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gotIfMatch != etag {
		t.Fatalf("If-Match header = %q, want %q", gotIfMatch, etag)
	}
}

func stubGvisorInstall(t *testing.T, calls *int) {
	t.Helper()
	orig := installGvisorOnHost
	t.Cleanup(func() { installGvisorOnHost = orig })
	installGvisorOnHost = func() error {
		*calls++
		return nil
	}

	kubehubcli.SetAutoAccept(true)
	t.Cleanup(func() { kubehubcli.SetAutoAccept(false) })
}

func nodeReconcileServer(t *testing.T, putCalls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"name": "node1", "etag": `"node-etag-456"`},
				"spec":     map[string]any{"os": "ubuntu", "arch": "amd64"},
			})
		case http.MethodPut:
			*putCalls++
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNodeReconcileInstallGvisor(t *testing.T) {
	withTestHome(t)
	writeCachedToken(t)

	var gvisorCalls, putCalls int
	stubGvisorInstall(t, &gvisorCalls)

	srv := nodeReconcileServer(t, &putCalls)

	cmd := nodeReconcileCmd(&kubehubcli.Config{Server: srv.URL})
	cmd.SetArgs([]string{"--cluster", "pp2", "--node", "node1", "--install-gvisor"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gvisorCalls != 1 {
		t.Fatalf("gVisor install calls = %d, want 1", gvisorCalls)
	}
	if putCalls != 1 {
		t.Fatalf("node update calls = %d, want 1", putCalls)
	}
}

func TestNodeReconcileSkipsGvisorByDefault(t *testing.T) {
	withTestHome(t)
	writeCachedToken(t)

	var gvisorCalls, putCalls int
	stubGvisorInstall(t, &gvisorCalls)

	srv := nodeReconcileServer(t, &putCalls)

	cmd := nodeReconcileCmd(&kubehubcli.Config{Server: srv.URL})
	cmd.SetArgs([]string{"--cluster", "pp2", "--node", "node1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if gvisorCalls != 0 {
		t.Fatalf("gVisor install calls = %d, want 0", gvisorCalls)
	}
	if putCalls != 1 {
		t.Fatalf("node update calls = %d, want 1", putCalls)
	}
}
