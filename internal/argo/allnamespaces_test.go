package argo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
)

// clusterListBody is a cluster-wide list page as Argo returns it: the items
// carry their own metadata.namespace, which is how a row learns where it
// lives.
const clusterListBody = `{
  "metadata": {"resourceVersion": "rv-cluster-1"},
  "items": [
    {"metadata": {"name": "etl", "namespace": "team-a", "uid": "uid-a",
      "resourceVersion": "11", "creationTimestamp": "2026-09-08T10:00:00Z"},
     "status": {"phase": "Running", "startedAt": "2026-09-08T10:01:00Z"}},
    {"metadata": {"name": "etl", "namespace": "team-b", "uid": "uid-b",
      "resourceVersion": "12", "creationTimestamp": "2026-09-08T09:00:00Z"},
     "status": {"phase": "Succeeded", "startedAt": "2026-09-08T09:01:00Z",
      "finishedAt": "2026-09-08T09:07:00Z"}}
  ]
}`

// An empty namespace lists every namespace: the request path is
// /api/v1/workflows/ with its trailing slash, the one Argo's route matches
// with an empty namespace, and each row keeps its own namespace.
func TestListAllNamespacesPath(t *testing.T) {
	var infoHits, listHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/info":
			atomic.AddInt32(&infoHits, 1)
			io.WriteString(w, `{"links":[],"navColor":""}`)
		case r.URL.Path != "/api/v1/workflows/":
			t.Errorf("request path = %q, want /api/v1/workflows/", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		case isGateScan(r):
			serveNoGates(w)
		default:
			atomic.AddInt32(&listHits, 1)
			io.WriteString(w, clusterListBody)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	for i := 0; i < 2; i++ {
		page, err := c.List(context.Background(), core.Query{Limit: 100})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 2 || page.Items[0].Ref.Namespace != "team-a" || page.Items[1].Ref.Namespace != "team-b" {
			t.Fatalf("items = %+v, want one row per namespace", page.Items)
		}
	}
	if got := atomic.LoadInt32(&listHits); got != 2 {
		t.Fatalf("list requests = %d, want 2", got)
	}
	// The server's scope cannot change under a connection, so it is asked
	// once, not on every poll.
	if got := atomic.LoadInt32(&infoHits); got != 1 {
		t.Fatalf("info requests = %d, want 1", got)
	}
}

// A single namespace never consults the server's scope.
func TestListOneNamespaceSkipsTheScopeCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			t.Error("a namespaced list asked for the server's scope")
		}
		if isGateScan(r) {
			serveNoGates(w)
			return
		}
		io.WriteString(w, clusterListBody)
	}))
	defer srv.Close()
	if _, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "team-a"}); err != nil {
		t.Fatalf("List: %v", err)
	}
}

// A token that may not list cluster-wide gets Argo's 403, and it reaches the
// caller as a forbidden error with the server's message, not as an empty page.
func TestListAllNamespacesForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/info" {
			io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"code":7,"message":"Permission denied, you are not allowed to list workflows in namespace \"\". Maybe you want to specify a namespace with query parameter `+"`.namespace=`"+`?"}`)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv).List(context.Background(), core.Query{})
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrForbidden || ae.Status != http.StatusForbidden {
		t.Fatalf("error = %v, want forbidden/403", err)
	}
	if want := "Permission denied"; !strings.Contains(ae.Message, want) {
		t.Fatalf("message = %q, want the server's %q", ae.Message, want)
	}
}

// A server that manages one namespace is refused a cluster-wide list before
// it is asked: its answer would cover that one namespace and read as the
// whole cluster.
func TestListAllNamespacesOnAManagedServer(t *testing.T) {
	var listed int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/info" {
			io.WriteString(w, `{"managedNamespace":"argo"}`)
			return
		}
		atomic.AddInt32(&listed, 1)
		io.WriteString(w, clusterListBody)
	}))
	defer srv.Close()
	_, err := newTestClient(t, srv).List(context.Background(), core.Query{})
	ae := core.AsAPIError(err)
	if ae == nil || ae.Kind != core.ErrUnsupported || !strings.Contains(ae.Message, "manages namespace argo only") {
		t.Fatalf("error = %v, want the managed namespace named", err)
	}
	if atomic.LoadInt32(&listed) != 0 {
		t.Fatal("the cluster-wide list was sent to a managed-namespace server")
	}
}

// A server that will not state its scope is asked for the list anyway; its
// own answer is then the truth. A failed lookup is not cached.
func TestListAllNamespacesWhenInfoFails(t *testing.T) {
	var infoHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			atomic.AddInt32(&infoHits, 1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if isGateScan(r) {
			serveNoGates(w)
			return
		}
		io.WriteString(w, clusterListBody)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	for i := 0; i < 2; i++ {
		if _, err := c.List(context.Background(), core.Query{}); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if got := atomic.LoadInt32(&infoHits); got != 2 {
		t.Fatalf("info requests = %d, want a retry after the failure", got)
	}
}

// The watch across namespaces opens /api/v1/workflow-events/ with the
// trailing slash, and events keep their own namespace.
func TestWatchAllNamespacesPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflow-events/" {
			t.Errorf("watch path = %q, want /api/v1/workflow-events/", r.URL.Path)
		}
		io.WriteString(w, `{"result":{"type":"MODIFIED","object":{"metadata":{"namespace":"team-b","name":"etl","uid":"uid-b","resourceVersion":"13"},"status":{"phase":"Running"}}}}`+"\n")
	}))
	defer srv.Close()
	var got []core.WatchEvent
	err := newTestClient(t, srv).Watch(context.Background(), core.WatchRequest{ResourceVersion: "rv-cluster-1"},
		func(e core.WatchEvent) error { got = append(got, e); return nil })
	var we *core.WatchError
	if !errors.As(err, &we) || we.Kind != core.WatchEnded {
		t.Fatalf("Watch: %v", err)
	}
	if len(got) != 1 || got[0].Summary.Ref.Namespace != "team-b" {
		t.Fatalf("events = %+v", got)
	}
}
