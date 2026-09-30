package core

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Every HTTP status maps to the kind the UI chooses its state by.
func TestKindOf(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{401, ErrUnauthenticated},
		{403, ErrForbidden},
		{404, ErrNotFound},
		{409, ErrConflict},
		{429, ErrRateLimited},
		{400, ErrInvalid},
		{501, ErrUnsupported},
		{503, ErrUnavailable},
		{504, ErrUnavailable},
		{408, ErrUnavailable},
		{500, ErrUnavailable},
		{200, ErrProtocol},
		{418, ErrProtocol},
	}
	for _, c := range cases {
		if got := KindOf(c.status); got != c.want {
			t.Errorf("KindOf(%d) = %q, want %q", c.status, got, c.want)
		}
	}
}

// An API error prints its kind, message and status, never its wrapped cause.
func TestAPIErrorText(t *testing.T) {
	cases := []struct {
		name string
		err  *APIError
		want string
	}{
		{"with a status", NewAPIError(ErrNotFound, 404, "get ns/wf"), "not_found: get ns/wf (HTTP 404)"},
		{"in-band", NewAPIError(ErrProtocol, 0, "malformed chunk"), "protocol: malformed chunk"},
		{"with a cause", WrapAPIError(ErrUnauthenticated, 401, "get /api/v1/workflows/ns1/w1: unauthorized",
			errors.New("Bearer secret-value")), "unauthenticated: get /api/v1/workflows/ns1/w1: unauthorized (HTTP 401)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
		})
	}
}

// Retry-After reads either form, clamps to zero and ignores what it cannot parse.
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want *time.Duration
	}{
		{"", nil},
		{"garbage", nil},
		{"5", durPtr(5 * time.Second)},
		{"0", durPtr(0)},
		{"-3", durPtr(0)},
		{"30", durPtr(30 * time.Second)},
		{now.Add(10 * time.Second).Format(http.TimeFormat), durPtr(10 * time.Second)},
		{now.Add(-10 * time.Second).Format(http.TimeFormat), durPtr(0)},
	}
	for _, c := range cases {
		got := ParseRetryAfter(c.in, now)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("ParseRetryAfter(%q) = %v, want nil", c.in, *got)
		case c.want != nil && got == nil:
			t.Errorf("ParseRetryAfter(%q) = nil, want %v", c.in, *c.want)
		case c.want != nil && *got != *c.want:
			t.Errorf("ParseRetryAfter(%q) = %v, want %v", c.in, *got, *c.want)
		}
	}
}

func durPtr(d time.Duration) *time.Duration { return &d }

// AsAPIError finds an API error anywhere in a chain and nothing in a plain one.
func TestAsAPIError(t *testing.T) {
	inner := ErrNotFoundf("workflow ns1/name gone")
	if got := AsAPIError(fmt.Errorf("reading: %w", inner)); got != inner {
		t.Errorf("AsAPIError through a wrap = %v, want %v", got, inner)
	}
	if got := AsAPIError(errors.New("boom")); got != nil {
		t.Errorf("AsAPIError(plain) = %v, want nil", got)
	}
}
