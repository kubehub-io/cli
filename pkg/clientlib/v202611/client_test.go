package v202611

import (
	"net/http/httptest"
	"testing"
)

func TestNewClientUsesAPIServerPath(t *testing.T) {
	server := httptest.NewServer(nil)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Server != server.URL+apiServerPath+"/" {
		t.Fatalf("client server = %q, want %q", client.Server, server.URL+apiServerPath+"/")
	}

	client, err = NewClient(server.URL + apiServerPath)
	if err != nil {
		t.Fatalf("NewClient() with API path error = %v", err)
	}
	if client.Server != server.URL+apiServerPath+"/" {
		t.Fatalf("client server = %q, want %q", client.Server, server.URL+apiServerPath+"/")
	}
}
