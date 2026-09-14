package argo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"argo-tui/internal/core"
)

// TestUserAgentComesFromOptions asserts the header comes from
// Options.UserAgent on both request paths, which build requests separately:
// newRequest (reads) and Execute (mutations).
func TestUserAgentComesFromOptions(t *testing.T) {
	const want = "argo-tui/9.9.9"

	newServer := func(seen chan<- string, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case seen <- r.Header.Get("User-Agent"):
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
	}

	newClient := func(t *testing.T, srv *httptest.Server) *Client {
		t.Helper()
		c, err := NewClient(Options{
			Server:    srv.URL,
			TokenFn:   func() (string, error) { return "test-token-abcdef12345678901234", nil },
			Now:       time.Now,
			UserAgent: want,
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		return c
	}

	t.Run("read path", func(t *testing.T) {
		seen := make(chan string, 1)
		srv := newServer(seen, `{"items":[],"metadata":{"resourceVersion":"1"}}`)
		defer srv.Close()

		if _, err := newClient(t, srv).List(context.Background(), core.Query{Namespace: "ns"}); err != nil {
			t.Fatalf("List: %v", err)
		}
		if ua := <-seen; ua != want {
			t.Errorf("User-Agent = %q, want %q", ua, want)
		}
	})

	t.Run("action path", func(t *testing.T) {
		seen := make(chan string, 1)
		srv := newServer(seen, `{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"status":{"phase":"Running"}}`)
		defer srv.Close()

		_, _ = newClient(t, srv).Execute(context.Background(), core.ActionRequest{
			Action:       core.ActionResume,
			Ref:          core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
			Confirmation: core.Confirmation{Confirmed: true, TypedName: "wf"},
		})
		if ua := <-seen; ua != want {
			t.Errorf("User-Agent = %q, want %q", ua, want)
		}
	})

	t.Run("empty option falls back without a version", func(t *testing.T) {
		c, err := NewClient(Options{
			Server:  "https://example.invalid",
			TokenFn: func() (string, error) { return "t", nil },
		})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if c.userAgent != defaultUserAgent {
			t.Errorf("userAgent = %q, want %q", c.userAgent, defaultUserAgent)
		}
		if defaultUserAgent != "argo-tui" {
			t.Errorf("defaultUserAgent = %q, want a version-free value", defaultUserAgent)
		}
	})
}
