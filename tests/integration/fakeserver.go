//go:build integration

package integration

// fakeserver.go — an in-process httptest server speaking the pinned
// Argo Workflows v4.1.2 wire contract (docs/development.md).
//
// Verified wire facts implemented here (pinned sources in docs/development.md):
//   - GET /api/v1/workflows/{namespace}: listOptions.limit is a
//     string-encoded integer; listOptions.continue is the next integer
//     OFFSET as a decimal string (v4.1.2 offset pagination); response
//     carries metadata.continue (only when more items exist) and
//     metadata.resourceVersion (opaque string).
//   - GET /api/v1/workflows/{namespace}/{name}?uid=…: a UID mismatch must
//     not silently return the live object — the server falls back to the
//     archive; the fake archives replaced generations so same-name
//     replacement tests exercise the real semantics.
//   - GET /api/v1/workflows/{namespace}/{name}/log: JSON-lines framing
//     {"result":{"content":…,"podName":…}}\n per chunk (gateway v1.16.0
//     default) with the SSE variant behind a server flag (both
//     framings per the v0.1 resolution). LogEntry carries exactly
//     content+podName — no timestamp/container field exists.
//   - In-band errors are {"error":{"code":<grpc>,"message":…}} FINAL
//     chunks with HTTP 200; pre-first-chunk errors are unary-style HTTP
//     errors with the mapped status.
//   - GET /api/v1/version: {"version":"v4.1.2",…} for smoke checks.
//   - Keepalive: streaming responses flush headers before the first
//     payload.
//
// This server is independent of internal/testkit's FakeReader (the in-memory
// core.Reader fake). Here the CLIENT side is real HTTP; only the SERVER is
// faked. Every payload is SYNTHETIC.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// gRPC code numbers used by the gateway round-trip (docs/development.md).
const (
	grpcInvalid       = 3
	grpcNotFound      = 5
	grpcPermDenied    = 7
	grpcInternal      = 13
	grpcUnimplemented = 12
	grpcUnavailable   = 14
	grpcUnauth        = 16
)

// WireWorkflow is the subset of the upstream v1alpha1.Workflow JSON shape
// the client consumes. Field names follow the pinned swagger.
type WireWorkflow struct {
	Metadata WireMetadata       `json:"metadata"`
	Status   WireWorkflowStatus `json:"status"`
	Spec     map[string]any     `json:"spec,omitempty"`
}

// WireMetadata is the object metadata projection.
type WireMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	ResourceVersion   string            `json:"resourceVersion"`
	Generation        int64             `json:"generation,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

// WireWorkflowStatus is the status projection (phase/message/timestamps/
// nodes). Nodes may be absent entirely (offloaded/unhydrated fixtures).
type WireWorkflowStatus struct {
	Phase                    string              `json:"phase,omitempty"`
	Message                  string              `json:"message,omitempty"`
	StartedAt                string              `json:"startedAt,omitempty"`
	FinishedAt               string              `json:"finishedAt,omitempty"`
	Nodes                    map[string]WireNode `json:"nodes,omitempty"`
	OffloadNodeStatusVersion string              `json:"offloadNodeStatusVersion,omitempty"`
}

// WireNode is one status.nodes entry (docs/development.md semantics).
type WireNode struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName,omitempty"`
	Type          string   `json:"type"`
	Phase         string   `json:"phase,omitempty"`
	Message       string   `json:"message,omitempty"`
	BoundaryID    string   `json:"boundaryID,omitempty"`
	Children      []string `json:"children,omitempty"`
	OutboundNodes []string `json:"outboundNodes,omitempty"`
	StartedAt     string   `json:"startedAt,omitempty"`
	FinishedAt    string   `json:"finishedAt,omitempty"`
	PodName       string   `json:"podName,omitempty"`
}

// WireLogEntry is the LogEntry envelope payload: EXACTLY content+podName
// (docs/development.md — no timestamp, no container field).
type WireLogEntry struct {
	Content string `json:"content"`
	PodName string `json:"podName"`
}

// WireVersion is the /api/v1/version payload (subset).
type WireVersion struct {
	Version   string `json:"version"`
	BuildOS   string `json:"buildOS,omitempty"`
	BuildDate string `json:"buildDate,omitempty"`
}

