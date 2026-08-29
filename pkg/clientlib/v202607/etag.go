package v202607

import (
	"context"
	"net/http"
)

// WithIfMatch returns a RequestEditorFn that sets the If-Match header used by
// the API for optimistic concurrency control. Passing an empty etag is a no-op
// so callers can use it unconditionally without special-casing a missing etag.
//
// The server requires If-Match on mutating requests (PUT/DELETE) and responds
// with PRECONDITION_REQUIRED when it is absent, so every update/delete should
// go through this editor.
func WithIfMatch(etag string) RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		return nil
	}
}
