package core

import (
	"fmt"
	"net/http"
	"time"
)

// Convenience constructors for the remaining kinds, mirroring the
// errors.go trio so call sites stay consistent across transport/fakes.

func ErrUnavailablef(format string, args ...any) *APIError {
	return NewAPIError(ErrUnavailable, http.StatusServiceUnavailable, fmt.Sprintf(format, args...))
}

func ErrRateLimitedf(retryAfter *time.Duration, format string, args ...any) *APIError {
	e := NewAPIError(ErrRateLimited, http.StatusTooManyRequests, fmt.Sprintf(format, args...))
	e.RetryAfter = retryAfter
	return e
}

func ErrInvalidf(format string, args ...any) *APIError {
	return NewAPIError(ErrInvalid, http.StatusBadRequest, fmt.Sprintf(format, args...))
}

func ErrUnsupportedf(format string, args ...any) *APIError {
	return NewAPIError(ErrUnsupported, http.StatusNotImplemented, fmt.Sprintf(format, args...))
}

func ErrProtocalf(format string, args ...any) *APIError {
	return NewAPIError(ErrProtocol, 0, fmt.Sprintf(format, args...))
}

func ErrConflictf(format string, args ...any) *APIError {
	return NewAPIError(ErrConflict, http.StatusConflict, fmt.Sprintf(format, args...))
}