// Fault is a per-endpoint fault injected by tests.
type Fault struct {
	// Status: when non-zero, respond with this HTTP status and a unary
	// error envelope ({"code","message"}), mapped like the gateway.
	Status int
	// Code: gRPC code for the error envelope (default derived from Status).
	Code int
	// Message: error message text.
	Message string
	// ContentType + Body: literal body override (login-page HTML,
	// partial/invalid JSON fixtures).
	ContentType string
	Body        []byte
	// Redirect: 3xx Location to test redirect rejection.
	Redirect string
}

// ServerConfig configures fixture-server behavior.
type ServerConfig struct {
	// SSELogs switches the log stream to SSE framing (data: prefix,
	// blank-line separation) instead of bare JSON-lines (docs/development.md).
	SSELogs bool
	// SlowLogDrip delays between log chunks (cancellation tests).
	SlowLogDrip time.Duration
	// BrokenJSONList makes the list handler emit invalid JSON
	// (a deliberately broken API response must make a test fail).
	BrokenJSONList bool
	// TruncateLogNewline cuts the log body mid-final-line, simulating a
	// connection severed mid-chunk (docs/development.md case 3: "mid-stream
	// cancellation may truncate"; the partial line is parseable only if it
	// is complete JSON, otherwise it must surface as protocol error).
	TruncateLogNewline bool
}

// FixtureServer is the Argo v4.1.2 wire fixture.
type FixtureServer struct {
	srv *httptest.Server

	mu      sync.Mutex
	live    map[string]WireWorkflow // key ns/name (current generation)
	archive map[string]WireWorkflow // previous generations (UID fallback)
	logs    []WireLogEntry
	logsErr *Fault
	faults  map[string]Fault // by endpoint key: list|get|get:ns/name|logs|version
	cfg     ServerConfig

	// Request log for assertions (proves only GETs were
	// sent; no mutation request is ever constructed).
	muReqs   sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
}

// NewFixtureServer starts an httptest server (plain HTTP loopback —
// loopback http is allowed for dev/demo/test).
func NewFixtureServer(t *testing.T, cfg ServerConfig) *FixtureServer {
	t.Helper()
	fs := &FixtureServer{
		live:    map[string]WireWorkflow{},
		archive: map[string]WireWorkflow{},
		faults:  map[string]Fault{},
		cfg:     cfg,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/version", fs.handleVersion)
	mux.HandleFunc("/api/v1/workflows/", fs.routeWorkflows)
	fs.srv = httptest.NewServer(mux)
	t.Cleanup(fs.srv.Close)
	return fs
}

// URL returns the server base URL (loopback http).
func (fs *FixtureServer) URL() string { return fs.srv.URL }

// PutWorkflow seeds or replaces a workflow. Replacing an existing entry
// moves the OLD object into the fake archive (same-name replacement with
// UID fallback semantics, docs/development.md).
func (fs *FixtureServer) PutWorkflow(wf WireWorkflow) {
	key := wf.Metadata.Namespace + "/" + wf.Metadata.Name
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if old, ok := fs.live[key]; ok {
		fs.archive[old.Metadata.UID] = old
	}
	fs.live[key] = wf
}

// PutArchiveOnly seeds an archived (deleted-live) workflow: visible via
// uid-addressed detail GET only, never in the live list (docs/development.md).
func (fs *FixtureServer) PutArchiveOnly(wf WireWorkflow) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.archive[wf.Metadata.UID] = wf
}

// SetLogs seeds the log stream payload.
func (fs *FixtureServer) SetLogs(entries []WireLogEntry) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.logs = entries
}

// SetLogError makes the log endpoint terminate streams with an in-band
// error chunk after delivering any seeded entries (docs/development.md).
func (fs *FixtureServer) SetLogError(f Fault) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.logsErr = &f
}

// SetFault installs a Fault for an endpoint key: "list", "get",
// "get:<ns>/<name>", "logs" or "version".
func (fs *FixtureServer) SetFault(endpoint string, f Fault) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.faults[endpoint] = f
}

// RequestsByMethod counts recorded requests (thread-safe copy).
func (fs *FixtureServer) RequestsByMethod() map[string]int {
	fs.muReqs.Lock()
	defer fs.muReqs.Unlock()
	out := map[string]int{}
	for _, r := range fs.requests {
		out[r.Method]++
	}
	return out
}

