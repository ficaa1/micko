package argo

import (
	"net/http"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// retryAfterOf extracts a typed Retry-After from the response headers
// (delay-seconds or HTTP-date; invalid/absent ⇒ nil, never fails the call).
func retryAfterOf(resp *http.Response, now func() time.Time) *time.Duration {
	if resp == nil {
		return nil
	}
	return core.ParseRetryAfter(resp.Header.Get("Retry-After"), now())
}

// redactMessage applies defense-in-depth token redaction to a message that
// may embed server-provided strings (which can, in compromised setups,
// echo back request material). Every message this package surfaces goes
// through it.
func redactMessage(s string) string {
	return shared.RedactTokens(sanitizeLine(s))
}
