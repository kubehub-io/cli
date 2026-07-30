package v202607

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type errorResponse struct {
	Error *OperationError `json:"error,omitempty"`
}

func ParseErrorResponse(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.Status
	}

	var apiErr errorResponse
	if err := json.Unmarshal(body, &apiErr); err != nil || apiErr.Error == nil || apiErr.Error.Message == nil || *apiErr.Error.Message == "" {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return resp.Status
		}
		return msg
	}

	return fmt.Sprintf("%s", *apiErr.Error.Message)
}