// RecordedPaths lists distinct "METHOD path" values seen so far.
func (fs *FixtureServer) RecordedPaths() []string {
	fs.muReqs.Lock()
	defer fs.muReqs.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range fs.requests {
		k := r.Method + " " + r.Path
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// QueryValues returns the raw query of the last request matching a path
// prefix (for asserting the exact wire parameters the client sent).
func (fs *FixtureServer) QueryValues(pathPrefix string) []string {
	fs.muReqs.Lock()
	defer fs.muReqs.Unlock()
	var out []string
	for _, r := range fs.requests {
		if strings.HasPrefix(r.Path, pathPrefix) {
			out = append(out, r.Query)
		}
	}
	return out
}

func (fs *FixtureServer) record(r *http.Request) (Fault, bool) {
	fs.muReqs.Lock()
	fs.requests = append(fs.requests, recordedRequest{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
	})
	fs.muReqs.Unlock()

	fs.mu.Lock()
	defer fs.mu.Unlock()
	var key string
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/")
	switch {
	case strings.HasSuffix(r.URL.Path, "/log"):
		key = "logs"
	case r.URL.Path == "/api/v1/version":
		key = "version"
	case strings.Count(rest, "/") >= 1:
		parts := strings.SplitN(rest, "/", 2)
		key = "get:" + parts[0] + "/" + parts[1]
		if idx := strings.Index(key, "/log"); idx >= 0 {
			key = key[:idx]
		}
		if k, ok := fs.faults[key]; ok {
			return k, true
		}
		key = "get"
	default:
		key = "list"
	}
	f, ok := fs.faults[key]
	return f, ok
}

// handleVersion serves GET /api/v1/version.
func (fs *FixtureServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeUnaryError(w, http.StatusMethodNotAllowed, grpcInternal, "method not allowed")
		return
	}
	if f, ok := fs.record(r); ok {
		applyFault(w, r, f)
		return
	}
	writeJSON(w, http.StatusOK, WireVersion{Version: "v4.1.2", BuildOS: "synthetic-fixture"})
}

// routeWorkflows dispatches the workflow endpoints.
func (fs *FixtureServer) routeWorkflows(w http.ResponseWriter, r *http.Request) {
	if f, ok := fs.record(r); ok {
		applyFault(w, r, f)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/")
	if path == "" {
		// /api/v1/workflows without a namespace — micko never scans all
		// namespaces; the fixture rejects it.
		writeUnaryError(w, http.StatusNotFound, grpcNotFound, "namespace required")
		return
	}
	if r.Method != http.MethodGet {
		// Alpha write prohibition: any non-GET reaching the fake server is
		// a client bug. Recorded (see record) and answered 405 so
		// a request-count assertion catches it loudly.
		writeUnaryError(w, http.StatusMethodNotAllowed, grpcInternal,
			"fixture server: write requests are prohibited in alpha")
		return
	}
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 1:
		fs.handleList(w, r, parts[0])
	case len(parts) == 2:
		fs.handleGet(w, r, parts[0], parts[1])
	case len(parts) == 3 && parts[2] == "log":
		fs.handleLogs(w, r, parts[0], parts[1])
	default:
		writeUnaryError(w, http.StatusNotFound, grpcNotFound, "unknown fixture path: "+r.URL.Path)
	}
}

// writeJSON writes a JSON body with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeUnaryError writes the gateway-style {"code","message"} envelope with
// the mapped HTTP status (docs/development.md).
func writeUnaryError(w http.ResponseWriter, status, code int, msg string) {
	writeJSON(w, status, map[string]any{"code": code, "message": msg})
}

// applyFault applies a Fault to a response writer.
func applyFault(w http.ResponseWriter, r *http.Request, f Fault) {
	if f.Redirect != "" {
		http.Redirect(w, r, f.Redirect, http.StatusFound)
		return
	}
	status := f.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	code := f.Code
	if code == 0 {
		code = grpcInternal
	}
	if f.ContentType != "" || len(f.Body) > 0 {
		ct := f.ContentType
		if ct == "" {
			ct = "application/json"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		if len(f.Body) > 0 {
			_, _ = w.Write(f.Body)
		}
		return
	}
	writeUnaryError(w, status, code, f.Message)
}

// handleList implements GET /api/v1/workflows/{ns} with v4.1.2 offset
// pagination over the seeded live workflows (deterministic order: by name).
func (fs *FixtureServer) handleList(w http.ResponseWriter, r *http.Request, ns string) {
	if fs.cfg.BrokenJSONList {
		// Deliberately broken API response: HTTP 200, invalid JSON.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"broken"`))
		return
	}
	q := r.URL.Query()
	limit := 0
	if v := q.Get("listOptions.limit"); v != "" {
		n, err := strconv.Atoi(v) // string-encoded integer on the wire
		if err != nil || n < 0 {
			writeUnaryError(w, http.StatusBadRequest, grpcInvalid, "listOptions.limit: invalid value")
			return
		}
		limit = n
	}
	offset := 0
	if v := q.Get("listOptions.continue"); v != "" {
		n, err := strconv.Atoi(v) // v4.1.2 continuation = decimal offset string
		if err != nil || n < 0 {
			writeUnaryError(w, http.StatusBadRequest, grpcInvalid, "listOptions.continue: invalid offset")
			return
		}
		offset = n
	}
	selector := q.Get("listOptions.labelSelector")

	fs.mu.Lock()
	var items []WireWorkflow
	for key, wf := range fs.live {
		if strings.HasPrefix(key, ns+"/") && selectorMatches(selector, wf.Metadata.Labels) {
			items = append(items, wf)
		}
	}
	fs.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		return items[i].Metadata.Name < items[j].Metadata.Name
	})

	rv := ""
	if len(items) > 0 {
		rv = items[0].Metadata.ResourceVersion
	}
	meta := map[string]any{"resourceVersion": rv}
	if offset > 0 {
		if offset >= len(items) {
			writeJSON(w, http.StatusOK, map[string]any{
				"metadata": meta, "items": []any{},
			})
			return
		}
		items = items[offset:]
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
		meta["continue"] = strconv.Itoa(offset + limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metadata": meta, "items": items,
	})
}

