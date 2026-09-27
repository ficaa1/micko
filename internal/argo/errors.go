// errors.go — typed, sanitized error construction for the argo transport
// adapter. The kinds and mapping table are frozen in internal/core
// (docs/development.md; docs/development.md). This file covers the
// transport-specific surface: redaction guarantees and Retry-After.
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
// through it (plan §3; SEC-06).
func redactMessage(s string) string {
	return shared.RedactTokens(sanitizeLine(s))
}

// compile-time guard: KindOf remains the single status→kind table; A1 adds
// no parallel mapping of its own for HTTP statuses.
var _ = core.KindOf
