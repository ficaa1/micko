package argo

import (
	"testing"
	"time"
)

// A node's run facts — progress, estimate, resource usage, host, exit code,
// parameters, artifacts and flags — decode from the NodeStatus shape the
// server sends. The node view and the failure explanation read all of them.
func TestDecodeNodeRunFacts(t *testing.T) {
	body := []byte(`{
	  "metadata": {"name": "wf", "namespace": "ns", "uid": "u1"},
	  "status": {
	    "phase": "Failed",
	    "progress": "2/3",
	    "estimatedDuration": 95,
	    "nodes": {
	      "wf-1": {
	        "name": "wf.train", "displayName": "train", "type": "Pod", "phase": "Failed",
	        "progress": "0/1", "estimatedDuration": 40,
	        "resourcesDuration": {"cpu": 12, "memory": 700},
	        "hostNodeName": "worker-3",
	        "inputs": {"parameters": [{"name": "epochs", "value": "10"}, {"name": "pending"}],
	                   "artifacts": [{"name": "dataset", "s3": {"key": "a/b"}}]},
	        "outputs": {"exitCode": "137", "result": "done",
	                    "parameters": [{"name": "loss", "value": "0.3"}]},
	        "nodeFlag": {"retried": true},
	        "memoizationStatus": {"hit": true, "key": "k", "cacheName": "c"}
	      }
	    }
	  }
	}`)
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Summary.Progress != "2/3" || wf.Summary.EstimatedDuration != 95*time.Second {
		t.Errorf("summary progress %q estimate %v", wf.Summary.Progress, wf.Summary.EstimatedDuration)
	}
	n := wf.Nodes["wf-1"]
	if n.Progress != "0/1" || n.EstimatedDuration != 40*time.Second {
		t.Errorf("node progress %q estimate %v", n.Progress, n.EstimatedDuration)
	}
	if n.ResourcesDuration["cpu"] != 12 || n.ResourcesDuration["memory"] != 700 {
		t.Errorf("resources %v", n.ResourcesDuration)
	}
	if n.HostNodeName != "worker-3" || n.ExitCode != "137" {
		t.Errorf("host %q exit %q", n.HostNodeName, n.ExitCode)
	}
	if len(n.Inputs.Parameters) != 2 || n.Inputs.Parameters[0].Value != "10" || n.Inputs.Parameters[1].Value != "" {
		t.Errorf("inputs %+v", n.Inputs.Parameters)
	}
	if len(n.Inputs.Artifacts) != 1 || n.Inputs.Artifacts[0] != "dataset" {
		t.Errorf("artifacts %v", n.Inputs.Artifacts)
	}
	if n.Outputs.Result != "done" || len(n.Outputs.Parameters) != 1 || n.Outputs.Parameters[0].Name != "loss" {
		t.Errorf("outputs %+v", n.Outputs)
	}
	if !n.Retried || n.Hooked || !n.MemoizationHit {
		t.Errorf("flags retried=%v hooked=%v memo=%v", n.Retried, n.Hooked, n.MemoizationHit)
	}
}

// A node that has not run carries none of the run facts, and each stays at
// its zero value rather than a fabricated one.
func TestDecodeNodeWithoutRunFacts(t *testing.T) {
	body := []byte(`{"metadata":{"name":"wf","namespace":"ns","uid":"u1"},
	  "status":{"nodes":{"n":{"name":"wf","type":"Steps","phase":"Pending"}}}}`)
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		t.Fatal(err)
	}
	n := wf.Nodes["n"]
	if n.ExitCode != "" || n.HostNodeName != "" || !n.Inputs.Empty() || !n.Outputs.Empty() ||
		n.EstimatedDuration != 0 || n.ResourcesDuration != nil || n.Retried {
		t.Errorf("unexpected run facts on a pending node: %+v", n)
	}
}