// selectorMatches implements the exact-match selector form micko v0.1
// sends (`key=value`, comma-joined pairs are ANDed).
func selectorMatches(selector string, labels map[string]string) bool {
	if selector == "" {
		return true
	}
	for _, pair := range strings.Split(selector, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 || labels[kv[0]] != kv[1] {
			return false
		}
	}
	return true
}

// handleGet implements GET /api/v1/workflows/{ns}/{name} with the uid
// fallback semantics (docs/development.md): uid mismatch → archive lookup; both
// miss → 404 with the live error preserved.
func (fs *FixtureServer) handleGet(w http.ResponseWriter, r *http.Request, ns, name string) {
	wantUID := r.URL.Query().Get("uid")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	live, liveOK := fs.live[ns+"/"+name]
	if wantUID == "" && liveOK {
		writeJSON(w, http.StatusOK, live)
		return
	}
	if wantUID != "" {
		if liveOK && live.Metadata.UID == wantUID {
			writeJSON(w, http.StatusOK, live)
			return
		}
		if archived, ok := fs.archive[wantUID]; ok &&
			archived.Metadata.Namespace == ns && archived.Metadata.Name == name {
			writeJSON(w, http.StatusOK, archived)
			return
		}
		// Neither matched: original (live) error preserved.
		writeUnaryError(w, http.StatusNotFound, grpcNotFound,
			fmt.Sprintf("workflow %s/%s with uid %q not found", ns, name, wantUID))
		return
	}
	writeUnaryError(w, http.StatusNotFound, grpcNotFound,
		fmt.Sprintf("workflow %s/%s not found", ns, name))
}

