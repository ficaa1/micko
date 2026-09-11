package argo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server started for one namespace answers the question by itself, and no
// workflow list is worth requesting after that.
func TestManagedNamespaceIsTheWholeAnswer(t *testing.T) {
	var listed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/info":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"managedNamespace":"argo-only"}`))
		default:
			listed = true
			w.Write([]byte(`{"items":[]}`))
		}
	}))
	defer srv.Close()

	names, note, err := newTestClient(t, srv).ListNamespaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "argo-only" {
		t.Fatalf("names = %v, want the managed namespace alone", names)
	}
	if !strings.Contains(note, "manages this namespace only") {
		t.Fatalf("note = %q", note)
	}
	if listed {
		t.Fatal("the workflow list was requested even though the server named its namespace")
	}
}

// Argo has no endpoint that lists namespaces, so they are derived from the
// workflows this token can see, cluster-wide and projected to one field.
func TestNamespacesAreDerivedFromTheWorkflowList(t *testing.T) {
	var gotPath, gotFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
			return
		}
		gotPath = r.URL.Path
		gotFields = r.URL.Query().Get("fields")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[
			{"metadata":{"namespace":"b-ns"}},
			{"metadata":{"namespace":"a-ns"}},
			{"metadata":{"namespace":"b-ns"}}
		]}`))
	}))
	defer srv.Close()

	names, note, err := newTestClient(t, srv).ListNamespaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/workflows/" {
		t.Fatalf("path = %q, want the cluster-wide list", gotPath)
	}
	if gotFields != "items.metadata.namespace" {
		t.Fatalf("fields = %q: the whole node map of every workflow is not worth downloading for one field", gotFields)
	}
	if len(names) != 2 || names[0] != "a-ns" || names[1] != "b-ns" {
		t.Fatalf("names = %v, want sorted and de-duplicated", names)
	}
	if !strings.Contains(note, "this token can see") {
		t.Fatalf("note = %q: a derived list must say it is derived", note)
	}
}

// A token that may not list cluster-wide gets a typed error, so the picker can
// say why rather than showing an empty cluster.
func TestDeniedListIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":7,"message":"forbidden"}`))
	}))
	defer srv.Close()

	if _, _, err := newTestClient(t, srv).ListNamespaces(context.Background()); err == nil {
		t.Fatal("a denied list was reported as success")
	}
}
