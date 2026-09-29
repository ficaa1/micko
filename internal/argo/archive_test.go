package argo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// The archive list is narrowed by the metadata.namespace field selector,
// pages with the verbatim offset token, and keeps each item's raw object.
func TestListArchivedWorkflows(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/archived-workflows" || q.Get("listOptions.fieldSelector") != "metadata.namespace=team-a" ||
			q.Get("listOptions.limit") != "100" || q.Get("listOptions.continue") != "0" || q.Get("listOptions.labelSelector") != "a=b" {
			t.Errorf("request = %s", r.URL)
		}
		if q.Has("namespace") {
			t.Error("the namespace parameter was sent; older servers do not read it")
		}
		_, _ = io.WriteString(w, `{
		  "metadata": {"continue": "100"},
		  "items": [
		    {"metadata": {"name": "etl-1", "namespace": "team-a", "uid": "arch-1", "creationTimestamp": "2026-09-01T10:00:00Z"},
		     "status": {"phase": "Succeeded", "startedAt": "2026-09-01T10:00:01Z", "finishedAt": "2026-09-01T10:06:00Z", "progress": "2/2"}},
		    {"metadata": {"name": "etl-2", "namespace": "team-a", "uid": "arch-2", "creationTimestamp": "2026-09-01T09:00:00Z"},
		     "status": {"phase": "Failed", "message": "child 'check' failed"}}
		  ]
		}`)
	})
	page, err := newTestClient(t, srv).ListArchivedWorkflows(context.Background(),
		core.ArchiveQuery{Namespace: "team-a", LabelSelector: "a=b", Limit: 100, Continue: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Continue != "100" || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	a, b := page.Items[0].Summary, page.Items[1]
	if a.Ref.UID != "arch-1" || a.Phase != "Succeeded" || a.FinishedAt == nil || a.Progress != "2/2" {
		t.Errorf("item 0 = %+v", a)
	}
	if b.Summary.Message != "child 'check' failed" || !strings.Contains(string(b.Resource), `"arch-2"`) {
		t.Errorf("item 1 = %+v", b)
	}
}

// An archived workflow is fetched by UID alone and decodes whole, node map
// and pod names included.
func TestGetArchivedWorkflow(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/archived-workflows/arch-1" || r.URL.RawQuery != "" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"metadata":{"name":"etl-1","namespace":"team-a","uid":"arch-1",
		  "annotations":{"workflows.argoproj.io/pod-name-format":"v1"}},
		  "spec":{"entrypoint":"main"},
		  "status":{"phase":"Succeeded","nodes":{"etl-1":{"name":"etl-1","type":"Pod","phase":"Succeeded","templateName":"main"}}}}`)
	})
	wf, err := newTestClient(t, srv).GetArchivedWorkflow(context.Background(), "arch-1")
	if err != nil {
		t.Fatal(err)
	}
	if wf.Summary.Ref.Name != "etl-1" || !wf.NodesAvailable || wf.Nodes["etl-1"].PodName != "etl-1" {
		t.Errorf("workflow = %+v", wf)
	}
	if !strings.Contains(string(wf.Resource), `"entrypoint":"main"`) {
		t.Error("raw object not kept")
	}
}

// A server without an archive reports it as not enabled; other failures keep
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
		{"null archive get", true, 500, `{"code":13,"message":"getting archived workflows not supported"}`, core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"no route list", false, 404, `{"code":5,"message":"Not Found"}`, core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"unimplemented list", false, 501, `{"code":12,"message":"method ListArchivedWorkflows not implemented"}`, core.ErrUnsupported, core.ArchiveDisabledMessage},
		{"forbidden list", false, 403, `{"code":7,"message":"Permission denied, you are not allowed to list workflows"}`, core.ErrForbidden, "not allowed"},
		{"unknown uid", true, 404, `{"code":5,"message":"not found"}`, core.ErrNotFound, "not found"},
		{"database down", false, 500, `{"code":13,"message":"dial tcp 10.0.0.9:5432: connect: connection refused"}`, core.ErrUnavailable, "connection refused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := newTestClient(t, serveBody(t, c.status, c.body))
			var err error
			if c.get {
				_, err = cl.GetArchivedWorkflow(context.Background(), "arch-1")
			} else {
				_, err = cl.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Namespace: "team-a"})
			}
			wantAPIError(t, err, c.kind, c.msg)
		})
	}
}
