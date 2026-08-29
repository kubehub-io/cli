package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	v202607 "github.com/kubehub-io/kubehubcli/pkg/clientlib/v202607"
	"github.com/kubehub-io/kubehubcli/pkg/kubehubcli"
	"github.com/spf13/cobra"
)

func withBearerToken(token string) v202607.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func getAuthenticatedClient(ctx context.Context, cfg *kubehubcli.Config) (*v202607.Client, string, error) {
	auth := kubehubcli.NewAuthenticator(cfg.Issuer, cfg.ClientID).WithVerbose(verbose)
	token, err := auth.Authenticate(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("authentication: %w", err)
	}

	client, err := v202607.NewClient(cfg.Server)
	if err != nil {
		return nil, "", fmt.Errorf("create client: %w", err)
	}

	return client, token, nil
}

func errorExit(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// errorExitAPI logs a structured error for a non-2xx API response, surfacing
// the server-provided error code and message, then exits.
func errorExitAPI(op string, resp *http.Response) {
	apiErr := v202607.ParseError(resp)
	if apiErr != nil {
		attrs := []any{slog.Int("status", resp.StatusCode)}
		if apiErr.Code != nil {
			attrs = append(attrs, slog.String("code", *apiErr.Code))
		}
		msg := ""
		if apiErr.Message != nil {
			msg = *apiErr.Message
		}
		slog.Error(op, attrs...)
		errorExit("%s: %s", op, msg)
	}
	errorExit("%s: status %d", op, resp.StatusCode)
}

func getMapFlag(cmd *cobra.Command, name string) map[string]string {
	vals, _ := cmd.Flags().GetStringArray(name)
	m := make(map[string]string, len(vals))
	for _, v := range vals {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
