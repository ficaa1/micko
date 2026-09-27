package argo

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// templateListBody is a WorkflowTemplate list as Argo returns it: the
// description annotation, arguments with a value, a default, an enum and a
// valueFrom, and one template of each defining field the view names.
const templateListBody = `{
  "metadata": {"resourceVersion": "900"},
  "items": [{
    "metadata": {"name": "release", "namespace": "ci", "uid": "wt-1",
      "creationTimestamp": "2026-07-01T00:00:00Z",
      "labels": {"team": "platform"},
      "annotations": {"workflows.argoproj.io/description": "Build, test and ship"}},
    "spec": {
      "entrypoint": "main",
      "serviceAccountName": "releaser",
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
  }]
}`

// A template decodes whole: identity, description, service account, every
// argument form, and each template's type from the field that defines it.
func TestListWorkflowTemplates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflow-templates/ci" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("listOptions.limit") != "100" {
			t.Errorf("limit = %q", r.URL.Query().Get("listOptions.limit"))
		}
		io.WriteString(w, templateListBody)
	}))
	defer srv.Close()
	got, err := newTestClient(t, srv).ListWorkflowTemplates(context.Background(), "ci")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	wt := got[0]
	if wt.Name != "release" || wt.Namespace != "ci" || wt.UID != "wt-1" || wt.CreatedAt.IsZero() {
		t.Fatalf("identity = %+v", wt)
	}
	if wt.Description != "Build, test and ship" || wt.ServiceAccount != "releaser" || wt.Entrypoint != "main" {
		t.Fatalf("spec = %+v", wt)
	}
	args := wt.Arguments
	if len(args) != 4 || args[0].Value != "linux/amd64" || len(args[0].Enum) != 2 || args[0].Description != "platform" {
		t.Fatalf("argument 0 = %+v", args[0])
	}
	if args[1].HasValue || args[2].Default != "3" || args[3].ValueFrom != "supplied at run time" {
		t.Fatalf("arguments = %+v", args)
	}
	var types []string
	for _, tm := range wt.Templates {
		types = append(types, tm.Name+":"+tm.Type)
	}
	want := "main:steps build:container lint:script fan:dag approve:suspend apply:resource fetch:data ping:http slack:plugin pair:containerSet odd:unknown"
	if strings.Join(types, " ") != want {
		t.Fatalf("templates = %s", strings.Join(types, " "))
	}
	if !strings.Contains(string(wt.Resource), "golang:1.25") {
		t.Fatal("raw object not kept")
	}
}

// Every namespace keeps the trailing slash; a managed-namespace server is
// refused before the list is asked for.
func TestListWorkflowTemplatesAllNamespaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/info":
			io.WriteString(w, `{}`)
		case "/api/v1/workflow-templates/":
			io.WriteString(w, templateListBody)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	if _, err := newTestClient(t, srv).ListWorkflowTemplates(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	managed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/info" {
			t.Errorf("a list was sent to a managed-namespace server")
		}
		io.WriteString(w, `{"managedNamespace":"argo"}`)
	}))
	defer managed.Close()
	if _, err := newTestClient(t, managed).ListWorkflowTemplates(context.Background(), ""); err == nil {
		t.Fatal("managed-namespace server not refused")
	}
}

// Cluster templates come from their own route, with no namespace segment,
// and carry no namespace.
func TestListClusterWorkflowTemplates(t *testing.T) {
	body := `{"metadata":{},"items":[{"metadata":{"name":"whalesay","uid":"cwt-1"},
	  "spec":{"entrypoint":"whalesay","templates":[{"name":"whalesay","container":{"image":"docker/whalesay"}}]}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cluster-workflow-templates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()
	got, err := newTestClient(t, srv).ListClusterWorkflowTemplates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Namespace != "" || got[0].Name != "whalesay" || got[0].Templates[0].Type != "container" {
		t.Fatalf("got %+v", got)
	}
}

// A refusal and a missing route come back typed with the server's message.
func TestListTemplatesErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   core.ErrorKind
		msg    string
	}{
		{403, `{"code":7,"message":"clusterworkflowtemplates.argoproj.io is forbidden: User \"system:serviceaccount:argo:viewer\" cannot list resource \"clusterworkflowtemplates\" in API group \"argoproj.io\" at the cluster scope"}`,
			core.ErrForbidden, "at the cluster scope"},
		{404, `{"code":5,"message":"Not Found"}`, core.ErrNotFound, "Not Found"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		}))
		_, err := newTestClient(t, srv).ListClusterWorkflowTemplates(context.Background())
		ae := core.AsAPIError(err)
		if ae == nil || ae.Kind != c.kind || !strings.Contains(ae.Message, c.msg) {
			t.Errorf("cluster HTTP %d: err = %v", c.status, err)
		}
		_, err = newTestClient(t, srv).ListWorkflowTemplates(context.Background(), "ns")
		ae = core.AsAPIError(err)
		if ae == nil || ae.Kind != c.kind {
			t.Errorf("namespaced HTTP %d: err = %v", c.status, err)
		}
		srv.Close()
	}
}
