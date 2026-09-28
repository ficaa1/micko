package argo

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// newTestClient builds a Client pointed at the given test server. The
// credential source returns a fixed token so tests can assert on the exact
// Authorization header (and its absence from error strings). passBase
// supplies the configured server base URL that must be preserved verbatim
// (base-path slice: the Argo paths are appended, never rebuilt).
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(Options{
		Server:  srv.URL,
		TokenFn: func() (string, error) { return "test-token-abcdef12345678901234", nil },
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// --- Slice 1: client transport ------------------------------------------------

// TestClientBasePathAndCredentials verifies base-path preservation, exact
// endpoint paths, query encoding (limit/continue/uid always) and the
// Authorization header (plan slice 1; CONN-07/09, SEC-06).
func TestClientBasePathAndCredentials(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotAccept string
	var listCalls int32
	// The handler accepts BOTH the bare root (httptest URL as-is) and the
	// /argo-prefixed mount; assertions below pin that the client appended
	// the Argo path to the configured base verbatim (no rebuild, no trim).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		switch {
		case strings.HasSuffix(r.URL.Path, "/log"):
			// Minimal stream so StreamLogs succeeds.
			io.WriteString(w, "")
			return
		case strings.HasSuffix(r.URL.Path, "/workflows/ns-a"):
			atomic.AddInt32(&listCalls, 1)
			io.WriteString(w, `{"metadata":{"continue":"","resourceVersion":"rv"},"items":[]}`)
			return
		default:
			io.WriteString(w, `{"metadata":{"name":"wf","namespace":"ns-a","uid":"u1"},"status":{}}`)
		}
	}))
	defer srv.Close()

	// The client is configured with a /argo path prefix (as behind a
	// rewriting reverse proxy); the endpoint paths must land under it.
	c, err := NewClient(Options{
		Server:  srv.URL + "/argo",
		TokenFn: func() (string, error) { return "test-token-abcdef12345678901234", nil },
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// List: base path with prefix must be preserved verbatim; pagination
	// params encoded with exact Argo names.
	page, listErr := c.List(context.Background(), core.Query{
		Namespace:     "ns-a",
		LabelSelector: "workflows.argoproj.io/phase=Running",
		Continue:      "5",
		Limit:         3,
	})
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	_ = page
	if want := "/argo/api/v1/workflows/ns-a"; gotPath != want {
		t.Errorf("list path = %q, want %q", gotPath, want)
	}
	// Expected query built with net/url so escaping is not hand-asserted.
	q := url.Values{}
	q.Set("listOptions.continue", "5")
	q.Set("listOptions.labelSelector", "workflows.argoproj.io/phase=Running")
	q.Set("listOptions.limit", "3")
	q.Set("fields", listFields)
	if gotQuery != q.Encode() {
		t.Errorf("list query = %q, want %q", gotQuery, q.Encode())
	}
	if want := "Bearer test-token-abcdef12345678901234"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	// Accept: text/event-stream must NOT be sent (docs/development.md).
	if gotAccept != "" {
		t.Errorf("Accept header sent: %q (must not send SSE hint)", gotAccept)
	}

	// Detail: uid always passed so the server can fall back to the archive
	// (docs/development.md).
	gotPath, gotQuery, gotAuth = "", "", ""
	if _, getErr := c.Get(context.Background(), core.Ref{Namespace: "ns-a", Name: "wf", UID: "u1"}); getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if want := "/argo/api/v1/workflows/ns-a/wf"; gotPath != want {
		t.Errorf("get path = %q, want %q", gotPath, want)
	}
	if want := "uid=u1"; gotQuery != want {
		t.Errorf("get query = %q, want %q", gotQuery, want)
	}
	if want := "Bearer test-token-abcdef12345678901234"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}

	// Logs: workflow-wide route, container always explicit, follow flag
	// encoded when requested.
	gotPath, gotQuery = "", ""
	if logErr := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:        core.Ref{Namespace: "ns-a", Name: "wf", UID: "u1"},
		Container:  "main",
		Follow:     true,
		TailLines:  50,
		Timestamps: true,
	}, func(core.LogRecord) error { return nil }); logErr != nil {
		t.Fatalf("StreamLogs: %v", logErr)
	}
	if want := "/argo/api/v1/workflows/ns-a/wf/log"; gotPath != want {
		t.Errorf("log path = %q, want %q", gotPath, want)
	}
	lq := url.Values{}
	lq.Set("logOptions.container", "main")
	lq.Set("logOptions.follow", "true")
	lq.Set("logOptions.tailLines", "50")
	lq.Set("logOptions.timestamps", "true")
	lq.Set("podName", "")
	if gotQuery != lq.Encode() {
		t.Errorf("log query = %q, want %q", gotQuery, lq.Encode())
	}
}

