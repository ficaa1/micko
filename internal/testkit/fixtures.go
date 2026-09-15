package testkit

// Fixtures are explicitly synthetic reference datasets shared by F1 tests
// and downstream worker cards. They are generated, never captured; names,
// UIDs and labels are fabricated (see package doc).
import (
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// FixtureEpoch is a fixed reference time so every fixture is deterministic.
var FixtureEpoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// SyntheticPhases covers the phase set listed in the pinned upstream types
// plus an unknown phase to pin tolerant display behavior (LIST-11).
var SyntheticPhases = []string{"Pending", "Running", "Succeeded", "Failed", "Error", "WeirdFuturePhase"}

// FixtureWorkflowList builds n synthetic workflows across phases,
// deterministically ordered. Names carry the "fixture-" prefix.
func FixtureWorkflowList(ns string, n int) []core.Workflow {
	out := make([]core.Workflow, 0, n)
	for i := 0; i < n; i++ {
		phase := SyntheticPhases[i%len(SyntheticPhases)]
		name := "fixture-wf-" + pad(i)
		wf := SyntheticWorkflow(ns, name, phase, FixtureEpoch.Add(-time.Duration(n-i)*time.Minute))
		wf.Summary.Message = ""
		if phase == "Failed" {
			wf.Summary.Message = "synthetic failure message"
		}
		out = append(out, wf)
	}
	return out
}

// FixtureDAGWorkflow builds a synthetic workflow exercising DAG semantics
// from docs/development.md: a DAG boundary with task children, a retry
// node, a skipped node, and a suspend node. Children/boundary/outbound
// fields follow pinned semantics (children are boundary grouping, NOT
// dependency edges; outboundNodes connect templates to the next step).
func FixtureDAGWorkflow(ns, name string) core.Workflow {
	wf := SyntheticWorkflow(ns, name, "Failed", FixtureEpoch.Add(-30*time.Minute))
	wf.Summary.StartedAt = ptrTime(FixtureEpoch.Add(-30 * time.Minute))
	wf.Summary.FinishedAt = ptrTime(FixtureEpoch.Add(-25 * time.Minute))
	wf.Summary.Message = "one task failed"

	wf.Nodes = map[string]core.Node{
		"root": {
			ID: "root", Name: name, DisplayName: name, Type: "DAG", Phase: "Failed",
			Children:      []string{"task-a", "task-b", "task-c", "task-retry"},
			OutboundNodes: []string{"task-c"},
		},
		"task-a": {
			ID: "task-a", Name: name + ".task-a", DisplayName: "task-a",
			Type: "Pod", Phase: "Succeeded", BoundaryID: "root",
			StartedAt: ptrTime(FixtureEpoch.Add(-30 * time.Minute)),
		},
		"task-b": {
			ID: "task-b", Name: name + ".task-b", DisplayName: "task-b",
			Type: "Pod", Phase: "Failed", BoundaryID: "root",
			Message:    "synthetic exit code 1",
			StartedAt:  ptrTime(FixtureEpoch.Add(-29 * time.Minute)),
			FinishedAt: ptrTime(FixtureEpoch.Add(-28 * time.Minute)),
		},
		"task-c": {
			ID: "task-c", Name: name + ".task-c", DisplayName: "task-c",
			Type: "Suspend", Phase: "Pending", BoundaryID: "root",
		},
		"task-retry": {
			ID: "task-retry", Name: name + ".task-retry", DisplayName: "task-retry",
			Type: "Retry", Phase: "Failed", BoundaryID: "root",
			Children: []string{"retry-1", "retry-2"},
		},
		"retry-1": {
			ID: "retry-1", Name: name + ".task-retry(0)", DisplayName: "task-retry(0)",
			Type: "Pod", Phase: "Failed", BoundaryID: "task-retry",
		},
		"retry-2": {
			ID: "retry-2", Name: name + ".task-retry(1)", DisplayName: "task-retry(1)",
			Type: "Pod", Phase: "Failed", BoundaryID: "task-retry",
		},
	}
	// Raw resource JSON with unknown fields that must be preserved verbatim
	// for the resource view (DET-03).
	wf.Resource = []byte(`{"metadata":{"name":"` + name + `","namespace":"` + ns +
		`"},"spec":{"entrypoint":"synthetic-dag","unknownFutureField":{"x":1}},"status":{"phase":"Failed"}}`)
	return wf
}

// FixtureOffloadedWorkflow builds a synthetic workflow whose node status is
// not available (offloaded and not hydrated) with an explicit reason —
// DET-04's degraded shape.
func FixtureOffloadedWorkflow(ns, name string) core.Workflow {
	wf := SyntheticWorkflow(ns, name, "Running", FixtureEpoch.Add(-10*time.Minute))
	wf.Nodes = nil
	wf.NodesAvailable = false
	wf.NodesUnavailableReason = "node status offloaded and not hydrated by the server"
	return wf
}

func pad(i int) string {
	s := "0000"
	b := []byte(s)
	for j := len(b) - 1; j >= 0 && i > 0; j-- {
		b[j] = byte('0' + i%10)
		i /= 10
	}
	return string(b)
}
