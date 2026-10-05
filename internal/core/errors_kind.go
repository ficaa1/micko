package core

import (
	"fmt"
	"net/http"
)

func ErrUnavailablef(format string, args ...any) *APIError {
	return NewAPIError(ErrUnavailable, http.StatusServiceUnavailable, fmt.Sprintf(format, args...))
}

func ErrInvalidf(format string, args ...any) *APIError {
	return NewAPIError(ErrInvalid, http.StatusBadRequest, fmt.Sprintf(format, args...))
}

func ErrProtocalf(format string, args ...any) *APIError {
	return NewAPIError(ErrProtocol, 0, fmt.Sprintf(format, args...))
}

func ErrConflictf(format string, args ...any) *APIError {
	return NewAPIError(ErrConflict, http.StatusConflict, fmt.Sprintf(format, args...))
}
