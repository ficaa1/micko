package argo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// List decodes each summary and the page metadata, passes the continuation
// token back verbatim, and runs no gate scan for an empty page.
func TestListPages(t *testing.T) {
	pages := map[string][]byte{"": loadFixture(t, "list_page1.json"), "100": loadFixture(t, "list_page2.json")}
	var scans int
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/team-a" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if isGateScan(r) {
			scans++
			serveNoGates(w)
			return
		}
		body, ok := pages[r.URL.Query().Get("listOptions.continue")]
		if !ok {
			t.Errorf("continue = %q, want a token the server sent", r.URL.Query().Get("listOptions.continue"))
		}
		_, _ = w.Write(body)
	})
	c := newTestClient(t, srv)

	first, err := c.List(context.Background(), core.Query{Namespace: "team-a", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	started := created.Add(time.Minute)
	finished := time.Date(2026, 9, 8, 9, 37, 0, 0, time.UTC)
	want := []core.Summary{
		{
			Ref: core.Ref{Namespace: "team-a", Name: "train-pipeline", UID: "uid-1"}, ResourceVersion: "11",
			Phase: "Running", Message: "running", CreatedAt: created, StartedAt: &started,
			Labels: map[string]string{"workflows.argoproj.io/phase": "Running"},
		},
		{
			Ref: core.Ref{Namespace: "team-a", Name: "nightly-report", UID: "uid-2"}, ResourceVersion: "12",
			Phase: "WaitingForDependency", CreatedAt: created.Add(-time.Hour), FinishedAt: &finished,
		},
	}
	if !reflect.DeepEqual(first.Items, want) {
		t.Errorf("items =\n%+v\nwant\n%+v", first.Items, want)
	}
	if first.Continue != "100" || first.ResourceVersion != "rv-live-1" {
		t.Errorf("continue %q, resource version %q", first.Continue, first.ResourceVersion)
	}

	second, err := c.List(context.Background(), core.Query{Namespace: "team-a", Continue: first.Continue, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 0 || second.Continue != "" || second.ResourceVersion != "rv-live-2" {
		t.Errorf("second page = %+v, want the empty last page", second)
	}
	if scans != 1 {
		t.Errorf("gate scans = %d, want one for the page with items", scans)
	}
}

// A running Suspend node or spec.suspend marks a workflow suspended; the
// gate scan stays within the caller's selector, and the list asks for no
// node maps.
func TestListMarksSuspendedWorkflows(t *testing.T) {
	list := `{"metadata":{},"items":[
		{"metadata":{"name":"gate","namespace":"ns","uid":"uid-gate"},"status":{"phase":"Running"}},
		{"metadata":{"name":"passed-gate","namespace":"ns","uid":"uid-passed"},"status":{"phase":"Running"}},
		{"metadata":{"name":"spec","namespace":"ns","uid":"uid-spec"},"spec":{"suspend":true},"status":{"phase":"Running"}},
		{"metadata":{"name":"plain","namespace":"ns","uid":"uid-plain"},"status":{"phase":"Running"}}]}`
	gates := `{"metadata":{},"items":[
		{"metadata":{"uid":"uid-gate"},"status":{"nodes":{"n1":{"type":"Suspend","phase":"Running"}}}},
		{"metadata":{"uid":"uid-passed"},"status":{"nodes":{"n1":{"type":"Pod","phase":"Running"},"n2":{"type":"Suspend","phase":"Succeeded"}}}}]}`
	want := map[string]bool{"gate": true, "passed-gate": false, "spec": true, "plain": false}
	summaryFields := []string{
		"metadata.continue", "metadata.resourceVersion",
		"items.metadata.name", "items.metadata.namespace", "items.metadata.uid", "items.metadata.creationTimestamp",
		"items.spec.suspend", "items.status.phase", "items.status.startedAt", "items.status.finishedAt",
		"items.status.progress", "items.status.estimatedDuration",
	}
	cases := []struct {
		selector, scanSelector string
	}{
		{"", "workflows.argoproj.io/completed!=true"},
		{"app=users", "app=users,workflows.argoproj.io/completed!=true"},
	}
	for _, c := range cases {
		t.Run("selector "+c.selector, func(t *testing.T) {
			var listFieldsSent, scanSelector, scanLimit string
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if isGateScan(r) {
					scanSelector = r.URL.Query().Get("listOptions.labelSelector")
					scanLimit = r.URL.Query().Get("listOptions.limit")
					_, _ = io.WriteString(w, gates)
					return
				}
				listFieldsSent = r.URL.Query().Get("fields")
				_, _ = io.WriteString(w, list)
			})
			page, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "ns", LabelSelector: c.selector})
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range page.Items {
				if it.Suspended != want[it.Ref.Name] {
					t.Errorf("%s suspended = %v, want %v", it.Ref.Name, it.Suspended, want[it.Ref.Name])
				}
			}
			if scanSelector != c.scanSelector || scanLimit != "500" {
				t.Errorf("gate scan selector %q limit %q, want %q and 500", scanSelector, scanLimit, c.scanSelector)
			}
			sent := strings.Split(listFieldsSent, ",")
			for _, f := range summaryFields {
				if !contains(sent, f) {
					t.Errorf("list projection drops %s", f)
				}
			}
			if contains(sent, "items.status.nodes") {
				t.Error("list projection asks for node maps")
			}
		})
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// A failed gate scan fails the list rather than showing gated workflows as
// plain Running rows.
func TestAFailedGateScanFailsTheList(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"code":7,"message":"scan denied"}`)
			return
		}
		_, _ = io.WriteString(w, `{"metadata":{},"items":[{"metadata":{"name":"wf","namespace":"ns","uid":"u"}}]}`)
	})
	_, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "ns"})
	wantAPIError(t, err, core.ErrForbidden, "scan denied")
}

// An empty namespace lists every namespace through the path with its
// trailing slash, unless the server manages one namespace.
func TestEveryNamespaceLists(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		scoped bool
		call   func(*Client) error
	}{
		{"workflows", "/api/v1/workflows/", true, func(c *Client) error {
			_, err := c.List(context.Background(), core.Query{})
			return err
		}},
		{"workflow watch", "/api/v1/workflow-events/", false, func(c *Client) error {
			err := c.Watch(context.Background(), core.WatchRequest{}, func(core.WatchEvent) error { return nil })
			if we := (*core.WatchError)(nil); errors.As(err, &we) && we.Kind == core.WatchEnded {
				return nil
			}
			return err
		}},
		{"cron workflows", "/api/v1/cron-workflows/", true, func(c *Client) error {
			_, err := c.ListCronWorkflows(context.Background(), "")
			return err
		}},
		{"workflow templates", "/api/v1/workflow-templates/", true, func(c *Client) error {
			_, err := c.ListWorkflowTemplates(context.Background(), "")
			return err
		}},
		{"archive", "/api/v1/archived-workflows", true, func(c *Client) error {
			_, err := c.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Limit: 100})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/info":
					_, _ = io.WriteString(w, `{}`)
				case r.URL.Path != c.path:
					t.Errorf("path = %q, want %q", r.URL.Path, c.path)
				case r.URL.Query().Has("listOptions.fieldSelector"):
					t.Errorf("query %q narrows the list to a namespace", r.URL.RawQuery)
				case isGateScan(r):
					serveNoGates(w)
				case strings.HasPrefix(r.URL.Path, "/api/v1/workflow-events"):
				default:
					_, _ = io.WriteString(w, `{"metadata":{},"items":[]}`)
				}
			})
			if err := c.call(newTestClient(t, srv)); err != nil {
				t.Fatal(err)
			}
			if !c.scoped {
				return
			}
			managed := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/info" {
					t.Errorf("%s was sent to a server that manages one namespace", r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"managedNamespace":"argo"}`)
			})
			wantAPIError(t, c.call(newTestClient(t, managed)), core.ErrUnsupported, "manages namespace argo only")
		})
	}
}