// TestClientRejectsRedirect verifies no credentials are ever sent to a
// redirect target and the response is explained as a typed error
// (plan slice 1; CONN-05, SEC-07).
func TestClientRejectsRedirect(t *testing.T) {
	var redirected int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only a redirect HOP (second request the client itself made)
		// counts as a credential leak; the original 302 answer carries the
		// Authorization header by design.
		if r.Header.Get("X-Hop") == "1" && r.Header.Get("Authorization") != "" {
			atomic.AddInt32(&redirected, 1)
		}
		w.Header().Set("Location", "https://evil.example/steal")
		w.Header().Set("X-Hop", "1")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.List(context.Background(), core.Query{Namespace: "ns-a"})
	if err == nil {
		t.Fatal("List: expected redirect rejection error, got nil")
	}
	// The client must never issue the second hop itself: with redirects
	// rejected there is exactly one request, so X-Hop cannot legitimately
	// appear on any request carrying Authorization.
	if atomic.LoadInt32(&redirected) != 0 {
		t.Error("credentials were sent to the redirect target")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrProtocol {
		t.Errorf("kind = %q, want protocol", ae.Kind)
	}
	if !strings.Contains(ae.Message, "redirect") {
		t.Errorf("message %q does not explain the redirect", ae.Message)
	}
	if strings.Contains(ae.Message, "test-token") || strings.Contains(ae.Message, "abcdef12345678901234") {
		t.Errorf("error message leaks token: %q", ae.Message)
	}
	// The redirect Location must not be echoed verbatim into the message.
	if strings.Contains(ae.Message, "evil.example") {
		t.Errorf("error message echoes redirect target: %q", ae.Message)
	}
}

// TestClientTLS verifies the custom-CA path (httptest TLS server) and the
// insecure-verify override (plan slice 1; CONN-02/03).
func TestClientTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"metadata":{"continue":"","resourceVersion":"rv"},"items":[]}`)
	}))
	defer srv.Close()

	// a) Without the CA, TLS verification fails with a typed error.
	cBad, err := NewClient(Options{
		Server:  srv.URL,
		TokenFn: func() (string, error) { return "tok", nil },
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = cBad.List(context.Background(), core.Query{Namespace: "ns-a"})
	if err == nil {
		t.Fatal("expected TLS verification failure without CA, got nil")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrUnavailable {
		t.Errorf("kind = %q, want unavailable", ae.Kind)
	}
	if !strings.Contains(strings.ToLower(ae.Message), "tls") &&
		!strings.Contains(strings.ToLower(ae.Message), "certificate") {
		t.Errorf("message %q does not name the TLS problem", ae.Message)
	}

	// b) With the server's CA exported to a file, the request succeeds.
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	der := srv.Certificate().Raw
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write ca.pem: %v", err)
	}

	cGood, err := NewClient(Options{
		Server:  srv.URL,
		CAFile:  caPath,
		TokenFn: func() (string, error) { return "tok", nil },
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient with CA: %v", err)
	}
	if _, err := cGood.List(context.Background(), core.Query{Namespace: "ns-a"}); err != nil {
		t.Errorf("List with CA: %v", err)
	}

	// c) Explicit insecure override skips verification (CONN-02), with the
	// client still usable.
	cInsecure, err := NewClient(Options{
		Server:                srv.URL,
		InsecureSkipTLSVerify: true,
		TokenFn:               func() (string, error) { return "tok", nil },
		Now:                   time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient insecure: %v", err)
	}
	if _, err := cInsecure.List(context.Background(), core.Query{Namespace: "ns-a"}); err != nil {
		t.Errorf("List insecure: %v", err)
	}
}

// TestClientURLContract verifies scheme/userinfo validation at construction
// time (plan slice 1; CONN-06/08).
func TestClientURLContract(t *testing.T) {
	cases := []struct {
		name, server string
		wantErr      bool
	}{
		{"https ok", "https://argo.example.test/argo", false},
		{"http loopback ok", "http://127.0.0.1:2746", false},
		{"http non-loopback rejected", "http://argo.example.test", true},
		{"userinfo rejected", "https://user:pass@argo.example.test", true},
		{"credential query rejected", "https://argo.example.test/?token=abc", true},
		{"ftp rejected", "ftp://argo.example.test", true},
		{"empty rejected", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(Options{Server: tc.server, TokenFn: func() (string, error) { return "t", nil }, Now: time.Now})
			if (err != nil) != tc.wantErr {
				t.Errorf("NewClient(%q) err = %v, wantErr %v", tc.server, err, tc.wantErr)
			}
		})
	}
}

// --- Slice 2: list pagination -------------------------------------------------

// loadFixture reads a testdata fixture relative to this package.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// newFixtureServer serves canned fixtures per exact path+query, recording
// the raw query strings of every hit.
type fixtureServer struct {
	*httptest.Server
	muHits   int32
	hitQuery atomic.Value // string
}

// isGateScan reports whether a request is the narrow scan that fills the
// Suspend marker rather than the list itself. It asks for node maps and only
// for the workflows that have not completed.
func isGateScan(r *http.Request) bool {
	return strings.Contains(r.URL.Query().Get("fields"), "items.status.nodes")
}

// serveNoGates answers a gate scan with a list of no workflows, which is what
// a namespace with nothing in flight returns. A test that does not care about
// the Suspend marker uses it so the scan neither fails nor counts as a page.
func serveNoGates(w http.ResponseWriter) {
	w.Write([]byte(`{"metadata":{},"items":[]}`))
}

func serveFixture(t *testing.T, status int, body []byte, check func(r *http.Request) error) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			serveNoGates(w)
			return
		}
		if check != nil {
			if err := check(r); err != nil {
				t.Errorf("request check: %v", err)
			}
		}
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestListPaginationMetadata verifies summaries + opaque continuation +
// resourceVersion decoding, with the exact fixture bytes (plan slice 2).
func TestListPaginationMetadata(t *testing.T) {
	body := loadFixture(t, "list_page1.json")
	srv := serveFixture(t, http.StatusOK, body, func(r *http.Request) error {
		if got := r.URL.Query().Get("listOptions.limit"); got != "2" {
			return fmt.Errorf("limit = %q", got)
		}
		if got := r.URL.Query().Get("listOptions.continue"); got != "" {
			return fmt.Errorf("first page must not send continue, got %q", got)
		}
		return nil
	})
	c := newTestClient(t, srv)

	page, err := c.List(context.Background(), core.Query{Namespace: "team-a", Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	// Page metadata: opaque continuation + resource version pass through.
	if page.Continue != "100" {
		t.Errorf("Continue = %q, want %q", page.Continue, "100")
	}
	if page.ResourceVersion != "rv-live-1" {
		t.Errorf("ResourceVersion = %q", page.ResourceVersion)
	}

	first := page.Items[0]
	if first.Ref.Name != "train-pipeline" || first.Ref.Namespace != "team-a" || first.Ref.UID != "uid-1" {
		t.Errorf("item0 ref = %+v", first.Ref)
	}
	if first.Phase != "Running" || first.ResourceVersion != "11" {
		t.Errorf("item0 phase/rv = %q/%q", first.Phase, first.ResourceVersion)
	}
	if first.Message != "running" {
		t.Errorf("item0 message = %q", first.Message)
	}
	wantCreated := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	if !first.CreatedAt.Equal(wantCreated) {
		t.Errorf("item0 CreatedAt = %v, want %v", first.CreatedAt, wantCreated)
	}
	if first.StartedAt == nil || !first.StartedAt.Equal(wantCreated.Add(time.Minute)) {
		t.Errorf("item0 StartedAt = %v", first.StartedAt)
	}
	if first.FinishedAt != nil {
		t.Errorf("item0 FinishedAt = %v, want nil", first.FinishedAt)
	}
	if first.Labels["workflows.argoproj.io/phase"] != "Running" {
		t.Errorf("item0 labels = %v", first.Labels)
	}

	// Item 1 has a phase the pinned v4.1.2 enum does not define. Unknown
	// phases remain displayable — never an error (LIST-11).
	second := page.Items[1]
	if second.Phase != "WaitingForDependency" {
		t.Errorf("item1 phase = %q, want verbatim unknown phase", second.Phase)
	}
	if second.FinishedAt == nil || !second.FinishedAt.Equal(time.Date(2026, 9, 8, 9, 37, 0, 0, time.UTC)) {
		t.Errorf("item1 FinishedAt = %v", second.FinishedAt)
	}
}

// TestListEmptyContinuedPage verifies the continuation is followed even when
// a page contains zero items, passing the token back verbatim (plan slice 2;
// LIST-02/03).
func TestListEmptyContinuedPage(t *testing.T) {
	p1 := loadFixture(t, "list_page1.json")
	p2 := loadFixture(t, "list_page2.json")
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/team-a" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if isGateScan(r) {
			serveNoGates(w)
			return
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Write(p1)
			return
		}
		// Token must be passed back verbatim, never parsed or re-built.
		if got := r.URL.Query().Get("listOptions.continue"); got != "100" {
			t.Errorf("page2 continue = %q, want verbatim %q", got, "100")
		}
		w.Write(p2)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	ctx := context.Background()
	seen := 0
	cont := ""
	var lastRV string
	for {
		page, err := c.List(ctx, core.Query{Namespace: "team-a", Continue: cont, Limit: 2})
		if err != nil {
			t.Fatalf("List page %d: %v", seen+1, err)
		}
		seen++
		lastRV = page.ResourceVersion
		if seen == 2 && len(page.Items) != 0 {
			t.Fatalf("page2 items = %d, want 0 (empty continued page)", len(page.Items))
		}
		if page.Continue == "" {
			break
		}
		cont = page.Continue
		if seen > 10 {
			t.Fatal("continuation did not terminate")
		}
	}
	if seen != 2 {
		t.Errorf("pages = %d, want 2", seen)
	}
	if lastRV != "rv-live-2" {
		t.Errorf("last RV = %q", lastRV)
	}
}

// TestListUnknownPhase exercises the dedicated unknown-phase fixture via the
// wire decoder: an undefined phase string must decode verbatim and the
// summary stay fully populated (plan slice 2; LIST-11).
func TestListUnknownPhase(t *testing.T) {
	body := loadFixture(t, "list_unknown_phase.json")
	srv := serveFixture(t, http.StatusOK, body, nil)
	c := newTestClient(t, srv)

	page, err := c.List(context.Background(), core.Query{Namespace: "ns-x"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	it := page.Items[0]
	if it.Phase != "ZombifiedByUpgrade" {
		t.Errorf("phase = %q, want verbatim server phase", it.Phase)
	}
	if it.Ref.UID != "uid-9" || it.Ref.Name != "weird-one" || it.Ref.Namespace != "ns-x" {
		t.Errorf("ref = %+v", it.Ref)
	}
	if it.ResourceVersion != "5" {
		t.Errorf("resourceVersion = %q", it.ResourceVersion)
	}
}

// --- Slice 3: detail mapping ---------------------------------------------------

// TestGetUIDMismatch verifies same-name replacement detection: a response
// whose UID differs from the requested one is a typed mismatch error, never
// silently swapped (plan slice 3; DET-02, LIST-12).
func TestGetUIDMismatch(t *testing.T) {
	body := loadFixture(t, "workflow_detail.json")
	srv := serveFixture(t, http.StatusOK, body, func(r *http.Request) error {
		if got := r.URL.Query().Get("uid"); got != "uid-selected" {
			return fmt.Errorf("uid query = %q, want requested uid", got)
		}
		return nil
	})
	c := newTestClient(t, srv)

	_, err := c.Get(context.Background(), core.Ref{Namespace: "team-a", Name: "dag-complex", UID: "uid-selected"})
	if err == nil {
		t.Fatal("expected UID mismatch error, got nil")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrConflict {
		t.Errorf("kind = %q, want conflict", ae.Kind)
	}
	if !strings.Contains(ae.Message, "wf-uid-111") || !strings.Contains(ae.Message, "uid-selected") {
		t.Errorf("message %q must name both UIDs", ae.Message)
	}
}

// TestGetMissingNodes verifies explicit representation of missing node data:
// offload markers ⇒ NodesAvailable=false + reason, node id references that
// are absent from the map survive as-is for the outline's ungrouped section
// (plan slice 3; DET-04).
func TestGetMissingNodes(t *testing.T) {
	off := loadFixture(t, "workflow_offloaded.json")
	srv := serveFixture(t, http.StatusOK, off, nil)
	c := newTestClient(t, srv)

	wf, err := c.Get(context.Background(), core.Ref{Namespace: "team-a", Name: "big-hydrate", UID: "wf-uid-222"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if wf.NodesAvailable {
		t.Error("NodesAvailable = true for offloaded detail")
	}
	if wf.NodesUnavailableReason == "" {
		t.Error("NodesUnavailableReason empty")
	}
	if len(wf.Nodes) != 0 {
		t.Errorf("Nodes = %d entries, want empty", len(wf.Nodes))
	}
	if wf.Summary.Phase != "Succeeded" || wf.Summary.Ref.UID != "wf-uid-222" {
		t.Errorf("summary = %+v", wf.Summary)
	}
}

// TestGetUnknownFields verifies the hydrated detail mapping and that raw
// resource JSON — including unknown fields — is preserved verbatim
// (plan slice 3; DET-13, DET-06..08 fixtures feed).
func TestGetUnknownFields(t *testing.T) {
	body := loadFixture(t, "workflow_detail.json")
	srv := serveFixture(t, http.StatusOK, body, nil)
	c := newTestClient(t, srv)

	wf, err := c.Get(context.Background(), core.Ref{Namespace: "team-a", Name: "dag-complex", UID: "wf-uid-111"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !wf.NodesAvailable {
		t.Fatalf("NodesAvailable = false, reason %q", wf.NodesUnavailableReason)
	}
	if len(wf.Nodes) != 4 {
		t.Fatalf("nodes = %d, want 4", len(wf.Nodes))
	}

	root := wf.Nodes["node-root"]
	if root.Name != "dag-complex" || root.Type != "DAG" || root.Phase != "Running" {
		t.Errorf("root = %+v", root)
	}
	if len(root.Children) != 1 || root.Children[0] != "node-echo" {
		t.Errorf("root children = %v", root.Children)
	}
	if len(root.OutboundNodes) != 1 || root.OutboundNodes[0] != "node-echo" {
		t.Errorf("root outboundNodes = %v", root.OutboundNodes)
	}
	echo := wf.Nodes["node-echo"]
	if echo.BoundaryID != "node-root" || echo.Phase != "Succeeded" {
		t.Errorf("echo = %+v", echo)
	}
	if echo.StartedAt == nil || echo.FinishedAt == nil {
		t.Error("echo timestamps missing")
	}
	suspend := wf.Nodes["node-suspend"]
	if suspend.StartedAt != nil || suspend.FinishedAt != nil {
		t.Error("suspend must have nil timestamps (absent = not yet started)")
	}
	// The reference to a node id missing from the map is preserved verbatim;
	// the outline (C1) renders it in an explicit ungrouped section.
	ghost := wf.Nodes["node-ghost-parent"]
	if len(ghost.Children) != 1 || ghost.Children[0] != "node-missing-id" {
		t.Errorf("ghost children = %v", ghost.Children)
	}
	if _, ok := wf.Nodes["node-missing-id"]; ok {
		t.Error("node-missing-id unexpectedly present in map")
	}

	// Summary carried through.
	if wf.Summary.Ref.UID != "wf-uid-111" || wf.Summary.Phase != "Running" || wf.Summary.Message != "progress 1/2" {
		t.Errorf("summary = %+v", wf.Summary)
	}
	if wf.Summary.StartedAt == nil {
		t.Error("summary StartedAt nil")
	}

	// Resource: raw JSON preserved verbatim (unknown fields intact).
	var probe struct {
		Spec struct {
			XCustomExtension string `json:"x-custom-extension"`
		} `json:"spec"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := jsonUnmarshal(wf.Resource, &probe); err != nil {
		t.Fatalf("resource JSON: %v", err)
	}
	if probe.Spec.XCustomExtension != "preserve-me-42" {
		t.Errorf("unknown field lost: %q", probe.Spec.XCustomExtension)
	}
	if probe.Status.Phase != "Running" {
		t.Errorf("resource phase = %q", probe.Status.Phase)
	}
}

