//go:build integration

package integration

// fixtures.go — explicitly synthetic ET-2 fixture builders (plan: "fixtures
// explicitly synthetic, never mislabeled captures"). Timestamps use the
// shared synthetic epoch so tests are deterministic.

import (
	"fmt"
	"time"

	"argo-tui/internal/testkit"
)

// FixtureEpoch re-exports the shared synthetic epoch.
var FixtureEpoch = testkit.FixtureEpoch

// rfctime formats a time as RFC3339 for wire payloads.
func rfctime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// FixtureWorkflow builds one synthetic wire workflow with nodes.
func FixtureWorkflow(ns, name, phase, uid string) WireWorkflow {
	created := FixtureEpoch.Add(-30 * time.Minute)
	wf := WireWorkflow{
		Metadata: WireMetadata{
			Name:              name,
			Namespace:         ns,
			UID:               uid,
			ResourceVersion:   "100",
			CreationTimestamp: rfctime(created),
			Labels: map[string]string{
				"workflows.argoproj.io/phase": phase,
				"fixture":                     "synthetic",
			},
		},
		Status: WireWorkflowStatus{Phase: phase},
		Spec:   map[string]any{"entrypoint": "synthetic-entry"},
	}
	if phase != "Pending" {
		wf.Status.StartedAt = rfctime(created)
	}
	if phase == "Succeeded" || phase == "Failed" {
		wf.Status.FinishedAt = rfctime(FixtureEpoch.Add(-25 * time.Minute))
	}
	wf.Status.Nodes = map[string]WireNode{
		"root": {
			ID: "root", Name: name, DisplayName: name, Type: "Steps", Phase: phase,
			StartedAt: rfctime(created),
		},
		fmt.Sprintf("%s.step-1", name): {
			ID: name + ".step-1", Name: name + ".step-1", DisplayName: "step-1",
			Type: "Pod", Phase: phase, BoundaryID: "root",
			StartedAt: rfctime(created), PodName: name + "-step-pod",
		},
	}
	return wf
}

// FixtureLogEntries builds a deterministic synthetic log sequence across
// two pods (pod identity comes from podName — LogEntry has no other field).
func FixtureLogEntries(count int) []WireLogEntry {
	out := make([]WireLogEntry, 0, count)
	for i := 0; i < count; i++ {
		pod := "wf-a-step-pod"
		if i%3 == 2 {
			pod = "wf-b-step-pod"
		}
		out = append(out, WireLogEntry{
			Content: fmt.Sprintf("synthetic log line %02d: epoch %d loss=0.%03d", i+1, i+1, i*7%1000),
			PodName: pod,
		})
	}
	return out
}

// FixtureArchiveWorkflow builds an archived-generation workflow (same
// ns/name as a live one but a different UID) for UID-fallback tests.
func FixtureArchiveWorkflow(ns, name string, oldUID string) WireWorkflow {
	wf := FixtureWorkflow(ns, name, "Failed", oldUID)
	wf.Metadata.ResourceVersion = "99"
	wf.Status.Message = "synthetic archived generation"
	return wf
}