// The server's scope is asked once per client for cluster-wide lists and
// never for one namespace; a failed answer is asked again. A scope warmed at
// connect answers the lists that follow.
func TestServerScopeIsAskedOnce(t *testing.T) {
	cases := []struct {
		name       string
		namespace  string
		infoStatus int
		warm       bool
		want       int
	}{
		{"cluster-wide", "", http.StatusOK, false, 1},
		{"failed lookup", "", http.StatusNotFound, false, 2},
		{"one namespace", "team-a", http.StatusOK, false, 0},
		{"warmed", "", http.StatusOK, true, 0},
		{"warmed, failed", "", http.StatusNotFound, true, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var asked int
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/info":
					asked++
					w.WriteHeader(c.infoStatus)
					_, _ = io.WriteString(w, `{}`)
				case isGateScan(r):
					serveNoGates(w)
				default:
					_, _ = io.WriteString(w, `{"metadata":{},"items":[{"metadata":{"name":"wf","namespace":"team-a","uid":"u"}}]}`)
				}
			})
			cl := newTestClient(t, srv)
			if c.warm {
				cl.WarmScope(context.Background())
				asked = 0
			}
			for i := 0; i < 2; i++ {
				if _, err := cl.List(context.Background(), core.Query{Namespace: c.namespace}); err != nil {
					t.Fatal(err)
				}
			}
			if asked != c.want {
				t.Errorf("the lists asked the scope %d times, want %d", asked, c.want)
			}
		})
	}
}

// Namespaces are the server's managed one, or else the sorted namespaces of
// a cluster-wide list.
func TestListNamespaces(t *testing.T) {
	cases := []struct {
		name       string
		info       string
		listStatus int
		want       []string
		note       string
		listed     bool
		wantErr    bool
	}{
		{"managed", `{"managedNamespace":"argo-only"}`, http.StatusOK, []string{"argo-only"}, "manages this namespace only", false, false},
		{"derived", `{}`, http.StatusOK, []string{"a-ns", "b-ns"}, "this token can see", true, false},
		{"refused", `{}`, http.StatusForbidden, nil, "", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var listed bool
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/info" {
					_, _ = io.WriteString(w, c.info)
					return
				}
				listed = true
				if r.URL.Path != "/api/v1/workflows/" || r.URL.Query().Get("fields") != "items.metadata.namespace" {
					t.Errorf("list = %s, want the cluster-wide list of namespaces alone", r.URL)
				}
				w.WriteHeader(c.listStatus)
				_, _ = io.WriteString(w, `{"items":[{"metadata":{"namespace":"b-ns"}},{"metadata":{"namespace":"a-ns"}},{"metadata":{"namespace":"b-ns"}}]}`)
			})
			names, note, err := newTestClient(t, srv).ListNamespaces(context.Background())
			if (err != nil) != c.wantErr || listed != c.listed {
				t.Fatalf("err = %v, listed = %v", err, listed)
			}
			if !reflect.DeepEqual(names, c.want) || !strings.Contains(note, c.note) {
				t.Errorf("names %v note %q, want %v and %q", names, note, c.want, c.note)
			}
		})
	}
}
