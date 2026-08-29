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

// ParseError decodes the API error payload (code/message) from a response, or
// returns nil when the body does not contain a recognizable error.
func ParseError(resp *http.Response) *OperationError {
	if resp == nil {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var apiErr errorResponse
	if err := json.Unmarshal(body, &apiErr); err != nil || apiErr.Error == nil {
		return nil
	}

	return apiErr.Error
}