// --- Slice 4: log transport -----------------------------------------------------

// streamingServer serves the given chunks with a small delay between them,
// honoring request context cancellation (mimics the Argo gateway stream).
func streamingServer(t *testing.T, chunks [][]byte, delay time.Duration, check func(r *http.Request) error) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			if err := check(r); err != nil {
				t.Errorf("request check: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, ch := range chunks {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(ch); err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestStreamLogsFixture checks the JSON-lines fixture end-to-end through the
// parser: multiple records per read, split-tolerant buffering, missing
// trailing newline, malformed UTF-8 → U+FFFD (plan slice 4; STR-02/04).
func TestStreamLogsFixture(t *testing.T) {
	raw := loadFixture(t, "logs_jsonl.txt")
	srv := streamingServer(t, [][]byte{raw}, 0, nil)
	c := newTestClient(t, srv)

	var got []core.LogRecord
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "team-a", Name: "wf", UID: "u1"},
		Container: "main",
	}, func(r core.LogRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	wantContent := []string{"line-one", "line-two", "line-\uFFFDbad", "final-no-newline"}
	if len(got) != len(wantContent) {
		t.Fatalf("records = %d, want %d (%+v)", len(got), len(wantContent), got)
	}
	wantPods := []string{"pod-a", "pod-b", "pod-b", "pod-c"}
	for i, rec := range got {
		if rec.PodName != wantPods[i] {
			t.Errorf("rec%d PodName = %q, want %q", i, rec.PodName, wantPods[i])
		}
		if rec.Content != wantContent[i] {
			t.Errorf("rec%d Content = %q, want %q", i, rec.Content, wantContent[i])
		}
		if rec.Container != "main" {
			t.Errorf("rec%d Container = %q (request context, not fabricated)", i, rec.Container)
		}
		if rec.ReceivedAt.IsZero() {
			t.Errorf("rec%d ReceivedAt zero", i)
		}
	}
}

// TestStreamLogsSSEFixture proves the same fixture through SSE framing is
// accepted by the one parser choke point (docs/development.md resolution).
func TestStreamLogsSSEFixture(t *testing.T) {
	raw := loadFixture(t, "logs_sse.txt")
	srv := streamingServer(t, [][]byte{raw}, 0, nil)
	c := newTestClient(t, srv)

	var got []core.LogRecord
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "team-a", Name: "wf", UID: "u1"},
		Container: "main",
	}, func(r core.LogRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	if len(got) != 4 || got[0].Content != "line-one" || got[3].Content != "final-no-newline" {
		t.Fatalf("SSE records = %+v", got)
	}
}

