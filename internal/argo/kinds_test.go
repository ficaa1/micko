package argo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// Cron workflows decode in the v3.5 and v3.6+ shapes, with both schedule
// fields merged.
func TestListCronWorkflows(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cron-workflows/team-a" || r.URL.Query().Get("listOptions.limit") != "100" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"metadata": {}, "items": [{
		  "metadata": {"name": "nightly", "namespace": "team-a", "uid": "cw-1", "creationTimestamp": "2026-08-01T00:00:00Z"},
		  "spec": {
		    "schedule": "0 2 * * *", "timezone": "Europe/Berlin", "concurrencyPolicy": "Forbid",
		    "startingDeadlineSeconds": 300, "successfulJobsHistoryLimit": 3, "failedJobsHistoryLimit": 1,
		    "workflowSpec": {"entrypoint": "main", "arguments": {"parameters": [
		      {"name": "batch", "value": 500},
		      {"name": "token", "value": "s3cr3t"},
		      {"name": "region", "valueFrom": {"configMapKeyRef": {"name": "cfg", "key": "region"}}}
		    ]}}
		  },
		  "status": {
		    "active": [{"kind": "Workflow", "namespace": "team-a", "name": "nightly-1788", "uid": "wf-1"}],
		    "lastScheduledTime": "2026-09-08T00:00:00Z",
		    "conditions": [{"type": "SubmissionError", "status": "True", "message": "quota exceeded"}]
		  }
		}, {
		  "metadata": {"name": "twice", "namespace": "team-a", "uid": "cw-2"},
		  "spec": {
		    "schedules": ["0 9 * * mon-fri", "30 17 * * mon-fri"], "suspend": true,
		    "when": "{{= cronworkflow.failed < 3 }}", "stopStrategy": {"expression": "cronworkflow.failed >= 3"},
		    "workflowSpec": {"workflowTemplateRef": {"name": "report", "clusterScope": true}}
		  },
		  "status": {"phase": "Stopped", "succeeded": 12, "failed": 3}
		}, {
		  "metadata": {"name": "moving", "namespace": "team-a"},
		  "spec": {"schedule": "0 1 * * *", "schedules": ["0 2 * * *", "0 1 * * *"]}
		}]}`)
	})
	got, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d cron workflows", len(got))
	}

	v35 := got[0]
	if v35.Name != "nightly" || v35.UID != "cw-1" || strings.Join(v35.Schedules, "|") != "0 2 * * *" || v35.Timezone != "Europe/Berlin" {
		t.Errorf("v3.5 identity = %+v", v35)
	}
	if v35.ConcurrencyPolicy != "Forbid" || *v35.StartingDeadlineSeconds != 300 || *v35.SuccessfulJobsHistoryLimit != 3 || *v35.FailedJobsHistoryLimit != 1 {
		t.Errorf("v3.5 policy = %+v", v35)
	}
	if len(v35.Active) != 1 || v35.Active[0].Name != "nightly-1788" || v35.LastScheduledTime == nil ||
		len(v35.Conditions) != 1 || v35.Conditions[0].Message != "quota exceeded" {
		t.Errorf("v3.5 status = %+v", v35)
	}
	if v35.Phase != "" || v35.Succeeded != nil || v35.Failed != nil {
		t.Errorf("v3.5 reported v3.6 fields: %+v", v35)
	}
	if a := v35.Arguments; v35.Entrypoint != "main" || len(a) != 3 || a[0].Value != "500" || !a[0].HasValue ||
		a[2].HasValue || a[2].ValueFrom != "configmap cfg key region" {
		t.Errorf("v3.5 arguments = %+v", a)
	}
	if !strings.Contains(string(v35.Resource), `"s3cr3t"`) {
		t.Error("raw object not kept")
	}

	v36 := got[1]
	if strings.Join(v36.Schedules, "|") != "0 9 * * mon-fri|30 17 * * mon-fri" || !v36.Suspend || v36.When == "" ||
		v36.StopExpression != "cronworkflow.failed >= 3" || v36.WorkflowTemplateRef != "cluster/report" {
		t.Errorf("v3.6 spec = %+v", v36)
	}
	if v36.Phase != "Stopped" || *v36.Succeeded != 12 || *v36.Failed != 3 {
		t.Errorf("v3.6 status = %+v", v36)
	}

	if s := strings.Join(got[2].Schedules, "|"); s != "0 1 * * *|0 2 * * *" {
		t.Errorf("both schedule fields = %q, want the single one first and each once", s)
	}
}

// A template decodes whole: identity, description, service account, every
// argument form, and each template's type from the field that defines it.
func TestListWorkflowTemplates(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflow-templates/ci" || r.URL.Query().Get("listOptions.limit") != "100" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"metadata": {}, "items": [{
		  "metadata": {"name": "release", "namespace": "ci", "uid": "wt-1", "creationTimestamp": "2026-07-01T00:00:00Z",
		    "annotations": {"workflows.argoproj.io/description": "Build, test and ship"}},
		  "spec": {
		    "entrypoint": "main", "serviceAccountName": "releaser",
		    "arguments": {"parameters": [
		      {"name": "target", "value": "linux/amd64", "enum": ["linux/amd64", "linux/arm64"], "description": "platform"},
		      {"name": "git-sha"},
		      {"name": "retries", "default": 3},
		      {"name": "token", "valueFrom": {"supplied": {}}}
		    ]},
		    "templates": [
		      {"name": "main", "steps": [[{"name": "build", "template": "build"}]]},
		      {"name": "build", "container": {"image": "golang:1.25"}},
		      {"name": "lint", "script": {"image": "python:3", "source": "print(1)"}},
		      {"name": "fan", "dag": {"tasks": []}},
		      {"name": "approve", "suspend": {}},
		      {"name": "apply", "resource": {"action": "create"}},
		      {"name": "fetch", "data": {"source": {}}},
		      {"name": "ping", "http": {"url": "https://example.invalid"}},
		      {"name": "slack", "plugin": {"slack": {}}},
		      {"name": "pair", "containerSet": {"containers": []}},
		      {"name": "odd"}
		    ]
		  }
		}]}`)
	})
	got, err := newTestClient(t, srv).ListWorkflowTemplates(context.Background(), "ci")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d templates", len(got))
	}
	wt := got[0]
	if wt.Name != "release" || wt.Namespace != "ci" || wt.UID != "wt-1" || wt.CreatedAt.IsZero() ||
		wt.Description != "Build, test and ship" || wt.ServiceAccount != "releaser" || wt.Entrypoint != "main" {
		t.Errorf("template = %+v", wt)
	}
	a := wt.Arguments
	if len(a) != 4 || a[0].Value != "linux/amd64" || len(a[0].Enum) != 2 || a[0].Description != "platform" ||
		a[1].HasValue || a[2].Default != "3" || a[3].ValueFrom != "supplied at run time" {
		t.Errorf("arguments = %+v", a)
	}
	var types []string
	for _, tm := range wt.Templates {
		types = append(types, tm.Name+":"+tm.Type)
	}
	want := "main:steps build:container lint:script fan:dag approve:suspend apply:resource fetch:data ping:http slack:plugin pair:containerSet odd:unknown"
	if strings.Join(types, " ") != want {
		t.Errorf("templates = %s", strings.Join(types, " "))
	}
	if !strings.Contains(string(wt.Resource), "golang:1.25") {
		t.Error("raw object not kept")
	}
}

