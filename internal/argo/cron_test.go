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

// cronV35Body is a v3.5 cron workflow list: one spec.schedule, the status
// without the v3.6 counters, an argument whose value was stored as a number.
const cronV35Body = `{
  "metadata": {"resourceVersion": "5001"},
  "items": [{
    "metadata": {"name": "nightly", "namespace": "team-a", "uid": "cw-uid-1",
      "creationTimestamp": "2026-08-01T00:00:00Z",
      "labels": {"team": "data"}},
    "spec": {
      "schedule": "0 2 * * *",
      "timezone": "Europe/Berlin",
      "concurrencyPolicy": "Forbid",
      "startingDeadlineSeconds": 300,
      "successfulJobsHistoryLimit": 3,
      "failedJobsHistoryLimit": 1,
      "workflowSpec": {
        "entrypoint": "main",
        "arguments": {"parameters": [
          {"name": "batch", "value": 500},
          {"name": "token", "value": "s3cr3t"},
          {"name": "region", "valueFrom": {"configMapKeyRef": {"name": "cfg", "key": "region"}}}
        ]}
      }
    },
    "status": {
      "active": [{"kind": "Workflow", "namespace": "team-a", "name": "nightly-1788", "uid": "wf-1"}],
      "lastScheduledTime": "2026-09-08T00:00:00Z",
      "conditions": [{"type": "SubmissionError", "status": "True", "message": "quota exceeded"}]
    }
  }]
}`

// cronV36Body is a v3.6+/v4 object: the schedules list, a stop strategy, a
// when expression, the status phase and counters, and a template reference.
const cronV36Body = `{
  "metadata": {},
  "items": [{
    "metadata": {"name": "twice", "namespace": "team-b", "uid": "cw-uid-2"},
    "spec": {
      "schedules": ["0 9 * * mon-fri", "30 17 * * mon-fri"],
      "suspend": true,
      "when": "{{= cronworkflow.failed < 3 }}",
      "stopStrategy": {"expression": "cronworkflow.failed >= 3"},
      "workflowSpec": {"workflowTemplateRef": {"name": "report", "clusterScope": true}}
    },
    "status": {"phase": "Stopped", "succeeded": 12, "failed": 3}
  }]
}`

func serveCron(t *testing.T, check func(r *http.Request), bodies ...string) *httptest.Server {
	t.Helper()
	i := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			io.WriteString(w, `{}`)
			return
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		if i >= len(bodies) {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, bodies[i])
		i++
	}))
}

// The v3.5 shape decodes: the single schedule, the policy fields, the active
// runs, the conditions, argument values of any JSON type as text, and the
// raw object kept for the raw view.
func TestListCronWorkflowsV35(t *testing.T) {
	srv := serveCron(t, func(r *http.Request) {
		if r.URL.Path != "/api/v1/cron-workflows/team-a" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("listOptions.limit"); got != "100" {
			t.Errorf("limit = %q, want 100", got)
		}
	}, cronV35Body)
	defer srv.Close()
	got, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	cw := got[0]
	if cw.Name != "nightly" || cw.Namespace != "team-a" || cw.UID != "cw-uid-1" {
		t.Fatalf("identity = %+v", cw)
	}
	if len(cw.Schedules) != 1 || cw.Schedules[0] != "0 2 * * *" || cw.Timezone != "Europe/Berlin" {
		t.Fatalf("schedule = %v tz %q", cw.Schedules, cw.Timezone)
	}
	if cw.ConcurrencyPolicy != "Forbid" || *cw.StartingDeadlineSeconds != 300 ||
		*cw.SuccessfulJobsHistoryLimit != 3 || *cw.FailedJobsHistoryLimit != 1 {
		t.Fatalf("policy = %+v", cw)
	}
	if len(cw.Active) != 1 || cw.Active[0].Name != "nightly-1788" || cw.LastScheduledTime == nil {
		t.Fatalf("status = %+v", cw)
	}
	if cw.Phase != "" || cw.Succeeded != nil || cw.Failed != nil {
		t.Fatalf("v3.5 status reported v3.6 fields: %+v", cw)
	}
	if len(cw.Conditions) != 1 || cw.Conditions[0].Message != "quota exceeded" {
		t.Fatalf("conditions = %+v", cw.Conditions)
	}
	if cw.Entrypoint != "main" || len(cw.Arguments) != 3 {
		t.Fatalf("spec = %+v", cw)
	}
	if a := cw.Arguments[0]; a.Value != "500" || !a.HasValue {
		t.Fatalf("numeric value = %+v", a)
	}
	if a := cw.Arguments[2]; a.HasValue || a.ValueFrom != "configmap cfg key region" {
		t.Fatalf("valueFrom = %+v", a)
	}
	if !strings.Contains(string(cw.Resource), `"s3cr3t"`) {
		t.Fatal("raw object not kept")
	}
}

