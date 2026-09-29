package argo

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// getWorkflow is Get's answer when the server returns body for a workflow
// selected without a UID.
func getWorkflow(t *testing.T, body string) core.Workflow {
	t.Helper()
	wf, err := newTestClient(t, serveBody(t, http.StatusOK, body)).Get(context.Background(), core.Ref{Namespace: "ns", Name: "wf"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return wf
}

// Get decodes the summary and every node, keeps a child reference to a node
// missing from the map, and keeps the raw object with its unknown fields.
func TestGetDecodesTheWorkflow(t *testing.T) {
	wf := getWorkflow(t, string(loadFixture(t, "workflow_detail.json")))
	if wf.Summary.Ref.UID != "wf-uid-111" || wf.Summary.Phase != "Running" || wf.Summary.Message != "progress 1/2" || wf.Summary.StartedAt == nil {
		t.Errorf("summary = %+v", wf.Summary)
	}
	if !wf.NodesAvailable || len(wf.Nodes) != 4 {
		t.Fatalf("nodes = %d available %v", len(wf.Nodes), wf.NodesAvailable)
	}
	root := wf.Nodes["node-root"]
	if root.Type != "DAG" || strings.Join(root.Children, ",") != "node-echo" || strings.Join(root.OutboundNodes, ",") != "node-echo" {
		t.Errorf("root = %+v", root)
	}
	if echo := wf.Nodes["node-echo"]; echo.BoundaryID != "node-root" || echo.StartedAt == nil || echo.FinishedAt == nil {
		t.Errorf("echo = %+v", echo)
	}
	if s := wf.Nodes["node-suspend"]; s.StartedAt != nil || s.FinishedAt != nil {
		t.Errorf("a node that has not started has times %v %v", s.StartedAt, s.FinishedAt)
	}
	if gap := wf.Nodes["node-ghost-parent"]; strings.Join(gap.Children, ",") != "node-missing-id" {
		t.Errorf("gap children = %v", gap.Children)
	}
	if !strings.Contains(string(wf.Resource), `"x-custom-extension": "preserve-me-42"`) {
		t.Error("the raw object lost an unknown field")
	}
}

// A workflow whose node status the server did not hydrate says so instead
// of looking like a workflow with no nodes.
func TestGetMarksNodesUnavailable(t *testing.T) {
	cases := []struct {
		name, body, reason string
	}{
		{"offloaded", string(loadFixture(t, "workflow_offloaded.json")), "offloaded"},
		{"compressed", `{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"status":{"phase":"Succeeded","compressedNodes":"H4sI"}}`, "compressed"},
	}
	for _, c := range cases {
		wf := getWorkflow(t, c.body)
		if wf.NodesAvailable || len(wf.Nodes) != 0 || !strings.Contains(wf.NodesUnavailableReason, c.reason) {
			t.Errorf("%s: available %v, %d nodes, reason %q", c.name, wf.NodesAvailable, len(wf.Nodes), wf.NodesUnavailableReason)
		}
	}
}

// A response for another workflow of the same name is refused as a
// conflict naming both UIDs, never shown in place of the selected one.
func TestGetRefusesAReplacedWorkflow(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("uid"); got != "uid-selected" {
			t.Errorf("uid = %q, want the selected UID", got)
		}
		_, _ = w.Write(loadFixture(t, "workflow_detail.json"))
	})
	_, err := newTestClient(t, srv).Get(context.Background(), core.Ref{Namespace: "team-a", Name: "dag-complex", UID: "uid-selected"})
	ae := wantAPIError(t, err, core.ErrConflict, "wf-uid-111")
	if !strings.Contains(ae.Message, "uid-selected") {
		t.Errorf("message = %q, want both UIDs", ae.Message)
	}
}

// A workflow is suspended while a Suspend node runs or spec.suspend holds
// an unfinished workflow.
func TestGetMarksASuspendedWorkflow(t *testing.T) {
	cases := []struct {
		name, spec, phase, nodes string
		want                     bool
	}{
		{"spec.suspend while running", `{"suspend":true}`, "Running", `{}`, true},
		{"spec.suspend after finishing", `{"suspend":true}`, "Failed", `{}`, false},
		{"spec.suspend false", `{"suspend":false}`, "Running", `{}`, false},
		{"running Suspend node", `{}`, "Running", `{"n":{"type":"Suspend","phase":"Running"}}`, true},
		{"finished Suspend node", `{}`, "Running", `{"n":{"type":"Suspend","phase":"Succeeded"}}`, false},
		{"no gate", `{}`, "Running", `{"n":{"type":"Pod","phase":"Running"}}`, false},
	}
	for _, c := range cases {
		body := fmt.Sprintf(`{"metadata":{"name":"wf","namespace":"ns","uid":"u"},"spec":%s,"status":{"phase":%q,"nodes":%s}}`, c.spec, c.phase, c.nodes)
		if got := getWorkflow(t, body).Summary.Suspended; got != c.want {
			t.Errorf("%s: suspended = %v, want %v", c.name, got, c.want)
		}
	}
}

