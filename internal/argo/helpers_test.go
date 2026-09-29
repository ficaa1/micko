package argo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// testToken is the credential every test client sends.
const testToken = "test-token-abcdef12345678901234"

// newTestClient is a client for srv with the test token.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return newClientWith(t, Options{Server: srv.URL})
}

// newClientWith builds a client from opts, with the test token when opts
// names no credential source.
func newClientWith(t *testing.T, opts Options) *Client {
	t.Helper()
	if opts.TokenFn == nil {
		opts.TokenFn = func() (string, error) { return testToken, nil }
	}
	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// serve starts a test server that closes with the test.
func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// serveBody answers every request with status and body.
func serveBody(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// loadFixture reads a file from testdata.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// isGateScan reports whether r is the scan that fills the Suspend marker
// rather than the list itself.
func isGateScan(r *http.Request) bool {
	return strings.Contains(r.URL.Query().Get("fields"), "items.status.nodes")
}

// serveNoGates answers a gate scan with no workflows in flight.
func serveNoGates(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"metadata":{},"items":[]}`))
}

// wantAPIError fails unless err is an APIError of kind whose message
// contains msg.
func wantAPIError(t *testing.T, err error, kind core.ErrorKind, msg string) *core.APIError {
	t.Helper()
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != kind || !strings.Contains(ae.Message, msg) {
		t.Fatalf("err = %v, want %s containing %q", err, kind, msg)
	}
	return ae
}
