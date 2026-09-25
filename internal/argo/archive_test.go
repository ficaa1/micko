package argo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
)

// archiveListBody is an archive list page as Argo returns it: the archive's
// own projection (metadata and status, no spec, no node map) and an offset
// as the continuation token.
const archiveListBody = `{
  "metadata": {"continue": "100"},
  "items": [
    {"metadata": {"name": "etl-1", "namespace": "team-a", "uid": "arch-1",
       "creationTimestamp": "2026-09-01T10:00:00Z",
       "labels": {"workflows.argoproj.io/phase": "Succeeded"}},
     "status": {"phase": "Succeeded", "startedAt": "2026-09-01T10:00:01Z",
       "finishedAt": "2026-09-01T10:06:00Z", "progress": "2/2"}},
    {"metadata": {"name": "etl-2", "namespace": "team-a", "uid": "arch-2",
       "creationTimestamp": "2026-09-01T09:00:00Z"},
     "status": {"phase": "Failed", "message": "child 'check' failed",
       "startedAt": "2026-09-01T09:00:01Z", "finishedAt": "2026-09-01T09:04:00Z"}}
  ]
}`

// The list is narrowed by the metadata.namespace field selector, pages with
// limit and the verbatim offset token, and keeps each item's raw object.
func TestListArchivedWorkflows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/archived-workflows" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := q.Get("listOptions.fieldSelector"); got != "metadata.namespace=team-a" {
			t.Errorf("fieldSelector = %q", got)
		}
		if q.Get("listOptions.limit") != "100" || q.Get("listOptions.continue") != "0" || q.Get("listOptions.labelSelector") != "a=b" {
			t.Errorf("query = %v", q)
		}
		if q.Has("namespace") {
			t.Error("the namespace query parameter was sent; older servers do not read it")
		}
		io.WriteString(w, archiveListBody)
	}))
	defer srv.Close()
	page, err := newTestClient(t, srv).ListArchivedWorkflows(context.Background(),
		core.ArchiveQuery{Namespace: "team-a", LabelSelector: "a=b", Limit: 100, Continue: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Continue != "100" || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	a, b := page.Items[0], page.Items[1]
	if a.Summary.Ref.UID != "arch-1" || a.Summary.Phase != "Succeeded" || a.Summary.FinishedAt == nil || a.Summary.Progress != "2/2" {
		t.Fatalf("item 0 = %+v", a.Summary)
	}
	if b.Summary.Message != "child 'check' failed" || !strings.Contains(string(b.Resource), `"arch-2"`) {
		t.Fatalf("item 1 = %+v", b)
	}
}

// Every namespace sends no field selector; a server with no archive answers
// an empty page, which is not an error.
func TestListArchivedWorkflowsAllNamespacesAndEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			io.WriteString(w, `{}`)
			return
		}
		if r.URL.Query().Has("listOptions.fieldSelector") {
			t.Error("a field selector was sent for every namespace")
		}
		io.WriteString(w, `{"metadata":{},"items":null}`)
	}))
	defer srv.Close()
	page, err := newTestClient(t, srv).ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Limit: 100})
	if err != nil || len(page.Items) != 0 || page.Continue != "" {
		t.Fatalf("page = %+v, %v", page, err)
	}
}

// The detail route returns a whole Workflow, node map included, keyed by
// UID alone.
func TestGetArchivedWorkflow(t *testing.T) {
	body := `{"metadata":{"name":"etl-1","namespace":"team-a","uid":"arch-1",
	  "annotations":{"workflows.argoproj.io/pod-name-format":"v2"}},
	  "spec":{"entrypoint":"main"},
	  "status":{"phase":"Succeeded","nodes":{"etl-1":{"id":"etl-1","name":"etl-1","type":"Pod","phase":"Succeeded","templateName":"main"}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/archived-workflows/arch-1" || r.URL.RawQuery != "" {
			t.Errorf("request = %s", r.URL)
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	wf, err := newTestClient(t, srv).GetArchivedWorkflow(context.Background(), "arch-1")
	if err != nil {
		t.Fatal(err)
	}
	if wf.Summary.Ref.Name != "etl-1" || !wf.NodesAvailable || len(wf.Nodes) != 1 || wf.Nodes["etl-1"].PodName == "" {
		t.Fatalf("workflow = %+v", wf)
	}
	if !strings.Contains(string(wf.Resource), `"entrypoint":"main"`) {
		t.Fatal("raw object not kept")
	}
}

// A server with no archive, or no archive route, is reported as the archive
// not being enabled; a refusal, and a UID the archive does not hold, keep
// their own kinds.
func TestArchiveErrors(t *testing.T) {
	cases := []struct {
		name   string
		get    bool
		status int
		body   string
		kind   core.ErrorKind
		msg    string
	}{
		{"null archive get", true, 500, `{"code":13,"message":"getting archived workflows not supported"}`,
			core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"no route list", false, 404, `{"code":5,"message":"Not Found"}`, core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"unimplemented list", false, 501, `{"code":12,"message":"method ListArchivedWorkflows not implemented"}`,
			core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"forbidden list", false, 403, `{"code":7,"message":"Permission denied, you are not allowed to list workflows in namespace \"team-a\"."}`,
			core.ErrForbidden, "not allowed to list workflows"},
		{"unknown uid", true, 404, `{"code":5,"message":"not found"}`, core.ErrNotFound, "not found"},
		{"database down", false, 500, `{"code":13,"message":"dial tcp 10.0.0.9:5432: connect: connection refused"}`,
			core.ErrUnavailable, "connection refused"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		}))
		cl := newTestClient(t, srv)
		var err error
		if c.get {
			_, err = cl.GetArchivedWorkflow(context.Background(), "arch-1")
		} else {
			_, err = cl.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Namespace: "team-a"})
		}
		srv.Close()
		ae := core.AsAPIError(err)
		if ae == nil || ae.Kind != c.kind || !strings.Contains(ae.Message, c.msg) {
			t.Errorf("%s: err = %v, want %s containing %q", c.name, err, c.kind, c.msg)
		}
	}
}
