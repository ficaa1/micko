package argo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// actionRef is the workflow every action test acts on.
var actionRef = core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}

// Each action is one request with the method, path and body Argo defines,
// without the UID, and its answer names the affected workflow.
func TestActionRequests(t *testing.T) {
	named := `{"name":"wf","namespace":"ns"}`
	answer := func(name, uid string) string {
		return `{"metadata":{"namespace":"ns","name":"` + name + `","uid":"` + uid + `"},"status":{"phase":"Running"}}`
	}
	same := &core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	confirmed := core.Confirmation{Confirmed: true}
	cases := []struct {
		action             core.Action
		confirm            core.Confirmation
		answer             string
		method, path, body string
		affected           *core.Ref
	}{
		{core.ActionResume, confirmed, answer("wf", "uid-1"), http.MethodPut, "/api/v1/workflows/ns/wf/resume", named, same},
		{core.ActionSuspend, confirmed, answer("wf", "uid-1"), http.MethodPut, "/api/v1/workflows/ns/wf/suspend", named, same},
		{core.ActionRetry, confirmed, answer("wf", "uid-1"), http.MethodPut, "/api/v1/workflows/ns/wf/retry", named, same},
		{core.ActionResubmit, confirmed, answer("wf-abc", "new"), http.MethodPut, "/api/v1/workflows/ns/wf/resubmit", named,
			&core.Ref{Namespace: "ns", Name: "wf-abc", UID: "new"}},
		{core.ActionStop, confirmed, answer("wf", "uid-1"), http.MethodPut, "/api/v1/workflows/ns/wf/stop", named, same},
		{core.ActionTerminate, core.Confirmation{Confirmed: true, BulkSize: 2, TypedCount: "2"}, answer("wf", "uid-1"),
			http.MethodPut, "/api/v1/workflows/ns/wf/terminate", named, same},
		{core.ActionDelete, core.Confirmation{Confirmed: true, Final: true}, `{}`, http.MethodDelete, "/api/v1/workflows/ns/wf", "", nil},
	}
	for _, c := range cases {
		t.Run(string(c.action), func(t *testing.T) {
			var seen []*http.Request
			var body string
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				seen, body = append(seen, r), string(b)
				_, _ = io.WriteString(w, c.answer)
			})
			result, err := newTestClient(t, srv).Execute(context.Background(), core.ActionRequest{Action: c.action, Ref: actionRef, Confirmation: c.confirm})
			if err != nil {
				t.Fatal(err)
			}
			if len(seen) != 1 {
				t.Fatalf("requests = %d, want one", len(seen))
			}
			r := seen[0]
			if r.Method != c.method || r.URL.Path != c.path || r.URL.RawQuery != "" || body != c.body {
				t.Errorf("request = %s %s?%s %q, want %s %s %q", r.Method, r.URL.Path, r.URL.RawQuery, body, c.method, c.path, c.body)
			}
			wantType := ""
			if c.body != "" {
				wantType = "application/json"
			}
			if r.Header.Get("Content-Type") != wantType {
				t.Errorf("content type = %q, want %q", r.Header.Get("Content-Type"), wantType)
			}
			if strings.Contains(body, "uid-1") {
				t.Error("the UID was sent")
			}
			if result.Outcome != core.ActionAccepted || string(result.Response) != c.answer || !reflect.DeepEqual(result.Affected, c.affected) {
				t.Errorf("result = %s %s affected %+v, want accepted, the answer, affected %+v", result.Outcome, result.Response, result.Affected, c.affected)
			}
			if (result.Workflow != nil) != (c.affected != nil) {
				t.Errorf("workflow = %+v", result.Workflow)
			}
		})
	}
}

// A failed action is sent at most once and reports what is known of its
// outcome.
func TestActionFailures(t *testing.T) {
	status := func(code int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, body)
		}
	}
	confirmed := core.Confirmation{Confirmed: true, Final: true}
	cases := []struct {
		name     string
		action   core.Action
		confirm  core.Confirmation
		handler  http.HandlerFunc
		requests int32
		kind     core.ErrorKind
		msg      string
		outcome  core.ActionOutcome
	}{
		{"missing workflow", core.ActionDelete, confirmed, status(404, `{"code":5,"message":"not found"}`), 1, core.ErrNotFound, "not found", ""},
		{"no permission", core.ActionSuspend, confirmed, status(403, `{"code":7,"message":"permission denied"}`), 1, core.ErrForbidden, "permission denied", ""},
		{"finished workflow", core.ActionSuspend, confirmed, status(500, `{"code":13,"message":"cannot suspend completed workflows"}`), 1,
			core.ErrUnavailable, "cannot suspend", ""},
		{"connection dropped", core.ActionTerminate, core.Confirmation{Confirmed: true, TypedName: "wf"},
			func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }, 1, core.ErrUnavailable, "connection failed", core.ActionUnknown},
		{"empty answer", core.ActionStop, confirmed, status(200, ``), 1, core.ErrProtocol, "empty response", core.ActionUnknown},
		{"invalid answer", core.ActionStop, confirmed, status(200, `not a workflow`), 1, core.ErrProtocol, "invalid workflow response", core.ActionUnknown},
		{"delete not final", core.ActionDelete, core.Confirmation{Confirmed: true}, status(200, `{}`), 0, "", "", core.ActionUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests int32
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requests, 1)
				c.handler(w, r)
			})
			result, err := newTestClient(t, srv).Execute(context.Background(), core.ActionRequest{Action: c.action, Ref: actionRef, Confirmation: c.confirm})
			if got := atomic.LoadInt32(&requests); got != c.requests {
				t.Errorf("requests = %d, want %d", got, c.requests)
			}
			if result.Outcome != c.outcome {
				t.Errorf("outcome = %q, want %q", result.Outcome, c.outcome)
			}
			if c.kind == "" {
				if !errors.Is(err, core.ErrDeleteNotFinal) {
					t.Errorf("err = %v, want the confirmation refused", err)
				}
				return
			}
			wantAPIError(t, err, c.kind, c.msg)
		})
	}
}
