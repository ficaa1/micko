package core

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ErrorKind classifies a server/transport failure for UI state selection
// (plan §4; mapping table in docs/protocol.md §9).
type ErrorKind string

const (
	// ErrUnauthenticated: HTTP 401 / auth challenge. Do not retry; surface
	// token guidance (plan §3).
	ErrUnauthenticated ErrorKind = "unauthenticated"
	// ErrForbidden: HTTP 403, authoritative; action availability is only
	// advisory (plan §3).
	ErrForbidden ErrorKind = "forbidden"
	// ErrNotFound: live miss + archive miss (docs/protocol.md §5).
	ErrNotFound ErrorKind = "not_found"
	// ErrConflict: HTTP 409 (AlreadyExists); not expected on reads.
	ErrConflict ErrorKind = "conflict"
	// ErrRateLimited: HTTP 429. Also the watch EOF→429 quirk
	// (docs/protocol.md §6.1) — context-dependent.
	ErrRateLimited ErrorKind = "rate_limited"
	// ErrUnavailable: 503/504/408 or network-level failure; transient,
	// bounded backoff ok (plan §5).
	ErrUnavailable ErrorKind = "unavailable"
	// ErrInvalid: HTTP 400, includes bad grep regex / bad timestamps
	// (docs/protocol.md §6.2).
	ErrInvalid ErrorKind = "invalid"
	// ErrUnsupported: HTTP 501, feature disabled server-side.
	ErrUnsupported ErrorKind = "unsupported"
	// ErrProtocol: malformed/unparseable wire data on an otherwise-200
	// response (docs/protocol.md §9).
	ErrProtocol ErrorKind = "protocol"
)

// APIError is the frozen typed error returned by every Reader failure.
// Message is already sanitized: it must never contain credentials, and it
// names the affected resource/endpoint, not the token source secret value
// (plan §3: error messages identify the source variable/file, never the
// secret).
type APIError struct {
	Kind    ErrorKind
	Message string
	// Status is the HTTP status when one applied; 0 for in-band stream
	// errors and pure transport failures (docs/protocol.md §9).
	Status int
	// RetryAfter is the server-requested delay from a Retry-After header
	// (seconds form or HTTP-date form); nil when absent.
	RetryAfter *time.Duration
	// Err is the wrapped cause, if any. Its Error() is never included
	// automatically — Message stays the single sanitized surface.
	Err error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Status != 0 {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Kind, e.Message, e.Status)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *APIError) Unwrap() error { return e.Err }

// NewAPIError builds an APIError with a sanitized message.
func NewAPIError(kind ErrorKind, status int, message string) *APIError {
	return &APIError{Kind: kind, Status: status, Message: message}
}

// WrapAPIError attaches a cause; the cause is never printed by Error().
func WrapAPIError(kind ErrorKind, status int, message string, cause error) *APIError {
	return &APIError{Kind: kind, Status: status, Message: message, Err: cause}
}

// KindOf maps an HTTP status to the frozen ErrorKind table
// (docs/protocol.md §9; gateway round-trip is identity for our cases).
func KindOf(status int) ErrorKind {
	switch status {
	case http.StatusUnauthorized:
		return ErrUnauthenticated
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusBadRequest:
		return ErrInvalid
	case http.StatusNotImplemented:
		return ErrUnsupported
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		http.StatusRequestTimeout:
		return ErrUnavailable
	default:
		if status >= 500 {
			return ErrUnavailable
		}
		return ErrProtocol
	}
}

// ParseRetryAfter interprets a Retry-After header value (delay-seconds or
// HTTP-date). It returns nil for empty/invalid input; negative results are
// clamped to zero. Parsing never fails a request.
func ParseRetryAfter(v string, now time.Time) *time.Duration {
	if v == "" {
		return nil
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		d := time.Duration(secs) * time.Second
		return &d
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return &d
	}
	return nil
}

// Convenience constructors used by transport and fake implementations so
// kinds stay consistent across the codebase.

func ErrUnauthenticatedf(format string, args ...any) *APIError {
	return NewAPIError(ErrUnauthenticated, http.StatusUnauthorized, fmt.Sprintf(format, args...))
}

func ErrForbiddenf(format string, args ...any) *APIError {
	return NewAPIError(ErrForbidden, http.StatusForbidden, fmt.Sprintf(format, args...))
}

func ErrNotFoundf(format string, args ...any) *APIError {
	return NewAPIError(ErrNotFound, http.StatusNotFound, fmt.Sprintf(format, args...))
}

// AsAPIError extracts an *APIError from an error chain, else nil.
func AsAPIError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}
