package argo

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// NewClient refuses a server URL that could leak credentials or is not
// plain HTTP on loopback or HTTPS.
func TestNewClientRefusesUnsafeURLs(t *testing.T) {
	cases := []struct {
		server  string
		wantErr bool
	}{
		{"https://argo.example.test/argo", false},
		{"http://127.0.0.1:2746", false},
		{"http://localhost:2746", false},
		{"http://argo.example.test", true},
		{"https://user:pass@argo.example.test", true},
		{"https://argo.example.test/?token=abc", true},
		{"ftp://argo.example.test", true},
		{"", true},
	}
	for _, c := range cases {
		_, err := NewClient(Options{Server: c.server, TokenFn: func() (string, error) { return "t", nil }})
		if (err != nil) != c.wantErr {
			t.Errorf("NewClient(%q) err = %v, want error %v", c.server, err, c.wantErr)
		}
	}
}

// Every request lands under the configured base path with the bearer token
// and user agent, and sends no Accept header.
func TestRequestsCarryTheBasePathAndCredentials(t *testing.T) {
	var got *http.Request
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		switch {
		case strings.HasSuffix(r.URL.Path, "/log"):
		case strings.HasSuffix(r.URL.Path, "/workflows/ns-a"):
			_, _ = io.WriteString(w, `{"metadata":{},"items":[]}`)
		default:
			_, _ = io.WriteString(w, `{"metadata":{"name":"wf","namespace":"ns-a","uid":"u1"},"status":{}}`)
		}
	})
	ref := core.Ref{Namespace: "ns-a", Name: "wf", UID: "u1"}
	cases := []struct {
		name             string
		token, userAgent string
		call             func(*Client) error
		method, path     string
		query            url.Values
		wantAuth, wantUA string
	}{
		{
			name: "list", token: testToken, userAgent: "micko/9.9.9",
			call: func(c *Client) error {
				_, err := c.List(context.Background(), core.Query{Namespace: "ns-a", LabelSelector: "a=b", Continue: "5", Limit: 3})
				return err
			},
			method: http.MethodGet, path: "/argo/api/v1/workflows/ns-a",
			query: url.Values{
				"listOptions.continue": {"5"}, "listOptions.labelSelector": {"a=b"}, "listOptions.limit": {"3"},
				"fields": {listFields},
			},
			wantAuth: "Bearer " + testToken, wantUA: "micko/9.9.9",
		},
		{
			name: "get", token: testToken, userAgent: "micko/9.9.9",
			call: func(c *Client) error {
				_, err := c.Get(context.Background(), ref)
				return err
			},
			method: http.MethodGet, path: "/argo/api/v1/workflows/ns-a/wf", query: url.Values{"uid": {"u1"}},
			wantAuth: "Bearer " + testToken, wantUA: "micko/9.9.9",
		},
		{
			name: "workflow logs", token: testToken, userAgent: "micko/9.9.9",
			call: func(c *Client) error {
				return c.StreamLogs(context.Background(), core.LogRequest{Ref: ref, Container: "main", Follow: true, TailLines: 50, Timestamps: true},
					func(core.LogRecord) error { return nil })
			},
			method: http.MethodGet, path: "/argo/api/v1/workflows/ns-a/wf/log",
			query: url.Values{
				"podName": {""}, "logOptions.container": {"main"}, "logOptions.follow": {"true"},
				"logOptions.tailLines": {"50"}, "logOptions.timestamps": {"true"},
			},
			wantAuth: "Bearer " + testToken, wantUA: "micko/9.9.9",
		},
		{
			name: "pod logs", token: testToken, userAgent: "micko/9.9.9",
			call: func(c *Client) error {
				return c.StreamLogs(context.Background(), core.LogRequest{Ref: ref, PodName: "wf-pod-1", Container: "wait"},
					func(core.LogRecord) error { return nil })
			},
			method: http.MethodGet, path: "/argo/api/v1/workflows/ns-a/wf/wf-pod-1/log",
			query:    url.Values{"podName": {"wf-pod-1"}, "logOptions.container": {"wait"}},
			wantAuth: "Bearer " + testToken, wantUA: "micko/9.9.9",
		},
		{
			name: "action", token: testToken, userAgent: "micko/9.9.9",
			call: func(c *Client) error {
				_, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionResume, Ref: ref, Confirmation: core.Confirmation{Confirmed: true}})
				return err
			},
			method: http.MethodPut, path: "/argo/api/v1/workflows/ns-a/wf/resume", query: url.Values{},
			wantAuth: "Bearer " + testToken, wantUA: "micko/9.9.9",
		},
		{
			name: "no token and no user agent",
			call: func(c *Client) error {
				_, err := c.Get(context.Background(), ref)
				return err
			},
			method: http.MethodGet, path: "/argo/api/v1/workflows/ns-a/wf", query: url.Values{"uid": {"u1"}},
			wantAuth: "", wantUA: "micko",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := newClientWith(t, Options{
				Server:    srv.URL + "/argo",
				TokenFn:   func() (string, error) { return c.token, nil },
				UserAgent: c.userAgent,
			})
			if err := c.call(cl); err != nil {
				t.Fatal(err)
			}
			if got.Method != c.method || got.URL.Path != c.path || got.URL.RawQuery != c.query.Encode() {
				t.Errorf("request = %s %s?%s, want %s %s?%s", got.Method, got.URL.Path, got.URL.RawQuery, c.method, c.path, c.query.Encode())
			}
			if a := got.Header.Get("Authorization"); a != c.wantAuth {
				t.Errorf("Authorization = %q, want %q", a, c.wantAuth)
			}
			if ua := got.Header.Get("User-Agent"); ua != c.wantUA {
				t.Errorf("User-Agent = %q, want %q", ua, c.wantUA)
			}
			if a := got.Header.Get("Accept"); a != "" {
				t.Errorf("Accept = %q, want none", a)
			}
		})
	}
}