// TestStreamLogsFragmentation verifies records split across and coalesced
// within reads parse identically (plan slice 4; STR-02).
func TestStreamLogsFragmentation(t *testing.T) {
	c1 := []byte(`{"result":{"content":"alpha","podName":"p1"}}` + "\n" + `{"result":{"content":"bet`)
	c2 := []byte(`a","podName":"p1"}}` + "\n" + `{"result":{"content":"gamma","podName":"p2"}}` + "\n")
	srv := streamingServer(t, [][]byte{c1, c2}, 5*time.Millisecond, nil)
	c := newTestClient(t, srv)

	var got []core.LogRecord
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
		Container: "main",
	}, func(r core.LogRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("records = %d (%+v), want 3", len(got), got)
	}
	want := []struct{ content, pod string }{{"alpha", "p1"}, {"beta", "p1"}, {"gamma", "p2"}}
	for i, w := range want {
		if got[i].Content != w.content || got[i].PodName != w.pod {
			t.Errorf("rec%d = %q/%q, want %q/%q", i, got[i].Content, got[i].PodName, w.content, w.pod)
		}
	}
}

// TestStreamLogsInBandError verifies the final {"error":{...}} chunk after
// HTTP 200 becomes a typed error with the gRPC-derived kind and no HTTP
// status (plan slice 4; STR-04, docs/development.md).
func TestStreamLogsInBandError(t *testing.T) {
	cases := []struct {
		name     string
		chunk    string
		wantKind core.ErrorKind
	}{
		{"pod deleted mid-stream (not_found)", `{"error":{"code":5,"message":"pod deleted"}}`, core.ErrNotFound},
		{"rbac (forbidden)", `{"error":{"code":7,"message":"forbidden"}}`, core.ErrForbidden},
		{"watch-EOF quirk (rate_limited)", `{"error":{"code":8,"message":"too many open files"}}`, core.ErrRateLimited},
		{"bad grep regex (invalid)", `{"error":{"code":3,"message":"invalid regexp"}}`, core.ErrInvalid},
		{"unimplemented (unsupported)", `{"error":{"code":12,"message":"logs disabled"}}`, core.ErrUnsupported},
		{"full gateway shape", `{"error":{"grpc_code":5,"http_code":404,"message":"pod gone","http_status":"Not Found"}}`, core.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks := [][]byte{
				[]byte(`{"result":{"content":"before-error","podName":"p1"}}` + "\n"),
				[]byte(tc.chunk + "\n"),
			}
			srv := streamingServer(t, chunks, 5*time.Millisecond, nil)
			c := newTestClient(t, srv)
			var got []core.LogRecord
			err := c.StreamLogs(context.Background(), core.LogRequest{
				Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
				Container: "main",
			}, func(r core.LogRecord) error {
				got = append(got, r)
				return nil
			})
			if err == nil {
				t.Fatal("expected in-band error, got nil")
			}
			ae := core.AsAPIError(err)
			if ae == nil {
				t.Fatalf("error is not *core.APIError: %v", err)
			}
			if ae.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", ae.Kind, tc.wantKind)
			}
			if ae.Status != 0 {
				t.Errorf("Status = %d, want 0 for in-band error", ae.Status)
			}
			if len(got) != 1 || got[0].Content != "before-error" {
				t.Errorf("records before error = %+v", got)
			}
		})
	}
}