// Cluster templates come from their own route with no namespace segment.
func TestListClusterWorkflowTemplates(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cluster-workflow-templates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"metadata":{},"items":[{"metadata":{"name":"whalesay","uid":"cwt-1"},
		  "spec":{"entrypoint":"whalesay","templates":[{"name":"whalesay","container":{"image":"docker/whalesay"}}]}}]}`)
	})
	got, err := newTestClient(t, srv).ListClusterWorkflowTemplates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Namespace != "" || got[0].Name != "whalesay" || got[0].Templates[0].Type != "container" {
		t.Errorf("got %+v", got)
	}
}

// A kind list follows the continuation token, passed back verbatim, until
// the server stops sending one.
func TestKindListsFollowTheContinuation(t *testing.T) {
	pages := []string{
		`{"metadata":{"continue":"tok/1=="},"items":[{"metadata":{"name":"a","namespace":"ns"}}]}`,
		`{"metadata":{},"items":[{"metadata":{"name":"b","namespace":"ns"}}]}`,
	}
	var sent []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, r.URL.Query().Get("listOptions.continue"))
		if len(sent) > len(pages) {
			t.Errorf("request %d after the last page", len(sent))
			return
		}
		_, _ = io.WriteString(w, pages[len(sent)-1])
	})
	got, err := newTestClient(t, srv).ListCronWorkflows(context.Background(), "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Name != "b" || strings.Join(sent, "|") != "|tok/1==" {
		t.Errorf("got %+v after sending %q", got, sent)
	}
}

// A kind list that fails comes back typed with the server's message.
func TestKindListErrors(t *testing.T) {
	cron := func(c *Client) error { _, err := c.ListCronWorkflows(context.Background(), "ns"); return err }
	templates := func(c *Client) error { _, err := c.ListWorkflowTemplates(context.Background(), "ns"); return err }
	cluster := func(c *Client) error { _, err := c.ListClusterWorkflowTemplates(context.Background()); return err }
	cases := []struct {
		name   string
		call   func(*Client) error
		status int
		body   string
		kind   core.ErrorKind
		msg    string
	}{
		{"cron forbidden", cron, 403, `{"code":7,"message":"cronworkflows.argoproj.io is forbidden: cannot list resource \"cronworkflows\""}`,
			core.ErrForbidden, "cannot list resource"},
		{"cron login page", cron, 200, `<html><form action="/login">login</form></html>`, core.ErrUnauthenticated, "HTML page"},
		{"cron unparseable", cron, 200, `{"items": "nope"}`, core.ErrProtocol, "unparseable"},
		{"cron empty", cron, 200, ``, core.ErrProtocol, "empty response body"},
		{"templates missing route", templates, 404, `{"code":5,"message":"Not Found"}`, core.ErrNotFound, "Not Found"},
		{"cluster templates forbidden", cluster, 403, `{"code":7,"message":"cannot list resource at the cluster scope"}`,
			core.ErrForbidden, "at the cluster scope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantAPIError(t, c.call(newTestClient(t, serveBody(t, c.status, c.body))), c.kind, c.msg)
		})
	}
}
