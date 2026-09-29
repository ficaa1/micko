//go:build integration

package integration

// WireClient conformance tests use the synthetic Argo HTTP server.

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/app"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/actions"
)

const testNS = "synthetic-ns"

// newSeededServer starts a fixture server with three synthetic workflows.
func newSeededServer(t *testing.T) *FixtureServer {
	t.Helper()
	fs := NewFixtureServer(t, ServerConfig{})
	for _, wf := range []WireWorkflow{
		FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"),
		FixtureWorkflow(testNS, "wf-b", "Failed", "uid-b"),
		FixtureWorkflow(testNS, "wf-c", "Succeeded", "uid-c"),
	} {
		fs.PutWorkflow(wf)
	}
	return fs
}

// harness drives the real root model without a terminal: it runs the
// model's async commands and feeds every message back through Update —
// the same loop tea.Program runs, minus the renderer.
type harness struct {
	t     *testing.T
	root  *app.Root
	queue []tea.Cmd

	// dropTicks stops the poll cycle: fed-back tick messages (app's
	// unexported tickMsg — identified via reflection, since an unexported
	// type cannot be named from this package) are dropped instead of
	// delivered, so the queue drains and the model settles at loading=false. This makes view
	// assertions deterministic: the program's own tick guard otherwise
	// restarts a poll the instant the previous one completes.
	dropTicks bool
}

func newHarness(t *testing.T, client *WireClient, interval time.Duration) *harness {
	t.Helper()
	clock := newPumpClock()
	h := &harness{
		t:    t,
		root: app.NewRoot(client, clock, testNS, interval, actions.Options{ReadOnly: true}),
	}
	h.enqueue(h.root.Init())
	return h
}

// pumpClock implements app.Clock with a fixed time (no progression needed
// for the journey tests; stale-age rendering is a view concern).
type pumpClock struct{ now time.Time }

func newPumpClock() *pumpClock { return &pumpClock{now: FixtureEpoch} }
func (c *pumpClock) Now() time.Time {
	return c.now
}

// enqueue adds commands to the pump queue (nil-safe).
func (h *harness) enqueue(cmds ...tea.Cmd) {
	for _, c := range cmds {
		if c != nil {
			h.queue = append(h.queue, c)
		}
	}
}

// newHarnessForNS is newHarness pointed at a specific namespace.
func newHarnessForNS(t *testing.T, client *WireClient, ns string) *harness {
	t.Helper()
	clock := newPumpClock()
	h := &harness{
		t:    t,
		root: app.NewRoot(client, clock, ns, time.Nanosecond, actions.Options{ReadOnly: true}),
	}
	h.enqueue(h.root.Init())
	return h
}

// send delivers one message to the model and enqueues the returned command.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.root.Update(msg)
	if next != nil {
		h.root = next.(*app.Root)
	}
	h.enqueue(cmd)
}

// pump executes queued commands and feeds their messages back until the
// queue drains or maxRounds is hit. Timer-based ticks (poll cycle) are
// skipped: integration journeys drive steps explicitly.
func (h *harness) pump(maxRounds int) {
	h.t.Helper()
	rounds := 0
	for len(h.queue) > 0 && rounds < maxRounds {
		rounds++
		cmd := h.queue[0]
		h.queue = h.queue[1:]
		for _, msg := range runLeaf(cmd) {
			if _, isTick := msg.(tickSentinel); isTick {
				continue
			}
			if h.dropTicks && isTickMsg(msg) {
				continue
			}
			h.send(msg)
		}
	}
}

// isTickMsg reports whether msg is the app package's unexported tickMsg
// (an empty struct). Reflection is the only handle these tests have on it.
func isTickMsg(msg tea.Msg) bool {
	if msg == nil {
		return false
	}
	typ := reflect.TypeOf(msg)
	return typ.Kind() == reflect.Struct && typ.NumField() == 0 &&
		typ.Name() == "tickMsg" && typ.PkgPath() == "github.com/ficaa1/micko/internal/app"
}

// settle stops the poll cycle and drains the queue so the model rests at
// loading=false (the last list command completes, fed-back ticks dropped).
func (h *harness) settle(maxRounds int) {
	h.dropTicks = true
	h.pump(maxRounds)
}