// TestStreamLogsCancel verifies prompt cancellation mid-stream: StreamLogs
// returns an error wrapping context.Canceled — distinguishable from network
// failure — and the in-flight HTTP body is closed (plan slice 4; STR-01/03/05).
func TestStreamLogsCancel(t *testing.T) {
	c1 := []byte(`{"result":{"content":"first","podName":"p1"}}` + "\n")
	c2 := []byte(`{"result":{"content":"second","podName":"p1"}}` + "\n")
	c3 := []byte(`{"result":{"content":"third","podName":"p1"}}` + "\n")
	srv := streamingServer(t, [][]byte{c1, c2, c3}, 200*time.Millisecond, nil)
	c := newTestClient(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	var got []core.LogRecord
	done := make(chan error, 1)
	go func() {
		done <- c.StreamLogs(ctx, core.LogRequest{
			Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
			Container: "main",
		}, func(r core.LogRecord) error {
			got = append(got, r)
			cancel() // cancel as soon as the first record arrives
			return nil
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("StreamLogs err = %v, want context.Canceled-wrapping", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StreamLogs did not return within 5s of cancellation")
	}
	if len(got) != 1 {
		t.Errorf("records = %d, want exactly 1 (callback stopped the stream too)", len(got))
	}
}

// TestStreamLogsOversize verifies a single record exceeding the client cap
// is dropped with a visible marker instead of unbounded memory growth, and
// bounded lines pass through (plan slice 4; STR-05, LOG-05/14).
func TestStreamLogsOversize(t *testing.T) {
	// Server pipeline caps at 1 MiB; our client cap is 2 MiB here — an
	// attacker/misbehaving proxy may still deliver more.
	big := strings.Repeat("x", 3*1024*1024) // 3 MiB > 2 MiB cap
	chunks := [][]byte{
		[]byte(`{"result":{"content":"ok-line","podName":"p1"}}` + "\n"),
		[]byte(`{"result":{"content":"` + big + `","podName":"p1"}}` + "\n"),
		[]byte(`{"result":{"content":"after-oversize","podName":"p1"}}` + "\n"),
	}
	srv := streamingServer(t, chunks, 2*time.Millisecond, nil)
	c := newTestClient(t, srv)

	var got []core.LogRecord
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
		Container: "main",
	}, func(r core.LogRecord) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("records = %d, want 3 (2 real + 1 marker)", len(got))
	}
	if got[0].Content != "ok-line" || got[2].Content != "after-oversize" {
		t.Errorf("real lines wrong: %q / %q", got[0].Content, got[2].Content)
	}
	if got[1].Content != OversizeRecordMarker {
		t.Errorf("oversize record content = %q (len %d), want marker", got[1].Content[:min(40, len(got[1].Content))], len(got[1].Content))
	}

	// A line at exactly the cap boundary passes.
	ok := strings.Repeat("y", int(MaxLogRecordBytes))
	srv2 := streamingServer(t, [][]byte{
		[]byte(`{"result":{"content":"` + ok + `","podName":"p1"}}` + "\n"),
	}, 0, nil)
	c2 := newTestClient(t, srv2)
	var pass int
	err = c2.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
		Container: "main",
	}, func(r core.LogRecord) error { pass++; return nil })
	if err != nil {
		t.Fatalf("StreamLogs boundary: %v", err)
	}
	if pass != 1 {
		t.Errorf("boundary-size record dropped (deliveries = %d)", pass)
	}
}