// A redirect is refused as a protocol error, and the credential never
// reaches its target.
func TestRedirectIsRefused(t *testing.T) {
	var reached bool
	target := serve(t, func(w http.ResponseWriter, r *http.Request) { reached = true })
	origin := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	})
	_, err := newTestClient(t, origin).List(context.Background(), core.Query{Namespace: "ns-a"})
	ae := wantAPIError(t, err, core.ErrProtocol, "redirect")
	if reached {
		t.Error("the request followed the redirect")
	}
	if strings.Contains(ae.Message, target.URL) || strings.Contains(err.Error(), testToken) {
		t.Errorf("message = %q, want neither the target nor the token", ae.Message)
	}
}

// TLS is verified unless a CA file or the opt-out says otherwise, and only
// an https failure is reported as TLS.
func TestTLSVerification(t *testing.T) {
	tlsSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"metadata":{},"items":[]}`)
	}))
	tlsSrv.Config.ErrorLog = log.New(io.Discard, "", 0)
	tlsSrv.StartTLS()
	defer tlsSrv.Close()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsSrv.Certificate().Raw})
	if err := os.WriteFile(caPath, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(nil)
	down.Close()

	cases := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{"self-signed without a CA", Options{Server: tlsSrv.URL}, "TLS handshake"},
		{"CA file", Options{Server: tlsSrv.URL, CAFile: caPath}, ""},
		{"verification off", Options{Server: tlsSrv.URL, InsecureSkipTLSVerify: true}, ""},
		{"plain http server down", Options{Server: down.URL}, "server unreachable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := newClientWith(t, c.opts).List(context.Background(), core.Query{Namespace: "ns-a"})
			if c.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantAPIError(t, err, core.ErrUnavailable, c.wantErr)
		})
	}
}

// A body the server does not finish ends the request as unavailable, not as
// a protocol mismatch.
func TestAnUnfinishedBodyIsUnavailable(t *testing.T) {
	cases := []struct {
		name    string
		timeout time.Duration
		handler http.HandlerFunc
	}{
		{"stalled", 200 * time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		{"truncated", 0, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[`))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			cl := newClientWith(t, Options{Server: srv.URL, RequestTimeout: c.timeout})
			done := make(chan error, 1)
			go func() {
				_, err := cl.List(context.Background(), core.Query{Namespace: "ns"})
				done <- err
			}()
			select {
			case err := <-done:
				wantAPIError(t, err, core.ErrUnavailable, "reading the response body failed")
			case <-time.After(5 * time.Second):
				t.Fatal("the request never ended")
			}
		})
	}
}