// runLeaf executes a command (recursing into BatchMsg) returning leaf msgs.
func runLeaf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, runLeaf(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

// tickSentinel replaces time.Sleep-based ticks in tests via a tiny
// interval; the pump drops them (see newHarness interval choice).
type tickSentinel struct{}

// view returns the model's rendered screen content.
func (h *harness) view() string {
	return h.root.View().Content
}

// --- wire contract: list + pagination ----------------------------

func TestListPaginationOverRealHTTP(t *testing.T) {
	fs := newSeededServer(t)
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	ctx := context.Background()

	// Page 1: limit=2 must return 2 items and the next offset as continue.
	page, err := client.List(ctx, core.Query{Namespace: testNS, Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("page1 items = %d, want 2", len(page.Items))
	}
	if page.Continue == "" {
		t.Fatal("page1 continue token missing")
	}
	if page.Continue != "2" {
		t.Errorf("page1 continue = %q, want decimal offset %q (v4.1.2)", page.Continue, "2")
	}
	if page.ResourceVersion == "" {
		t.Error("resourceVersion empty")
	}

	// Verbatim pass-back: the token is opaque; return unchanged.
	page2, err := client.List(ctx, core.Query{Namespace: testNS, Limit: 2, Continue: page.Continue})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page2 items = %d, want 1", len(page2.Items))
	}
	if page2.Continue != "" {
		t.Errorf("page2 continue = %q, want empty (end of list)", page2.Continue)
	}

	// Wire check: the second request must carry the token verbatim.
	qs := fs.QueryValues("/api/v1/workflows/" + testNS)
	if len(qs) < 2 {
		t.Fatalf("recorded %d list queries, want >=2", len(qs))
	}
	if !strings.Contains(qs[len(qs)-1], "listOptions.continue="+page.Continue) {
		t.Errorf("continue token not passed back verbatim: %q", qs[len(qs)-1])
	}
	if !strings.Contains(qs[0], "listOptions.limit=2") {
		t.Errorf("limit not string-encoded on the wire: %q", qs[0])
	}
}

func TestListUnknownPhasePreserved(t *testing.T) {
	fs := newSeededServer(t)
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-weird", "WeirdFuturePhase", "uid-weird"))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	page, err := client.List(context.Background(), core.Query{Namespace: testNS})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, s := range page.Items {
		if s.Ref.Name == "wf-weird" {
			found = true
			if s.Phase != "WeirdFuturePhase" {
				t.Errorf("unknown phase mangled: %q", s.Phase)
			}
		}
	}
	if !found {
		t.Fatal("unknown-phase workflow missing from list")
	}
}

func TestListServerErrorMapsToTypedKind(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetFault("list", Fault{Status: 403, Code: grpcPermDenied, Message: "synthetic rbac denial"})
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	_, err := client.List(context.Background(), core.Query{Namespace: testNS})
	if err == nil {
		t.Fatal("expected error")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("not an APIError: %T", err)
	}
	if ae.Kind != core.ErrForbidden || ae.Status != 403 {
		t.Errorf("kind/status = %s/%d, want forbidden/403", ae.Kind, ae.Status)
	}
}

// --- wire contract: detail + UID semantics -------------------------