// TestStreamLogsCallbackError verifies a callback error stops the stream and
// is returned to the caller (plan slice 4; contract: callback error stops).
func TestStreamLogsCallbackError(t *testing.T) {
	chunks := [][]byte{
		[]byte(`{"result":{"content":"one","podName":"p1"}}` + "\n" +
			`{"result":{"content":"two","podName":"p1"}}` + "\n"),
	}
	srv := streamingServer(t, chunks, 0, nil)
	c := newTestClient(t, srv)

	cbErr := errors.New("consumer full")
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
		Container: "main",
	}, func(core.LogRecord) error { return cbErr })
	if !errors.Is(err, cbErr) {
		t.Fatalf("err = %v, want callback error wrapped", err)
	}
}

// TestStreamLogsHTTPError verifies a pre-stream HTTP failure maps to a
// typed error (stream errors before the first chunk are unary-style).
func TestStreamLogsHTTPError(t *testing.T) {
	srv := serveFixture(t, http.StatusNotFound,
		[]byte(`{"code":5,"message":"workflow team-a/ghost not found","error":"workflow team-a/ghost not found"}`), nil)
	c := newTestClient(t, srv)
	err := c.StreamLogs(context.Background(), core.LogRequest{
		Ref:       core.Ref{Namespace: "team-a", Name: "ghost", UID: "u"},
		Container: "main",
	}, func(core.LogRecord) error { return nil })
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrNotFound || ae.Status != http.StatusNotFound {
		t.Errorf("kind/status = %q/%d", ae.Kind, ae.Status)
	}
	if !strings.Contains(ae.Message, "ghost") {
		t.Errorf("message %q must name the workflow", ae.Message)
	}
}