// The v3.6+ shape decodes: the schedules list, suspend, when, the stop
// strategy, a cluster template reference, and the phase and counters.
func TestListCronWorkflowsV36(t *testing.T) {
	srv := serveCron(t, nil, cronV36Body)
	defer srv.Close()
	got, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "team-b")
	if err != nil {
		t.Fatal(err)
	}
	cw := got[0]
	if len(cw.Schedules) != 2 || cw.Schedules[1] != "30 17 * * mon-fri" || !cw.Suspend {
		t.Fatalf("schedules = %v suspend %v", cw.Schedules, cw.Suspend)
	}
	if cw.When == "" || cw.StopExpression != "cronworkflow.failed >= 3" {
		t.Fatalf("when/stop = %q %q", cw.When, cw.StopExpression)
	}
	if cw.WorkflowTemplateRef != "cluster/report" {
		t.Fatalf("template ref = %q", cw.WorkflowTemplateRef)
	}
	if cw.Phase != "Stopped" || *cw.Succeeded != 12 || *cw.Failed != 3 {
		t.Fatalf("status = %+v", cw)
	}
}

// Both schedule fields on one object merge, the single one first, a
// duplicate once.
func TestMergeSchedules(t *testing.T) {
	got := mergeSchedules("0 1 * * *", []string{"0 2 * * *", "0 1 * * *"})
	if strings.Join(got, "|") != "0 1 * * *|0 2 * * *" {
		t.Fatalf("merged = %v", got)
	}
	if got := mergeSchedules("", nil); got != nil {
		t.Fatalf("empty = %v", got)
	}
}

// A continuation token is followed, passed back verbatim, until the server
// stops sending one.
func TestListCronWorkflowsFollowsContinue(t *testing.T) {
	page1 := `{"metadata":{"continue":"tok/1=="},"items":[{"metadata":{"name":"a","namespace":"ns"},"spec":{"schedule":"@hourly"}}]}`
	page2 := `{"metadata":{},"items":[{"metadata":{"name":"b","namespace":"ns"},"spec":{"schedule":"@daily"}}]}`
	var conts []string
	srv := serveCron(t, func(r *http.Request) {
		conts = append(conts, r.URL.Query().Get("listOptions.continue"))
	}, page1, page2)
	defer srv.Close()
	got, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Name != "b" {
		t.Fatalf("got %+v", got)
	}
	if len(conts) != 2 || conts[0] != "" || conts[1] != "tok/1==" {
		t.Fatalf("continue tokens sent = %q", conts)
	}
}

// Every namespace: the path keeps its trailing slash, and a server managing
// one namespace is refused before the list is asked for.
func TestListCronWorkflowsAllNamespaces(t *testing.T) {
	srv := serveCron(t, func(r *http.Request) {
		if r.URL.Path != "/api/v1/cron-workflows/" {
			t.Errorf("path = %q, want the trailing slash", r.URL.Path)
		}
	}, cronV36Body)
	defer srv.Close()
	if _, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), ""); err != nil {
		t.Fatal(err)
	}

	managed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/info" {
			t.Errorf("a list was sent to a managed-namespace server: %s", r.URL.Path)
		}
		io.WriteString(w, `{"managedNamespace":"argo"}`)
	}))
	defer managed.Close()
	_, err := newTestClient(t, managed).ListCronWorkflows(context.Background(), "")
	if ae := core.AsAPIError(err); ae == nil || !strings.Contains(ae.Message, "manages namespace argo only") {
		t.Fatalf("err = %v", err)
	}
}

// A refusal and a missing route come back typed with the server's message:
// 403 as forbidden, 404 as not found.
func TestListCronWorkflowsErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   core.ErrorKind
		msg    string
	}{
		{403, `{"code":7,"message":"cronworkflows.argoproj.io is forbidden: User \"system:serviceaccount:argo:viewer\" cannot list resource \"cronworkflows\""}`,
			core.ErrForbidden, "cannot list resource"},
		{404, `{"code":5,"message":"Not Found"}`, core.ErrNotFound, "Not Found"},
		{200, `<html><form action="/login">login</form></html>`, core.ErrUnauthenticated, "HTML page"},
		{200, `{"items": "nope"}`, core.ErrProtocol, "unparseable"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		}))
		_, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "ns")
		srv.Close()
		ae := core.AsAPIError(err)
		if ae == nil || ae.Kind != c.kind || !strings.Contains(ae.Message, c.msg) {
			t.Errorf("HTTP %d: err = %v, want %s containing %q", c.status, err, c.kind, c.msg)
		}
	}
}
