package argo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"argo-tui/internal/core"
)

// Only the header timeout was bounded, so a server that flushed headers and
// then stalled the body held the refresh open with no way out but quitting.
func TestAStalledBodyEndsTheRequest(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	// Close runs last: the handler has to be released before the server
	// waits for it, or the test deadlocks on its own fixture.
	defer srv.Close()
	defer close(release)

	c, err := NewClient(Options{
		Server:         srv.URL,
		TokenFn:        func() (string, error) { return "t", nil },
		RequestTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.List(context.Background(), core.Query{Namespace: "ns"})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stalled body returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled body held the request open")
	}
}

// A body cut short by a dead connection leaves valid-looking bytes. Dropping
// the read error reported it as a protocol mismatch, which sends the reader
// after a fault that never happened.
func TestATruncatedBodyIsReportedAsATransportFault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[`))
		w.(http.Flusher).Flush()
		// Returning now closes the connection with the body incomplete.
	}))
	defer srv.Close()

	c, err := NewClient(Options{Server: srv.URL, TokenFn: func() (string, error) { return "t", nil }})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = c.List(context.Background(), core.Query{Namespace: "ns"})
	if err == nil {
		t.Fatal("a truncated body returned no error")
	}
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrUnavailable {
		t.Fatalf("error = %v, want an unavailable API error, not a protocol one", err)
	}
}