// A refused request maps to the kind of its status with the server's
// message, redacted, and a 429 carries its Retry-After.
func TestErrorsMapByStatus(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		kind       core.ErrorKind
		msg        string
		wantRetry  time.Duration
	}{
		{"forbidden", 403, "", `{"code":7,"message":"workflows.argoproj.io is forbidden: cannot list in ns-a"}`,
			core.ErrForbidden, "cannot list in ns-a", -1},
		{"token echoed back", 401, "", `{"code":16,"message":"rejected Bearer ` + testToken + `"}`,
			core.ErrUnauthenticated, "rejected Bearer [REDACTED]", -1},
		{"error field only", 404, "", `{"error":"no such workflow"}`, core.ErrNotFound, "no such workflow", -1},
		{"no message", 503, "", `upstream down`, core.ErrUnavailable, "GET /api/v1/workflows/ns-a failed", -1},
		{"login page", 200, "", `<html><body><form>Sign in</form></body></html>`, core.ErrUnauthenticated, "HTML page", -1},
		{"retry after seconds", 429, "30", `{"code":8,"message":"throttled"}`, core.ErrRateLimited, "throttled", 30 * time.Second},
		{"retry after date", 429, now.Add(2 * time.Minute).Format(http.TimeFormat), `{"code":8,"message":"throttled"}`,
			core.ErrRateLimited, "throttled", 2 * time.Minute},
		{"no retry after", 429, "", `{"code":8,"message":"throttled"}`, core.ErrRateLimited, "throttled", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if c.retryAfter != "" {
					w.Header().Set("Retry-After", c.retryAfter)
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			cl := newClientWith(t, Options{Server: srv.URL, Now: func() time.Time { return now }})
			_, err := cl.List(context.Background(), core.Query{Namespace: "ns-a"})
			ae := wantAPIError(t, err, c.kind, c.msg)
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %v", err)
			}
			switch {
			case c.wantRetry < 0 && ae.RetryAfter != nil:
				t.Errorf("RetryAfter = %v, want none", *ae.RetryAfter)
			case c.wantRetry >= 0 && (ae.RetryAfter == nil || *ae.RetryAfter != c.wantRetry):
				t.Errorf("RetryAfter = %v, want %v", ae.RetryAfter, c.wantRetry)
			}
		})
	}
}

// Requests go to the host and port the resolver names, keeping the configured
// scheme and path, and nowhere when it names none.
func TestRequestsGoToTheResolvedEndpoint(t *testing.T) {
	list := func(c *Client) error {
		_, err := c.List(context.Background(), core.Query{Namespace: "ns"})
		return err
	}
	resume := func(c *Client) error {
		_, err := c.Execute(context.Background(), core.ActionRequest{
			Action: core.ActionResume, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Confirmation: core.Confirmation{Confirmed: true},
		})
		return err
	}
	cases := []struct {
		name  string
		moved bool
		call  func(*Client) error
	}{
		{"list on the moved port", true, list},
		{"action on the moved port", true, resume},
		{"list with no endpoint", false, list},
		{"action with no endpoint", false, resume},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var startHits, movedHits int
			start := serve(t, func(w http.ResponseWriter, r *http.Request) { startHits++ })
			moved := serve(t, func(w http.ResponseWriter, r *http.Request) {
				movedHits++
				_, _ = io.WriteString(w, `{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"items":[]}`)
			})
			endpoint := ""
			if c.moved {
				endpoint = moved.URL
			}
			err := c.call(newClientWith(t, Options{Server: start.URL, ResolveServer: func() string { return endpoint }}))
			if startHits != 0 {
				t.Fatal("a request went to the port the client started on")
			}
			if !c.moved {
				wantAPIError(t, err, core.ErrUnavailable, "not available")
				return
			}
			if err != nil || movedHits != 1 {
				t.Fatalf("err = %v, requests to the moved port = %d", err, movedHits)
			}
		})
	}

	// A forward announces a plain-http loopback address; only its host and
	// port are adopted, never its scheme or its missing path.
	t.Run("only the host and port move", func(t *testing.T) {
		var path string
		moved := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			_, _ = io.WriteString(w, `{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"status":{}}`)
		}))
		t.Cleanup(moved.Close)
		cl := newClientWith(t, Options{
			Server:                "https://argo.example.invalid/argo",
			InsecureSkipTLSVerify: true,
			ResolveServer:         func() string { return "http://" + strings.TrimPrefix(moved.URL, "https://") },
		})
		if _, err := cl.Get(context.Background(), core.Ref{Namespace: "ns", Name: "wf", UID: "u"}); err != nil {
			t.Fatalf("request over the configured https scheme failed: %v", err)
		}
		if path != "/argo/api/v1/workflows/ns/wf" {
			t.Errorf("path = %q, want it under the configured /argo prefix", path)
		}
	})
}

// A credential source that fails refuses the request as unauthenticated,
// naming the source and sending nothing.
func TestAFailingCredentialSourceSendsNothing(t *testing.T) {
	cases := []struct {
		name string
		call func(*Client) error
	}{
		{"list", func(c *Client) error {
			_, err := c.List(context.Background(), core.Query{Namespace: "ns"})
			return err
		}},
		{"action", func(c *Client) error {
			_, err := c.Execute(context.Background(), core.ActionRequest{
				Action: core.ActionResume, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Confirmation: core.Confirmation{Confirmed: true},
			})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) { t.Error("a request was sent without credentials") })
			cl := newClientWith(t, Options{
				Server:      srv.URL,
				TokenFn:     func() (string, error) { return "", errors.New("token file unreadable") },
				TokenSource: "token file /etc/argo/token",
			})
			wantAPIError(t, c.call(cl), core.ErrUnauthenticated, "reading credentials from token file /etc/argo/token: token file unreadable")
		})
	}
}