// handleLogs implements GET /api/v1/workflows/{ns}/{name}/log with both
// framings (docs/development.md). Server behavior mirrored: fetch-then-validate
// the workflow first; unknown workflow → 404 before any chunk.
func (fs *FixtureServer) handleLogs(w http.ResponseWriter, r *http.Request, ns, name string) {
	q := r.URL.Query()
	podName := q.Get("podName")
	tail := q.Get("logOptions.tailLines")
	if tail != "" {
		if _, err := strconv.ParseInt(tail, 10, 64); err != nil {
			writeUnaryError(w, http.StatusBadRequest, grpcInvalid, "logOptions.tailLines: invalid value")
			return
		}
	}
	fs.mu.Lock()
	_, known := fs.live[ns+"/"+name]
	logs := append([]WireLogEntry(nil), fs.logs...)
	var logErr *Fault
	if fs.logsErr != nil {
		f := *fs.logsErr
		logErr = &f
	}
	sse, drip, trunc := fs.cfg.SSELogs, fs.cfg.SlowLogDrip, fs.cfg.TruncateLogNewline
	fs.mu.Unlock()

	if !known {
		writeUnaryError(w, http.StatusNotFound, grpcNotFound,
			fmt.Sprintf("workflow %s/%s not found", ns, name))
		return
	}
	if f, ok := fs.faultLookup("logs"); ok {
		applyFault(w, r, f)
		return
	}

	// Server-side selection: podName filter + tailLines (swagger semantics).
	if podName != "" {
		filtered := logs[:0]
		for _, e := range logs {
			if e.PodName == podName {
				filtered = append(filtered, e)
			}
		}
		logs = filtered
	}
	if tail != "" {
		n, _ := strconv.ParseInt(tail, 10, 64)
		if n >= 0 && int64(len(logs)) > n {
			logs = logs[len(logs)-int(n):]
		}
	}

	body := buildLogBody(logs, logErr, sse)
	if trunc {
		body = truncateMidLine(body)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeUnaryError(w, http.StatusInternalServerError, grpcInternal, "streaming unsupported")
		return
	}
	ct := "application/json"
	if sse {
		ct = "text/event-stream"
	}
	// Eager header flush before first payload.
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Stream the body: with a drip configured, write progressively in
	// small steps (forcing split reads, aborting on client cancellation);
	// otherwise write the whole body in one go.
	const step = 8 // bytes per drip step — forces split reads on small bodies
	if drip == 0 {
		if _, err := w.Write(body); err != nil {
			return
		}
		flusher.Flush()
		return
	}
	for off := 0; off < len(body); off += step {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(drip):
		}
		end := off + step
		if end > len(body) {
			end = len(body)
		}
		if _, err := w.Write(body[off:end]); err != nil {
			return
		}
		flusher.Flush()
	}
}

// buildLogBody renders log entries + optional in-band error in the selected
// framing. JSON-lines: {"result":{...}}\n per chunk. SSE:
// data: {"result":{...}}\n\n per event.
func buildLogBody(logs []WireLogEntry, logErr *Fault, sse bool) []byte {
	var b strings.Builder
	for _, e := range logs {
		payload, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if sse {
			fmt.Fprintf(&b, "data: {\"result\":%s}\n\n", payload)
		} else {
			fmt.Fprintf(&b, "{\"result\":%s}\n", payload)
		}
	}
	if logErr != nil {
		code := logErr.Code
		if code == 0 {
			code = grpcInternal
		}
		chunk := fmt.Sprintf(`{"error":{"code":%d,"message":%q}}`, code, logErr.Message)
		if sse {
			fmt.Fprintf(&b, "data: %s\n\n", chunk)
		} else {
			b.WriteString(chunk + "\n")
		}
	}
	return []byte(b.String())
}

// truncateMidLine simulates a stream severed mid-chunk: the final line is
// cut partway through its JSON payload.
func truncateMidLine(b []byte) []byte {
	// Locate the final (newline-terminated) line.
	end := len(b)
	if end > 0 && b[end-1] == '\n' {
		end-- // exclude the final newline
	}
	start := 0
	for i := end - 1; i >= 0; i-- {
		if b[i] == '\n' {
			start = i + 1
			break
		}
	}
	if end <= start {
		// No line to truncate; keep everything minus one byte.
		if end > 1 {
			return b[:end-1]
		}
		return b[:1]
	}
	cut := start + 12
	if cut >= end {
		cut = end - 1 // short lines: still cut mid-JSON
	}
	return b[:cut]
}

// faultLookup finds an endpoint fault without re-recording the request.
func (fs *FixtureServer) faultLookup(key string) (Fault, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.faults[key]
	return f, ok
}
