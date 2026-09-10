package argo

import (
	"encoding/json"
	"testing"

	"argo-tui/internal/core"
)

// The pod name is what a node-scoped log request is addressed to. Getting it
// wrong sends the request to a pod that may belong to a different workflow,
// so these cases pin the exact algorithm Argo itself uses.
func TestGeneratePodNameFollowsTheDeclaredFormat(t *testing.T) {
	cases := []struct {
		name                                    string
		wfName, nodeName, template, nodeID, ver string
		want                                    string
	}{
		{
			name:   "v1 uses the node id verbatim",
			wfName: "wf", nodeName: "wf.step", template: "run", nodeID: "wf-123", ver: "v1",
			want: "wf-123",
		},
		{
			name:   "v2 hashes the node name behind workflow and template",
			wfName: "deploy", nodeName: "deploy.plan-prd", template: "terraform", nodeID: "deploy-99", ver: "v2",
			want: "deploy-terraform-1573283856",
		},
		{
			name:   "the root node is the workflow pod itself",
			wfName: "deploy", nodeName: "deploy", template: "main", nodeID: "deploy", ver: "v2",
			want: "deploy",
		},
		{
			name:   "an undeclared format yields no name at all",
			wfName: "deploy", nodeName: "deploy.plan", template: "t", nodeID: "deploy-1", ver: "",
			want: "",
		},
		{
			name:   "an unknown future format yields no name either",
			wfName: "deploy", nodeName: "deploy.plan", template: "t", nodeID: "deploy-1", ver: "v9",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := generatePodName(tc.wfName, tc.nodeName, tc.template, tc.nodeID, tc.ver)
			if got != tc.want {
				t.Fatalf("generatePodName = %q, want %q", got, tc.want)
			}
		})
	}
}

// The prefix must leave room for the hash inside the Kubernetes name limit,
// or the derived name would be one no pod can ever have.
func TestPodNamePrefixFitsTheKubernetesLimit(t *testing.T) {
	long := make([]byte, 400)
	for i := range long {
		long[i] = 'a'
	}
	name := generatePodName(string(long), "node", "tpl", "id", "v2")
	if len(name) > 253 {
		t.Fatalf("derived pod name is %d characters, over the 253 limit", len(name))
	}
	if name == "" {
		t.Fatal("a long workflow name must still produce a pod name")
	}
}

// A workflow parked on an approval gate must be distinguishable from an
// ordinary running one, straight out of the list response.
func TestDecodeMarksASuspendedWorkflowAndResolvesPods(t *testing.T) {
	body := []byte(`{
	  "metadata": {
	    "name": "deploy-multi-layer-abc",
	    "namespace": "batch-cd-prd",
	    "uid": "uid-1",
	    "annotations": {"workflows.argoproj.io/pod-name-format": "v2"}
	  },
	  "status": {
	    "phase": "Running",
	    "nodes": {
	      "deploy-multi-layer-abc-1": {
	        "name": "deploy-multi-layer-abc.plan-prd",
	        "displayName": "plan-prd",
	        "type": "Pod",
	        "phase": "Succeeded",
	        "templateName": "terraform-plan"
	      },
	      "deploy-multi-layer-abc-2": {
	        "name": "deploy-multi-layer-abc.suspend",
	        "displayName": "suspend",
	        "type": "Suspend",
	        "phase": "Running"
	      }
	    }
	  }
	}`)
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !wf.Summary.Suspended {
		t.Error("a running Suspend node must mark the workflow as suspended")
	}
	if wf.PodNameVersion != "v2" {
		t.Errorf("pod name version = %q, want v2", wf.PodNameVersion)
	}
	pod := wf.Nodes["deploy-multi-layer-abc-1"]
	if pod.PodName == "" {
		t.Error("a Pod node must get a resolved pod name when the format is declared")
	}
	if pod.TemplateName != "terraform-plan" {
		t.Errorf("template name = %q", pod.TemplateName)
	}
	if got := wf.Nodes["deploy-multi-layer-abc-2"].PodName; got != "" {
		t.Errorf("a Suspend node owns no pod, got pod name %q", got)
	}
}

// With no annotation the server has not said how it names pods, so nothing
// may be derived: a guessed name is worse than no node-scoped logs.
func TestNoAnnotationMeansNoDerivedPodName(t *testing.T) {
	body := []byte(`{
	  "metadata": {"name": "wf", "namespace": "ns", "uid": "u"},
	  "status": {"phase": "Running", "nodes": {
	    "wf-1": {"name": "wf.a", "type": "Pod", "phase": "Running", "templateName": "t"}
	  }}
	}`)
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := wf.Nodes["wf-1"].PodName; got != "" {
		t.Fatalf("pod name derived without a declared format: %q", got)
	}
	if wf.Summary.Suspended {
		t.Error("no Suspend node, so the workflow must not be marked suspended")
	}
}

// A node name with a retry index and a loop item, as Argo writes a nested
// step. This case
// fails if the hash function or the prefix rule ever drifts from Argo's own.
//
// The same value also appears as the node ID suffix, because Argo derives
// both from the same hash of the node name.
func TestPodNameOfANestedStep(t *testing.T) {
	got := generatePodName(
		"deploy-all-regions-r5t9z",
		"deploy-all-regions-r5t9z[1].plan-apply-stages(0:web).plan-prd",
		"preview",
		"deploy-all-regions-r5t9z-3798756459",
		"v2",
	)
	const want = "deploy-all-regions-r5t9z-preview-3798756459"
	if got != want {
		t.Fatalf("generatePodName = %q, want the pod that actually exists: %q", got, want)
	}
}

// A templateRef names the template indirectly; it must feed the same
// resolution as a plain templateName.
func TestTemplateRefFeedsPodNameResolution(t *testing.T) {
	var n core.Node
	raw := json.RawMessage(`{"name":"wf.a","type":"Pod","templateRef":{"name":"lib","template":"plan"}}`)
	n, err := decodeNode("wf-1", raw)
	if err != nil {
		t.Fatalf("decode node: %v", err)
	}
	if n.TemplateRefTemplate != "plan" {
		t.Fatalf("templateRef template = %q, want plan", n.TemplateRefTemplate)
	}
	if got := nodeTemplateName(n); got != "plan" {
		t.Fatalf("nodeTemplateName = %q, want plan", got)
	}
}
