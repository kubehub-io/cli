package kubehubcli

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactRequestBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	redacted := redactRequestBody(req, []byte("grant_type=refresh_token&refresh_token=secret-value"))
	if strings.Contains(redacted, "secret-value") {
		t.Fatalf("request body contains token: %s", redacted)
	}
	if !strings.Contains(redacted, "grant_type=refresh_token") {
		t.Fatalf("request body lost grant type: %s", redacted)
	}
}

func TestRedactJSONBody(t *testing.T) {
	redacted := redactJSONBody([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":1800}`))
	if strings.Contains(redacted, `:"access"`) || strings.Contains(redacted, `:"refresh"`) {
		t.Fatalf("response body contains token: %s", redacted)
	}
	if !strings.Contains(redacted, `"expires_in":1800`) {
		t.Fatalf("response body lost safe fields: %s", redacted)
	}
}
