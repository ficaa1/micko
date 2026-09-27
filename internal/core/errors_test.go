package core

import (
	"net/http"
	"testing"
	"time"
)

func TestKindOfFrozenTable(t *testing.T) {
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

func TestAPIErrorMessageSanitized(t *testing.T) {
	// The message surface must never contain credential material; these
	// constructors carry what the caller gives them, so the test pins the
	// discipline: callers pass resource/endpoint descriptions, and Error()
	// must not append wrapped causes (which could smuggle secrets in).
	e := WrapAPIError(ErrUnauthenticated, 401, "get /api/v1/workflows/ns1/w1: unauthorized", nil)
	got := e.Error()
	for _, bad := range []string{"MICKO_TOKEN", "secret-value"} {
		if contains(got, bad) {
			t.Fatalf("APIError.Error() contains %q: %q", bad, got)
		}
	}
	if got != "unauthenticated: get /api/v1/workflows/ns1/w1: unauthorized (HTTP 401)" {
		t.Fatalf("unexpected format: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

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
		{"-3", durPtr(0)}, // clamped
		{"30", durPtr(30 * time.Second)},
		// HTTP-date form: 10s in the future
		{now.Add(10 * time.Second).Format(http.TimeFormat), durPtr(10 * time.Second)},
		// HTTP-date in the past clamps to zero
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

func TestAsAPIError(t *testing.T) {
	inner := ErrNotFoundf("workflow ns1/name gone")
	wrapped := errWrap{inner}
	if got := AsAPIError(wrapped); got != inner {
		t.Errorf("AsAPIError through wrap = %v, want %v", got, inner)
	}
	if got := AsAPIError(errPlain{"boom"}); got != nil {
		t.Errorf("AsAPIError(plain) = %v, want nil", got)
	}
}

type errWrap struct{ error }

func (w errWrap) Unwrap() error { return w.error }

type errPlain struct{ msg string }

func (e errPlain) Error() string { return e.msg }
