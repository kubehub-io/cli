package kubehubcli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type Authenticator struct {
	IssuerURL string
	ClientID  string
	Scopes    []string
	tokenFile string
	forceAuth bool
	Verbose   bool
}

func NewAuthenticator(issuerURL, clientID string) *Authenticator {
	home, _ := os.UserHomeDir()
	tokenFile := filepath.Join(home, ".config", "kubehubcli", "token.json")
	if home == "" {
		tokenFile = ".token.json"
	}
	return &Authenticator{
		IssuerURL: issuerURL,
		ClientID:  clientID,
		Scopes:    []string{"openid", "profile", "email", "offline_access"},
		tokenFile: tokenFile,
		forceAuth: false,
	}
}

func (a *Authenticator) WithVerbose(v bool) *Authenticator {
	a.Verbose = v
	return a
}

func (a *Authenticator) WithTokenFile(path string) *Authenticator {
	a.tokenFile = path
	return a
}

func (a *Authenticator) WithForceAuth(force bool) *Authenticator {
	a.forceAuth = force
	return a
}

func (a *Authenticator) Authenticate(ctx context.Context) (string, error) {
	if a.Verbose {
		slog.Info(fmt.Sprintf("Authenticator: issuer=%s client-id=%s token-file=%s", a.IssuerURL, a.ClientID, a.tokenFile))
	}

	if !a.forceAuth {
		token, err := a.loadToken()
		if err == nil && token != nil && !token.Expiry.IsZero() && token.Expiry.After(time.Now()) {
			if a.Verbose {
				slog.Info(fmt.Sprintf("Authenticator: using cached token (valid until %s)", token.Expiry))
			}
			return token.AccessToken, nil
		}
		if err != nil {
			if a.Verbose {
				slog.Info(fmt.Sprintf("Authenticator: cached token not usable: %v", err))
			}
		} else if a.Verbose {
			slog.Info("Authenticator: cached token expired or missing")
		}
	} else if a.Verbose {
		slog.Info("Authenticator: force-auth enabled, skipping cached token")
	}

	hasBrowser, err := detectBrowser()
	if err != nil {
		return "", fmt.Errorf("detect browser: %w", err)
	}
	if a.Verbose {
		slog.Info(fmt.Sprintf("Authenticator: browser detected=%v os=%s", hasBrowser, runtime.GOOS))
	}

	if hasBrowser && runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		return a.webAuth(ctx)
	}
	return a.deviceAuth(ctx)
}

func (a *Authenticator) oauth2Config() *oauth2.Config {
	authURL, _ := url.JoinPath(a.IssuerURL, "/protocol/openid-connect/auth")
	tokenURL, _ := url.JoinPath(a.IssuerURL, "/protocol/openid-connect/token")
	deviceAuthURL, _ := url.JoinPath(a.IssuerURL, "/protocol/openid-connect/auth/device")

	if a.Verbose {
		slog.Info(fmt.Sprintf("OAuth2 config: auth-url=%s token-url=%s device-auth-url=%s", authURL, tokenURL, deviceAuthURL))
	}

	return &oauth2.Config{
		ClientID: a.ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:       authURL,
			TokenURL:      tokenURL,
			DeviceAuthURL: deviceAuthURL,
		},
		RedirectURL: "http://localhost:8000/callback",
		Scopes:      a.Scopes,
	}
}

func (a *Authenticator) oauthContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: &loggingRoundTripper{next: http.DefaultTransport, verbose: a.Verbose},
	})
}

func (a *Authenticator) deviceAuth(ctx context.Context) (string, error) {
	conf := a.oauth2Config()
	ctx = a.oauthContext(ctx)

	if a.Verbose {
		slog.Info("DeviceAuth: requesting device authorization code...")
	}
	deviceAuth, err := conf.DeviceAuth(ctx, oauth2.AccessTypeOffline)
	if err != nil {
		return "", fmt.Errorf("device auth request: %w", err)
	}

	if a.Verbose {
		slog.Info(fmt.Sprintf("DeviceAuth: response verification_uri=%s user_code=%s device_code=%s interval=%d",
			deviceAuth.VerificationURI, deviceAuth.UserCode, deviceAuth.DeviceCode, deviceAuth.Interval))
	}

	slog.Info(fmt.Sprintf("Please visit: %s", deviceAuth.VerificationURI))
	slog.Info(fmt.Sprintf("and enter code: %s", deviceAuth.UserCode))
	slog.Info("Waiting for authentication...")
	slog.Info("(If you can't open the URL, copy the code and enter it manually)")

	if a.Verbose {
		slog.Info("DeviceAuth: polling for token...")
	}
	token, err := conf.DeviceAccessToken(ctx, deviceAuth, oauth2.AccessTypeOffline)
	if err != nil {
		return "", fmt.Errorf("device access token: %w", err)
	}

	if a.Verbose {
		slog.Info(fmt.Sprintf("DeviceAuth: token received (expiry=%s)", token.Expiry))
	}

	if err := a.saveToken(token); err != nil {
		return "", fmt.Errorf("save token: %w", err)
	}

	if a.Verbose {
		slog.Info(fmt.Sprintf("DeviceAuth: token saved to %s", a.tokenFile))
	}
	return token.AccessToken, nil
}