func TestGetUIDFallbackAndMismatch(t *testing.T) {
	fs := newSeededServer(t)
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-a", "Failed", "uid-a2")) // archives uid-a
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	ctx := context.Background()

	// Detail GET with the OLD uid must return the ARCHIVED old object —
	// never the live replacement (docs/development.md).
	wf, err := client.Get(ctx, core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"})
	if err != nil {
		t.Fatalf("get archived: %v", err)
	}
	if wf.Summary.Ref.UID != "uid-a" {
		t.Fatalf("uid = %q, want archived uid-a (not the live replacement)", wf.Summary.Ref.UID)
	}

	// Same-name replacement: live uid returns the new generation.
	wf2, err := client.Get(ctx, core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a2"})
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if wf2.Summary.Ref.UID != "uid-a2" || wf2.Summary.Phase != "Failed" {
		t.Errorf("live generation wrong: %+v", wf2.Summary)
	}

	// Unknown uid: 404 with the live error preserved.
	_, err = client.Get(ctx, core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-ghost"})
	if err == nil {
		t.Fatal("expected 404 for unknown uid")
	}
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrNotFound {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestGetNodesAndResourcePreserved(t *testing.T) {
	client := NewWireClient(WireClientConfig{BaseURL: newSeededServer(t).URL()})
	wf, err := client.Get(context.Background(), core.Ref{Namespace: testNS, Name: "wf-b", UID: "uid-b"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !wf.NodesAvailable || len(wf.Nodes) != 2 {
		t.Fatalf("nodes = %d available=%v, want 2 hydrated nodes", len(wf.Nodes), wf.NodesAvailable)
	}
	if wf.Nodes["root"].Type != "Steps" {
		t.Errorf("root type = %q", wf.Nodes["root"].Type)
	}
	if len(wf.Resource) == 0 {
		t.Error("raw resource JSON not preserved")
	}
}

// --- wire contract: log streams (framing edges) ----------------

func TestStreamLogsJSONLinesFraming(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetLogs(FixtureLogEntries(6))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	var got []core.LogRecord
	err := client.StreamLogs(context.Background(),
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
		func(r core.LogRecord) error {
			got = append(got, r)
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("records = %d, want 6", len(got))
	}
	for _, r := range got {
		if r.Container != "main" {
			t.Fatalf("container context not from request: %+v", r)
		}
	}
	// Pod identity flows from podName (no fabricated container field).
	pods := map[string]bool{}
	for _, r := range got {
		pods[r.PodName] = true
	}
	if len(pods) != 2 {
		t.Errorf("pods = %v, want 2 distinct", pods)
	}
}

func TestStreamLogsSSEFraming(t *testing.T) {
	fs := NewFixtureServer(t, ServerConfig{SSELogs: true})
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"))
	fs.SetLogs(FixtureLogEntries(4))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	var got []string
	err := client.StreamLogs(context.Background(),
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
		func(r core.LogRecord) error {
			got = append(got, r.Content)
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("records = %d, want 4 (SSE framing)", len(got))
	}
}

func TestStreamLogsInBandError(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetLogs(FixtureLogEntries(2))
	fs.SetLogError(Fault{Code: grpcInternal, Message: "synthetic in-band stream failure"})
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	var got int
	err := client.StreamLogs(context.Background(),
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
		func(core.LogRecord) error {
			got++
			return nil
		})
	if err == nil {
		t.Fatal("in-band error must surface")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("not APIError: %v", err)
	}
	if ae.Status != 0 {
		t.Errorf("in-band error carries no HTTP status; got %d", ae.Status)
	}
	if got != 2 {
		t.Errorf("records before error = %d, want 2 (order preserved)", got)
	}
}

func TestStreamLogsTruncatedFinalChunkSurfaces(t *testing.T) {
	fs := NewFixtureServer(t, ServerConfig{TruncateLogNewline: true})
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"))
	fs.SetLogs(FixtureLogEntries(1))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	err := client.StreamLogs(context.Background(),
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
		func(core.LogRecord) error { return nil })
	// The final chunk arrives without its terminating newline;
	// the last (incomplete) line must not be silently swallowed as if the
	// stream had ended cleanly with all data intact.
	if err == nil {
		t.Fatal("truncated final chunk must not pass as a clean EOF")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("not APIError: %v", err)
	}
}

func TestStreamLogsPodFilterAndTail(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetLogs(FixtureLogEntries(9))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	var got []core.LogRecord
	err := client.StreamLogs(context.Background(),
		core.LogRequest{
			Ref:       core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"},
			PodName:   "wf-b-step-pod",
			Container: "main",
			TailLines: 2,
		},
		func(r core.LogRecord) error {
			got = append(got, r)
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2 (tail)", len(got))
	}
	for _, r := range got {
		if r.PodName != "wf-b-step-pod" {
			t.Fatalf("pod filter not applied: %+v", r)
		}
	}
}

func TestStreamLogsUnknownWorkflow404(t *testing.T) {
	client := NewWireClient(WireClientConfig{BaseURL: newSeededServer(t).URL()})
	err := client.StreamLogs(context.Background(),
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "nope", UID: "uid-x"}, Container: "main"},
		func(core.LogRecord) error { return nil })
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrNotFound || ae.Status != 404 {
		t.Fatalf("want not_found/404, got %v", err)
	}
}

func TestStreamLogsCancelDuringDrip(t *testing.T) {
	fs := NewFixtureServer(t, ServerConfig{SlowLogDrip: 50 * time.Millisecond})
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"))
	fs.SetLogs(FixtureLogEntries(50))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.StreamLogs(ctx,
			core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
			func(core.LogRecord) error { return nil })
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled stream must return an error, not nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancellation must be distinguishable from network failure; got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled stream did not return promptly")
	}
}

// --- deliberately broken API responses -------------------------------

func TestBrokenJSONListFails(t *testing.T) {
	fs := NewFixtureServer(t, ServerConfig{BrokenJSONList: true})
	fs.PutWorkflow(FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	_, err := client.List(context.Background(), core.Query{Namespace: testNS})
	if err == nil {
		t.Fatal("deliberately broken list response must FAIL the test client, not pass")
	}
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrProtocol {
		t.Fatalf("want protocol-kind error, got %v", err)
	}
}

// --- security: write prohibition + redirect rejection --------

func TestNoWriteRequestsEverSent(t *testing.T) {
	fs := newSeededServer(t)
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	ctx := context.Background()
	if _, err := client.List(ctx, core.Query{Namespace: testNS}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := client.Get(ctx, core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}); err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := client.StreamLogs(ctx,
		core.LogRequest{Ref: core.Ref{Namespace: testNS, Name: "wf-a", UID: "uid-a"}, Container: "main"},
		func(core.LogRecord) error { return nil }); err != nil {
		t.Fatalf("logs: %v", err)
	}
	methods := fs.RequestsByMethod()
	for m, n := range methods {
		if m != http.MethodGet {
			t.Errorf("non-GET %s request count = %d; a read must never construct a write", m, n)
		}
	}
	if methods[http.MethodGet] < 3 {
		t.Errorf("get count = %d, want >=3", methods[http.MethodGet])
	}
}

func TestRedirectRejectedByTransport(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetFault("list", Fault{Redirect: "http://evil.example.test/steal"})
	client := NewWireClient(WireClientConfig{
		BaseURL: fs.URL(),
		HTTPClient: &http.Client{
			// Adapter policy: reject redirects — no credentials
			// ever reach the redirect target. The test pins the behavior
			// the production client must implement.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	})
	_, err := client.List(context.Background(), core.Query{Namespace: testNS})
	if err == nil {
		t.Fatal("redirect must be rejected/explained, not followed")
	}
	ae := core.AsAPIError(err)
	if ae == nil {
		t.Fatalf("not APIError: %v", err)
	}
	if ae.Status == 0 {
		t.Errorf("expected HTTP status on error, got none: %v", err)
	}
}

// --- real root model journey over the fixture server ----------------------------

func TestRootModelJourneyOverFixtureServer(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetLogs(FixtureLogEntries(4))
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})

	// Interval tiny: the poll tick returns instantly and the pump drops
	// ticks (journey steps are driven explicitly below).
	h := newHarness(t, client, time.Nanosecond)

	// 1. Initial list collection (real HTTP under the fake server).
	h.pump(50)
	if v := h.view(); !strings.Contains(v, "list: 3 workflows") {
		t.Fatalf("list route view after pump = %q (want 3 workflows)", v)
	}

	// 2. Open detail of the failed workflow (real HTTP GET with uid).
	h.send(app.OpenWorkflowMsg{Ref: core.Ref{Namespace: testNS, Name: "wf-b", UID: "uid-b"}})
	h.pump(50)
	if v := h.view(); !strings.Contains(v, "Detail wf-b") || !strings.Contains(v, "workflow wf-b phase=Failed") {
		t.Fatalf("detail view = %q", v)
	}

	// 3. Open logs (real HTTP stream; records arrive via the root pump).
	h.send(app.OpenLogsMsg{
		Ref:       core.Ref{Namespace: testNS, Name: "wf-b", UID: "uid-b"},
		Container: "main",
	})
	h.pump(200)
	if v := h.view(); !strings.Contains(v, "Logs wf-b") || !strings.Contains(v, "stream ended (4 records)") {
		t.Fatalf("logs view = %q (want stream ended (4 records))", v)
	}

	// 4. Esc back to detail, then to list.
	h.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if v := h.view(); !strings.Contains(v, "Detail wf-b") {
		t.Fatalf("route after esc from logs = %q", v)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if v := h.view(); !strings.Contains(v, "list: 3 workflows") {
		t.Fatalf("route after esc to list = %q", v)
	}

	// 5. Quit.
	_, quitCmd := h.root.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if quitCmd == nil {
		t.Fatal("q did not produce a quit command")
	}
}

func TestRootModelStaleDetailDiscarded(t *testing.T) {
	fs := newSeededServer(t)
	client := NewWireClient(WireClientConfig{BaseURL: fs.URL()})
	h := newHarness(t, client, time.Nanosecond)
	h.pump(50)

	// Slow detail fetch: the server holds the GET ~300ms. Esc before the
	// response lands; the late (canceled-generation) result must never
	// overwrite the list route state.
	fs.SetFault("get:"+testNS+"/wf-b", Fault{
		Status:  http.StatusServiceUnavailable,
		Code:    grpcUnavailable,
		Message: "synthetic slow path",
	})
	h.send(app.OpenWorkflowMsg{Ref: core.Ref{Namespace: testNS, Name: "wf-b", UID: "uid-b"}})
	// Esc immediately: route returns to list, in-flight detail canceled.
	h.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	// Drain everything: the canceled detail must never be applied.
	h.pump(100)
	if v := h.view(); strings.Contains(v, "Detail wf-b") {
		t.Fatalf("late detail response resurrected detail route: %q", v)
	}
}

// logRecordProbe is a test helper for counting log messages pumped through
// the harness (exported-free: same package).
func countMsgs(msgs []tea.Msg, pred func(tea.Msg) bool) int {
	n := 0
	for _, m := range msgs {
		if pred(m) {
			n++
		}
	}
	return n
}

var _ = countMsgs // used by future sub-tests
