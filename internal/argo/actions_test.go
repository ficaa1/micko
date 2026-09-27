package argo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// recordedRequest is what a test server saw of one action request.
type recordedRequest struct {
	method, path, query, body, contentType string
}

// actionServer answers every request with status and body, and records
// each request it sees.
func actionServer(t *testing.T, status int, body string) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Get("Content-Type")})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// suspendedWorkflow is the server's answer to a suspend: the workflow with
// spec.suspend set and its phase still Running, as Argo's SuspendWorkflow
// returns it.
const suspendedWorkflow = `{"metadata":{"namespace":"ns","name":"wf","uid":"uid-1"},"spec":{"suspend":true,"entrypoint":"main"},"status":{"phase":"Running"}}`

// Suspend is PUT .../{name}/suspend with a body naming the workflow, and its
// answer is the workflow, which now reports itself suspended.
func TestSuspendIsOnePUTWithTheSchemaBody(t *testing.T) {
	srv, seen := actionServer(t, http.StatusOK, suspendedWorkflow)
	c := newTestClient(t, srv)
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	result, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionSuspend, Ref: ref, Confirmation: core.Confirmation{Confirmed: true}})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %d, want exactly one", len(*seen))
	}
	got := (*seen)[0]
	if got.method != http.MethodPut || got.path != "/api/v1/workflows/ns/wf/suspend" || got.body != `{"name":"wf","namespace":"ns"}` {
		t.Fatalf("request = %+v", got)
	}
	if got.contentType != "application/json" {
		t.Fatalf("content type = %q", got.contentType)
	}
	if strings.Contains(got.body, "uid-1") || strings.Contains(got.query, "uid-1") {
		t.Fatal("the UID was sent")
	}
	if result.Outcome != core.ActionAccepted || result.Workflow == nil || !result.Workflow.Summary.Suspended {
		t.Fatalf("result = %+v", result)
	}
}

// Delete is DELETE on the workflow itself, with no body, and its answer is
// an empty object. A 2xx is the whole answer: there is no workflow left to
// name.
func TestDeleteIsOneDELETEWithNoBody(t *testing.T) {
	srv, seen := actionServer(t, http.StatusOK, `{}`)
	c := newTestClient(t, srv)
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	result, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionDelete, Ref: ref, Confirmation: core.Confirmation{Confirmed: true, Final: true}})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %d, want exactly one", len(*seen))
	}
	got := (*seen)[0]
	if got.method != http.MethodDelete || got.path != "/api/v1/workflows/ns/wf" || got.body != "" || got.query != "" {
		t.Fatalf("request = %+v", got)
	}
	if got.contentType != "" {
		t.Fatalf("a bodiless DELETE declared a content type: %q", got.contentType)
	}
	if result.Outcome != core.ActionAccepted || result.Affected != nil || result.Workflow != nil {
		t.Fatalf("result = %+v", result)
	}
}

// A delete that skipped its final confirmation never reaches the server.
func TestDeleteWithoutTheFinalStepSendsNothing(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionDelete, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Confirmation: core.Confirmation{Confirmed: true}})
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d, want a refusal with nothing sent", err, calls)
	}
}

// Terminate is PUT .../{name}/terminate. A bulk terminate carries the typed
// count instead of a typed name, and the transport accepts it.
func TestTerminateFromABulkConfirmation(t *testing.T) {
	srv, seen := actionServer(t, http.StatusOK, `{"metadata":{"namespace":"ns","name":"wf","uid":"uid-1"},"spec":{"shutdown":"Terminate"},"status":{"phase":"Running"}}`)
	c := newTestClient(t, srv)
	req := core.ActionRequest{Action: core.ActionTerminate, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"},
		Confirmation: core.Confirmation{Confirmed: true, BulkSize: 2, TypedCount: "2"}}
	if _, err := c.Execute(context.Background(), req); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	got := (*seen)[0]
	if got.method != http.MethodPut || got.path != "/api/v1/workflows/ns/wf/terminate" || got.body != `{"name":"wf","namespace":"ns"}` {
		t.Fatalf("request = %+v", got)
	}
}

// A non-2xx answer maps to the typed error of its status, carries no
// outcome, and is never retried.
func TestActionErrorsMapByStatus(t *testing.T) {
	cases := []struct {
		name   string
		action core.Action
		status int
		body   string
		kind   core.ErrorKind
	}{
		{"delete of a missing workflow", core.ActionDelete, http.StatusNotFound, `{"code":5,"message":"workflows.argoproj.io \"wf\" not found"}`, core.ErrNotFound},
		{"suspend without permission", core.ActionSuspend, http.StatusForbidden, `{"code":7,"message":"permission denied"}`, core.ErrForbidden},
		{"suspend of a completed workflow", core.ActionSuspend, http.StatusInternalServerError, `{"code":13,"message":"cannot suspend completed workflows"}`, core.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := actionServer(t, tc.status, tc.body)
			c := newTestClient(t, srv)
			result, err := c.Execute(context.Background(), core.ActionRequest{Action: tc.action, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"},
				Confirmation: core.Confirmation{Confirmed: true, Final: true}})
			apiErr := core.AsAPIError(err)
			if apiErr == nil || apiErr.Kind != tc.kind || apiErr.Status != tc.status {
				t.Fatalf("err = %v, want kind %s status %d", err, tc.kind, tc.status)
			}
			if result.Outcome != "" {
				t.Fatalf("outcome = %q, want none from the transport", result.Outcome)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %d, want exactly one", len(*seen))
			}
		})
	}
}

// spec.suspend is what a suspended workflow reports: its phase stays
// Running. A finished workflow keeps the flag after a stop, but nothing
// waits on it any more.
func TestSpecSuspendMarksAnUnfinishedWorkflowSuspended(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{suspendedWorkflow, true},
		{`{"metadata":{"namespace":"ns","name":"wf","uid":"uid-1"},"spec":{"suspend":true},"status":{"phase":"Failed"}}`, false},
		{`{"metadata":{"namespace":"ns","name":"wf","uid":"uid-1"},"spec":{"suspend":false},"status":{"phase":"Running"}}`, false},
		{`{"metadata":{"namespace":"ns","name":"wf","uid":"uid-1"},"status":{"phase":"Running"}}`, false},
	}
	for _, tc := range cases {
		wf, err := decodeWorkflowDetail([]byte(tc.body))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if wf.Summary.Suspended != tc.want {
			t.Errorf("%s: suspended = %v, want %v", tc.body, wf.Summary.Suspended, tc.want)
		}
	}
}

// The list carries spec.suspend itself, and the gate scan, which knows only
// about Suspend nodes, must not erase it.
func TestListKeepsSpecSuspendThroughTheGateScan(t *testing.T) {
	list := `{"metadata":{"resourceVersion":"7"},"items":[
		{"metadata":{"name":"wf-a","namespace":"team-a","uid":"uid-a"},"spec":{"suspend":true},"status":{"phase":"Running"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateScan(r) {
			serveNoGates(w)
			return
		}
		if !strings.Contains(r.URL.Query().Get("fields"), "items.spec.suspend") {
			t.Error("the list request did not ask for spec.suspend")
		}
		_, _ = w.Write([]byte(list))
	}))
	defer srv.Close()
	page, err := newTestClient(t, srv).List(context.Background(), core.Query{Namespace: "team-a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || !page.Items[0].Suspended {
		t.Fatalf("items = %+v, want wf-a suspended", page.Items)
	}
}