func (a *Authenticator) webAuth(ctx context.Context) (string, error) {
	if a.Verbose {
		slog.Info("webAuth: starting web-based authentication")
	}
	conf := a.oauth2Config()
	ctx = a.oauthContext(ctx)
	verifier := oauth2.GenerateVerifier()
	state, err := randomString(20)
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:8000")
	if err != nil {
		return "", fmt.Errorf("listen on port 8000: %w", err)
	}
	defer listener.Close()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errChan <- fmt.Errorf("missing code in callback")
			return
		}
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("state mismatch")
			return
		}
		codeChan <- code
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p>Authentication complete. You may close this page.</p>"))
	})

	server := &http.Server{Handler: mux}
	go func() {
		errChan <- server.Serve(listener)
	}()

	authURL := conf.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
	)

	slog.Info(fmt.Sprintf("Please open in your browser: %s", authURL))

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		return "", fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		return "", ctx.Err()
	}

	server.Shutdown(ctx)

	if a.Verbose {
		slog.Info("webAuth: exchanging code for token...")
	}
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	if a.Verbose {
		slog.Info(fmt.Sprintf("webAuth: token received (expiry=%s)", token.Expiry))
	}

	if err := a.saveToken(token); err != nil {
		return "", fmt.Errorf("save token: %w", err)
	}

	return token.AccessToken, nil
}

func (a *Authenticator) loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(a.tokenFile)
	if err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("loadToken: cannot read token file %s: %v", a.tokenFile, err))
		}
		return nil, err
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("loadToken: cannot parse token file %s: %v", a.tokenFile, err))
		}
		return nil, err
	}

	if a.Verbose {
		slog.Info(fmt.Sprintf("loadToken: loaded token (expiry=%s, refresh=%v)", token.Expiry, token.RefreshToken != ""))
	}

	if token.RefreshToken == "" {
		if a.Verbose {
			slog.Info("loadToken: no refresh token, need re-authentication")
		}
		return nil, fmt.Errorf("no refresh token")
	}

	conf := a.oauth2Config()
	ctx := a.oauthContext(context.Background())
	ts := conf.TokenSource(ctx, &token)
	if a.Verbose {
		slog.Info("loadToken: refreshing token...")
	}
	newToken, err := ts.Token()
	if err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("loadToken: refresh failed: %v", err))
		}
		return nil, err
	}
	if a.Verbose {
		slog.Info(fmt.Sprintf("loadToken: refresh succeeded (new expiry=%s)", newToken.Expiry))
	}

	if newToken.AccessToken != token.AccessToken {
		if a.Verbose {
			slog.Info("loadToken: saving refreshed token")
		}
		if err := a.saveToken(newToken); err != nil {
			return nil, err
		}
	}

	return newToken, nil
}

func (a *Authenticator) saveToken(token *oauth2.Token) error {
	dir := filepath.Dir(a.tokenFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("saveToken: mkdir failed: %v", err))
		}
		return err
	}

	data, err := json.Marshal(token)
	if err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("saveToken: marshal failed: %v", err))
		}
		return err
	}

	tmp := a.tokenFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("saveToken: write tmp failed: %v", err))
		}
		return err
	}

	if err := os.Rename(tmp, a.tokenFile); err != nil {
		if a.Verbose {
			slog.Info(fmt.Sprintf("saveToken: rename failed: %v", err))
		}
		return err
	}

	if a.Verbose {
		slog.Info(fmt.Sprintf("saveToken: token saved to %s (expiry=%s)", a.tokenFile, token.Expiry))
	}
	return nil
}

type loggingRoundTripper struct {
	next    http.RoundTripper
	verbose bool
}

func (t *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.verbose {
		slog.Info(fmt.Sprintf(">>> %s %s", req.Method, req.URL.String()))
		for k, v := range req.Header {
			kl := strings.ToLower(k)
			isSensitive := kl == "authorization" ||
				kl == "proxy-authorization" ||
				kl == "cookie" ||
				kl == "set-cookie" ||
				kl == "x-api-key" ||
				kl == "x-auth-token"
			if isSensitive {
				slog.Info(fmt.Sprintf(">>> %s: [REDACTED]", k))
				continue
			}
			for _, h := range v {
				slog.Info(fmt.Sprintf(">>> %s: %s", k, h))
			}
		}

		if req.Body != nil {
			body, _ := io.ReadAll(req.Body)
			req.Body.Close()
			if len(body) > 0 {
				slog.Info(fmt.Sprintf(">>> body: %s", string(body)))
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		if t.verbose {
			slog.Info(fmt.Sprintf("<<< %s %s error: %v", req.Method, req.URL.String(), err))
		}
		return nil, err
	}

	if t.verbose {
		slog.Info(fmt.Sprintf("<<< %s %s status=%d", req.Method, req.URL.String(), resp.StatusCode))

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(respBody) > 0 {
			slog.Info(fmt.Sprintf("<<< body: %s", string(respBody)))
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}

	return resp, nil
}

func randomString(length int) (string, error) {
	b := make([]byte, length/2)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func detectBrowser() (bool, error) {
	var browsers []string
	switch runtime.GOOS {
	case "darwin":
		browsers = []string{"open"}
	case "linux":
		browsers = []string{"xdg-open", "gio open", "firefox", "google-chrome", "chromium"}
	case "windows":
		return false, nil
	}

	for _, browser := range browsers {
		cmd := exec.Command("which", browser)
		if err := cmd.Run(); err == nil {
			return true, nil
		}
	}

	return false, nil
}