// A pod-backed node's pod name follows the naming scheme the workflow
// declares, the way Argo derives it; with no known scheme there is none.
func TestGetResolvesPodNames(t *testing.T) {
	long := strings.Repeat("a", 400)
	cases := []struct {
		name, workflow, format, nodeID, node, want string
	}{
		{"v1 is the node ID", "wf", "v1", "wf-123", `{"name":"wf.step","type":"Pod","templateName":"run"}`, "wf-123"},
		{"v2 hashes the node name", "deploy", "v2", "deploy-99", `{"name":"deploy.plan-prd","type":"Pod","templateName":"terraform"}`,
			"deploy-terraform-1573283856"},
		{"v2 with retry and loop brackets", "deploy-all-regions-r5t9z", "v2", "deploy-all-regions-r5t9z-3798756459",
			`{"name":"deploy-all-regions-r5t9z[1].plan-apply-stages(0:web).plan-prd","type":"Pod","templateName":"preview"}`,
			"deploy-all-regions-r5t9z-preview-3798756459"},
		{"templateRef names the template", "wf", "v2", "wf-1", `{"name":"wf.a","type":"Pod","templateRef":{"name":"lib","template":"plan"}}`,
			"wf-plan-2962115457"},
		{"the prefix leaves room for the hash", long, "v2", "id", `{"name":"node","type":"Pod","templateName":"tpl"}`,
			long[:242] + "-2982235661"},
		{"the root node is the workflow pod", "deploy", "v2", "deploy", `{"name":"deploy","type":"Pod","templateName":"main"}`, "deploy"},
		{"no declared scheme", "wf", "", "wf-1", `{"name":"wf.a","type":"Pod","templateName":"t"}`, ""},
		{"unknown scheme", "wf", "v9", "wf-1", `{"name":"wf.a","type":"Pod","templateName":"t"}`, ""},
		{"a Suspend node owns no pod", "wf", "v2", "wf-1", `{"name":"wf.a","type":"Suspend","templateName":"t"}`, ""},
	}
	for _, c := range cases {
		annotations := ""
		if c.format != "" {
			annotations = `,"annotations":{"workflows.argoproj.io/pod-name-format":` + strconv.Quote(c.format) + `}`
		}
		body := fmt.Sprintf(`{"metadata":{"name":%q,"namespace":"ns","uid":"u"%s},"status":{"phase":"Running","nodes":{%q:%s}}}`,
			c.workflow, annotations, c.nodeID, c.node)
		if got := getWorkflow(t, body).Nodes[c.nodeID].PodName; got != c.want {
			t.Errorf("%s: pod name = %q, want %q", c.name, got, c.want)
		}
	}
}

// A node's run facts decode from NodeStatus, and a node that has not run has
// none.
func TestGetDecodesNodeRunFacts(t *testing.T) {
	wf := getWorkflow(t, `{
	  "metadata": {"name": "wf", "namespace": "ns", "uid": "u1"},
	  "status": {
	    "phase": "Failed", "progress": "2/3", "estimatedDuration": 95,
	    "nodes": {
	      "ran": {
	        "name": "wf.train", "type": "Pod", "phase": "Failed",
	        "progress": "0/1", "estimatedDuration": 40,
	        "resourcesDuration": {"cpu": 12, "memory": 700},
	        "hostNodeName": "worker-3",
	        "inputs": {"parameters": [{"name": "epochs", "value": "10"}, {"name": "pending"}],
	                   "artifacts": [{"name": "dataset", "s3": {"key": "a/b"}}]},
	        "outputs": {"exitCode": "137", "result": "done", "parameters": [{"name": "loss", "value": "0.3"}]},
	        "nodeFlag": {"retried": true},
	        "memoizationStatus": {"hit": true, "key": "k", "cacheName": "c"}
	      },
	      "pending": {"name": "wf", "type": "Steps", "phase": "Pending"}
	    }
	  }
	}`)
	if wf.Summary.Progress != "2/3" || wf.Summary.EstimatedDuration != 95*time.Second {
		t.Errorf("summary progress %q estimate %v", wf.Summary.Progress, wf.Summary.EstimatedDuration)
	}
	n := wf.Nodes["ran"]
	if n.Progress != "0/1" || n.EstimatedDuration != 40*time.Second || n.ResourcesDuration["cpu"] != 12 || n.ResourcesDuration["memory"] != 700 {
		t.Errorf("progress %q estimate %v resources %v", n.Progress, n.EstimatedDuration, n.ResourcesDuration)
	}
	if n.HostNodeName != "worker-3" || n.ExitCode != "137" || n.Outputs.Result != "done" {
		t.Errorf("host %q exit %q result %q", n.HostNodeName, n.ExitCode, n.Outputs.Result)
	}
	if len(n.Inputs.Parameters) != 2 || n.Inputs.Parameters[0].Value != "10" || n.Inputs.Parameters[1].Value != "" ||
		strings.Join(n.Inputs.Artifacts, ",") != "dataset" || len(n.Outputs.Parameters) != 1 || n.Outputs.Parameters[0].Name != "loss" {
		t.Errorf("inputs %+v outputs %+v", n.Inputs, n.Outputs)
	}
	if !n.Retried || n.Hooked || !n.MemoizationHit {
		t.Errorf("retried %v hooked %v memoized %v", n.Retried, n.Hooked, n.MemoizationHit)
	}
	p := wf.Nodes["pending"]
	if p.ExitCode != "" || p.HostNodeName != "" || !p.Inputs.Empty() || !p.Outputs.Empty() ||
		p.EstimatedDuration != 0 || p.ResourcesDuration != nil || p.Retried {
		t.Errorf("a pending node has run facts: %+v", p)
	}
}