// --- Slice 5: errors -------------------------------------------------------------

// TestAPIErrorRedaction verifies typed errors are sanitized: server messages
// pass through, but no token material ever appears in an error surface
// (plan slice 5; SEC-02/06, CONN-11).
func TestAPIErrorRedaction(t *testing.T) {
	token := "tok-sup3r-secret-0123456789abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assert the client actually authenticated (so the token exists on
		// the wire and the redaction test is meaningful).
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"code":7,"message":"workflows.argoproj.io is forbidden: cannot list in ns-secret","error":"forbidden"}`)
	}))
	defer srv.Close()

	c, err := NewClient(Options{
		Server:  srv.URL,
		TokenFn: func() (string, error) { return token, nil },
		Now:     time.Now,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.List(context.Background(), core.Query{Namespace: "ns-secret"})
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrForbidden || ae.Status != http.StatusForbidden {
		t.Errorf("kind/status = %q/%d", ae.Kind, ae.Status)
	}
	if !strings.Contains(ae.Message, "forbidden") {
		t.Errorf("server message lost: %q", ae.Message)
	}
	for _, surface := range []string{err.Error(), ae.Message} {
		if strings.Contains(surface, token) {
			t.Errorf("error surface leaks token: %q", surface)
		}
		if strings.Contains(surface, "test") && strings.Contains(surface, "secret-0123") {
			t.Errorf("error surface leaks partial token: %q", surface)
		}
	}
}

// TestErrorBodyNonJSON verifies a non-JSON error body (login HTML from an
// SSO reverse proxy) maps to unauthenticated guidance with the content type
// named, not a parse crash (CONN-14; docs/development.md).
func TestErrorBodyNonJSON(t *testing.T) {
	srv := serveFixture(t, http.StatusOK,
		[]byte("<html><body><form>Sign in to your identity provider</form></body></html>"),
		func(r *http.Request) error {
			if r.Header.Get("Authorization") == "" {
				return errors.New("credentials not sent")
			}
			return nil
		})
	c := newTestClient(t, srv)

	_, err := c.List(context.Background(), core.Query{Namespace: "ns-a"})
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("error is not *core.APIError: %v", err)
	}
	if ae.Kind != core.ErrUnauthenticated {
		t.Errorf("kind = %q, want unauthenticated for HTML login page", ae.Kind)
	}
	if !strings.Contains(strings.ToLower(ae.Message), "html") {
		t.Errorf("message %q must name the HTML body", ae.Message)
	}
}

// TestRateLimitRetryAfter verifies 429 mapping parses Retry-After in both
// delay-seconds and HTTP-date forms (plan slice 5; LIST-08).
func TestRateLimitRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, header string
		want         time.Duration
		wantNil      bool
	}{
		{"delay-seconds", "30", 30 * time.Second, false},
		{"http-date", now.Add(2 * time.Minute).UTC().Format(http.TimeFormat), 2 * time.Minute, false},
		{"past date clamps to zero", "Mon, 01 Jan 2001 00:00:00 GMT", 0, false},
		{"garbage is nil", "later", 0, true},
		{"absent is nil", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, `{"code":8,"message":"throttled","error":"throttled"}`)
			}))
			defer srv.Close()

			c, err := NewClient(Options{
				Server:  srv.URL,
				TokenFn: func() (string, error) { return "t", nil },
				Now:     func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.List(context.Background(), core.Query{Namespace: "ns-a"})
			ae := core.AsAPIError(err)
			if ae == nil {
				t.Fatalf("error is not *core.APIError: %v", err)
			}
			if ae.Kind != core.ErrRateLimited {
				t.Errorf("kind = %q, want rate_limited", ae.Kind)
			}
			if tc.wantNil {
				if ae.RetryAfter != nil {
					t.Errorf("RetryAfter = %v, want nil", *ae.RetryAfter)
				}
				return
			}
			if ae.RetryAfter == nil {
				t.Fatalf("RetryAfter = nil, want %v", tc.want)
			}
			if *ae.RetryAfter != tc.want {
				t.Errorf("RetryAfter = %v, want %v", *ae.RetryAfter, tc.want)
			}
		})
	}
}

// TestNoRetryOnMutations pins the "no retries of mutations" rule: this
// package implements no write paths at all — there is no action method to
// call, so no mutation can be retried (plan slice 5; §6).
func TestNoRetryOnMutations(t *testing.T) {
	// The transport surface is read-only: List/Get/StreamLogs only.
	var _ core.Reader = (*Client)(nil)
	// If someone adds a mutating method with retry logic, this compile-time
	// surface check plus review will catch it; the retry budget below stays
	// a guard for read retries only.
	c := &Client{}
	if c.maxRetries != 0 {
		t.Errorf("maxRetries = %d, want 0 (no automatic retries; the app layer owns backoff)",
			c.maxRetries)
	}
}

// A plain-http endpoint cannot fail a TLS handshake. Reporting one sends the
// operator after a certificate problem when the real cause is usually a dead
// port-forward.
func TestPlainHTTPFailureIsNotReportedAsTLS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close() // nothing is listening now

	c, err := NewClient(Options{Server: base, TokenFn: func() (string, error) { return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(context.Background(), core.Ref{Namespace: "ns", Name: "wf", UID: "u"})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "TLS handshake") {
		t.Fatalf("plain http failure reported as TLS: %v", err)
	}
	if !strings.Contains(err.Error(), "server unreachable") {
		t.Fatalf("want an unreachable-server error, got: %v", err)
	}
}

// The list projection is what keeps a poll cheap, and it is also the one
// place where a summary field can be lost without any error: the server
// simply omits what the projection does not name.
func TestListProjectionNamesEverySummaryField(t *testing.T) {
	want := []string{
		"metadata.continue",
		"metadata.resourceVersion",
		"items.metadata.name",
		"items.metadata.namespace",
		"items.metadata.uid",
		"items.metadata.creationTimestamp",
		"items.spec.suspend",
		"items.status.phase",
		"items.status.startedAt",
		"items.status.finishedAt",
		"items.status.progress",
		"items.status.estimatedDuration",
	}
	have := map[string]bool{}
	for _, f := range strings.Split(listFields, ",") {
		have[f] = true
	}
	for _, f := range want {
		if !have[f] {
			t.Errorf("list projection drops %s", f)
		}
	}
	// The node map is the bulk of a workflow object and belongs to the gate
	// scan alone. In the list projection it would undo the saving.
	if have["items.status.nodes"] {
		t.Error("list projection carries items.status.nodes")
	}
}

// The list page carries no node data, so the Suspend marker can only come
// from the gate scan. A workflow waiting for a human Resume that the list
// draws as a plain Running row is the row an operator misses.
func TestListMarksSuspendedFromTheGateScan(t *testing.T) {
	list := `{"metadata":{"resourceVersion":"7"},"items":[
		{"metadata":{"name":"wf-a","namespace":"team-a","uid":"uid-a"},"status":{"phase":"Running"}},
		{"metadata":{"name":"wf-b","namespace":"team-a","uid":"uid-b"},"status":{"phase":"Running"}}]}`
	gates := `{"metadata":{},"items":[
		{"metadata":{"uid":"uid-b"},"status":{"nodes":{"n1":{"type":"Suspend","phase":"Running"}}}},
		{"metadata":{"uid":"uid-a"},"status":{"nodes":{"n1":{"type":"Pod","phase":"Running"},"n2":{"type":"Suspend","phase":"Succeeded"}}}}]}`
	var gateQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			gateQuery = r.URL.Query()
			w.Write([]byte(gates))
			return
		}
		if strings.Contains(r.URL.Query().Get("fields"), "items.status.nodes") {
			t.Error("the list request asked for node maps")
		}
		w.Write([]byte(list))
	}))
	defer srv.Close()

	page, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "team-a", Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Items[0].Suspended {
		t.Error("wf-a is marked suspended: its only Suspend node has finished")
	}
	if !page.Items[1].Suspended {
		t.Error("wf-b is not marked suspended: it holds a running Suspend node")
	}
	if got := gateQuery.Get("listOptions.labelSelector"); got != incompleteSelector {
		t.Errorf("gate scan selector = %q, want %q", got, incompleteSelector)
	}
	if got := gateQuery.Get("listOptions.limit"); got != "500" {
		t.Errorf("gate scan limit = %q, want the scan bound", got)
	}
}

// A caller's own selector narrows the scan as well, or the scan reads
// workflows the list never shows.
func TestGateScanKeepsTheCallersSelector(t *testing.T) {
	var gateSelector string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			gateSelector = r.URL.Query().Get("listOptions.labelSelector")
			serveNoGates(w)
			return
		}
		w.Write([]byte(`{"metadata":{},"items":[{"metadata":{"name":"wf-a","namespace":"team-a","uid":"uid-a"},"status":{"phase":"Running"}}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "team-a", LabelSelector: "app=users"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := "app=users," + incompleteSelector; gateSelector != want {
		t.Errorf("gate scan selector = %q, want %q", gateSelector, want)
	}
}

// An empty page needs no scan. Paging past the end of a namespace must not
// cost a request per page.
func TestAnEmptyPageRunsNoGateScan(t *testing.T) {
	var scans int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			scans++
			serveNoGates(w)
			return
		}
		w.Write([]byte(`{"metadata":{},"items":[]}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "team-a"}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if scans != 0 {
		t.Errorf("gate scans = %d, want none", scans)
	}
}
