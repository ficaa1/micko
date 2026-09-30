//go:build integration

package integration

// FixtureServer is an in-process Argo Server for the root journeys. It
// serves the read endpoints the app opens on the list, detail and log
// routes, in Argo's wire shapes:
//   - GET /api/v1/workflows/{ns}: every seeded workflow of the namespace.
//   - GET /api/v1/workflows/{ns}/{name}: one workflow, or a 404.
//   - GET /api/v1/workflows/{ns}/{name}/log: JSON lines of
//     {"result":{"content","podName"}}, and an in-band
//     {"error":{"code","message"}} chunk after them when one is set.
//   - GET /api/v1/workflow-events/{ns}: a watch that stays open and quiet
//     until the client leaves, as a server with no changes does.
//
// Every payload is synthetic.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// gRPC codes the gateway puts in its error envelopes.
const (
	grpcNotFound   = 5
	grpcPermDenied = 7
	grpcInternal   = 13
)

// WireWorkflow is the part of a v1alpha1.Workflow the client reads.
type WireWorkflow struct {
	Metadata WireMetadata       `json:"metadata"`
	Status   WireWorkflowStatus `json:"status"`
	Spec     map[string]any     `json:"spec,omitempty"`
}

// WireMetadata is the object metadata the client reads.
type WireMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	ResourceVersion   string            `json:"resourceVersion"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

// WireWorkflowStatus is the status the client reads.
type WireWorkflowStatus struct {
	Phase      string              `json:"phase,omitempty"`
	Message    string              `json:"message,omitempty"`
	StartedAt  string              `json:"startedAt,omitempty"`
	FinishedAt string              `json:"finishedAt,omitempty"`
	Nodes      map[string]WireNode `json:"nodes,omitempty"`
}

// WireNode is one status.nodes entry.
type WireNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Type        string `json:"type"`
	Phase       string `json:"phase,omitempty"`
	BoundaryID  string `json:"boundaryID,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	PodName     string `json:"podName,omitempty"`
}

// WireLogEntry is a log record: Argo sends the content and the pod, nothing
// more.
type WireLogEntry struct {
	Content string `json:"content"`
	PodName string `json:"podName"`
}

// Fault is an error a test makes an endpoint answer with: an HTTP status
// with the gateway's envelope, or with Status zero an in-band log error.
type Fault struct {
	Status  int
	Code    int
	Message string
}

// FixtureServer is the fake Argo Server.
type FixtureServer struct {
	srv *httptest.Server

	mu       sync.Mutex
	live     map[string]WireWorkflow // by ns/name
	logs     []WireLogEntry
	logsErr  *Fault
	faults   map[string]Fault // "list" or "get:<ns>/<name>"
	requests map[string]int   // by method
}

// NewFixtureServer starts a fixture server on loopback that closes with t.
func NewFixtureServer(t *testing.T) *FixtureServer {
	t.Helper()
	fs := &FixtureServer{live: map[string]WireWorkflow{}, faults: map[string]Fault{}, requests: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/workflows/", fs.routeWorkflows)
	mux.HandleFunc("/api/v1/workflow-events/", fs.handleWatch)
	fs.srv = httptest.NewServer(mux)
	// A watch the model never closed is still open when the test ends, and
	// Close waits for open requests, so their connections go first.
	t.Cleanup(func() {
		fs.srv.CloseClientConnections()
		fs.srv.Close()
	})
	return fs
}

// URL is the server's base URL.
func (fs *FixtureServer) URL() string { return fs.srv.URL }

// PutWorkflow seeds a workflow, replacing one of the same name.
func (fs *FixtureServer) PutWorkflow(wf WireWorkflow) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.live[wf.Metadata.Namespace+"/"+wf.Metadata.Name] = wf
}

// SetLogs seeds the records every log stream sends.
func (fs *FixtureServer) SetLogs(entries []WireLogEntry) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.logs = entries
}

// SetLogError ends every log stream with an in-band error after its records.
func (fs *FixtureServer) SetLogError(f Fault) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.logsErr = &f
}

// SetFault makes an endpoint answer with f: "list", or "get:<ns>/<name>".
func (fs *FixtureServer) SetFault(endpoint string, f Fault) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.faults[endpoint] = f
}

// RequestsByMethod counts the requests received so far by method.
func (fs *FixtureServer) RequestsByMethod() map[string]int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := map[string]int{}
	for m, n := range fs.requests {
		out[m] = n
	}
	return out
}

// routeWorkflows serves the list, get and log endpoints.
func (fs *FixtureServer) routeWorkflows(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	fs.requests[r.Method]++
	fs.mu.Unlock()
	if r.Method != http.MethodGet {
		writeError(w, Fault{Status: http.StatusMethodNotAllowed, Code: grpcInternal, Message: "the fixture serves reads only"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/"), "/")
	switch {
	case len(parts) == 1:
		fs.handleList(w, parts[0])
	case len(parts) == 2:
		fs.handleGet(w, parts[0], parts[1])
	case len(parts) == 3 && parts[2] == "log":
		fs.handleLogs(w, parts[0], parts[1])
	default:
		writeError(w, Fault{Status: http.StatusNotFound, Code: grpcNotFound, Message: "unknown path " + r.URL.Path})
	}
}

func (fs *FixtureServer) handleList(w http.ResponseWriter, ns string) {
	fs.mu.Lock()
	f, faulted := fs.faults["list"]
	var items []WireWorkflow
	for key, wf := range fs.live {
		if strings.HasPrefix(key, ns+"/") {
			items = append(items, wf)
		}
	}
	fs.mu.Unlock()
	if faulted {
		writeError(w, f)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Metadata.Name < items[j].Metadata.Name })
	if items == nil {
		items = []WireWorkflow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": map[string]any{"resourceVersion": "100"}, "items": items})
}

func (fs *FixtureServer) handleGet(w http.ResponseWriter, ns, name string) {
	fs.mu.Lock()
	f, faulted := fs.faults["get:"+ns+"/"+name]
	wf, ok := fs.live[ns+"/"+name]
	fs.mu.Unlock()
	switch {
	case faulted:
		writeError(w, f)
	case !ok:
		writeError(w, Fault{Status: http.StatusNotFound, Code: grpcNotFound, Message: fmt.Sprintf("workflows.argoproj.io %q not found", name)})
	default:
		writeJSON(w, http.StatusOK, wf)
	}
}

func (fs *FixtureServer) handleLogs(w http.ResponseWriter, ns, name string) {
	fs.mu.Lock()
	_, known := fs.live[ns+"/"+name]
	logs := append([]WireLogEntry(nil), fs.logs...)
	logErr := fs.logsErr
	fs.mu.Unlock()
	if !known {
		writeError(w, Fault{Status: http.StatusNotFound, Code: grpcNotFound, Message: fmt.Sprintf("workflows.argoproj.io %q not found", name)})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	for _, e := range logs {
		_ = enc.Encode(map[string]any{"result": e})
	}
	if logErr != nil {
		_ = enc.Encode(map[string]any{"error": map[string]any{"code": logErr.Code, "message": logErr.Message}})
	}
}

// handleWatch holds a watch open until the client leaves.
func (fs *FixtureServer) handleWatch(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	fs.requests[r.Method]++
	fs.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the gateway's {"code","message"} envelope.
func writeError(w http.ResponseWriter, f Fault) {
	writeJSON(w, f.Status, map[string]any{"code": f.Code, "message": f.Message})
}
