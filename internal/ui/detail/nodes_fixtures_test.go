package detail

import (
	"testing"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

// Node-type fixtures (DET-07): every pinned node type renders
// deterministically through the outline, with honest pod/log capability
// flags. The fixtures here are synthetic (testkit policy).

// stepsFixture builds a Steps workflow: Steps -> StepGroup -> Pod children.
func stepsFixture() core.Workflow {
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf-steps", UID: "u-steps"},
			Phase: "Succeeded", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf-steps", DisplayName: "wf-steps", Type: "Steps",
				Phase: "Succeeded", Children: []string{"group-1"}},
			"group-1": {ID: "group-1", Name: "wf-steps.group-1", DisplayName: "group-1",
				Type: "StepGroup", Phase: "Succeeded", BoundaryID: "root",
				Children: []string{"pod-1"}},
			"pod-1": {ID: "pod-1", Name: "wf-steps.pod-1", DisplayName: "pod-1",
				Type: "Pod", Phase: "Succeeded", BoundaryID: "root"},
		},
		NodesAvailable: true,
	}
}

// loopFixture builds a Loop (TaskGroup over repeated pod children).
func loopFixture() core.Workflow {
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf-loop", UID: "u-loop"},
			Phase: "Succeeded", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf-loop", DisplayName: "wf-loop", Type: "DAG",
				Phase: "Succeeded", Children: []string{"fan"}},
			"fan": {ID: "fan", Name: "wf-loop.fan", DisplayName: "fan",
				Type: "TaskGroup", Phase: "Succeeded", BoundaryID: "root",
				Children: []string{"fan(0)", "fan(1)"}},
			"fan(0)": {ID: "fan(0)", Name: "wf-loop.fan(0)", DisplayName: "fan(0)",
				Type: "Pod", Phase: "Succeeded", BoundaryID: "fan"},
			"fan(1)": {ID: "fan(1)", Name: "wf-loop.fan(1)", DisplayName: "fan(1)",
				Type: "Pod", Phase: "Succeeded", BoundaryID: "fan"},
		},
		NodesAvailable: true,
	}
}

// containerSetFixture builds a ContainerSet node with container children.
func containerSetFixture() core.Workflow {
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf-cset", UID: "u-cset"},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf-cset", DisplayName: "wf-cset", Type: "DAG",
				Phase: "Running", Children: []string{"cset"}},
			"cset": {ID: "cset", Name: "wf-cset.cset", DisplayName: "cset",
				Type: "ContainerSet", Phase: "Running", BoundaryID: "root",
				Children: []string{"cset-app", "cset-sidecar"}},
			"cset-app": {ID: "cset-app", Name: "wf-cset.cset-app", DisplayName: "cset-app",
				Type: "Container", Phase: "Running", BoundaryID: "cset"},
			"cset-sidecar": {ID: "cset-sidecar", Name: "wf-cset.cset-sidecar", DisplayName: "cset-sidecar",
				Type: "Container", Phase: "Running", BoundaryID: "cset"},
		},
		NodesAvailable: true,
	}
}

// skippedFixture pins Skipped/Omitted phase rendering (protocol §8: Skipped
// is a node type; Omitted is a phase some servers emit).
func skippedFixture() core.Workflow {
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf-skip", UID: "u-skip"},
			Phase: "Succeeded", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf-skip", DisplayName: "wf-skip", Type: "DAG",
				Phase: "Succeeded", Children: []string{"when-skipped", "when-omitted"}},
			"when-skipped": {ID: "when-skipped", Name: "wf-skip.when-skipped", DisplayName: "when-skipped",
				Type: "Skipped", Phase: "Skipped", BoundaryID: "root",
				Message: "when condition false"},
			"when-omitted": {ID: "when-omitted", Name: "wf-skip.when-omitted", DisplayName: "when-omitted",
				Type: "Pod", Phase: "Omitted", BoundaryID: "root"},
		},
		NodesAvailable: true,
	}
}

// TestNodeOutlineStepsFixture: StepGroup nesting renders under the Steps
// boundary; pods keep their identity.
func TestNodeOutlineStepsFixture(t *testing.T) {
	wf := stepsFixture()
	out := BuildNodeOutline(wf, OutlineOptions{})
	if got := renderOutlineShape(out); got != "root\n  group-1\n    pod-1\n" {
		t.Fatalf("steps outline =\n%s", got)
	}
	if out.Rows[0].Children[0].Children[0].HasPod != true {
		t.Error("Pod node must report pod potential")
	}
	if out.Rows[0].Children[0].HasPod {
		t.Error("StepGroup must not claim pod potential")
	}
}

// TestNodeOutlineLoopFixture: loop fan-out children group once under the
// TaskGroup.
func TestNodeOutlineLoopFixture(t *testing.T) {
	wf := loopFixture()
	out := BuildNodeOutline(wf, OutlineOptions{})
	if got := renderOutlineShape(out); got != "root\n  fan\n    fan(0)\n    fan(1)\n" {
		t.Fatalf("loop outline =\n%s", got)
	}
}

// TestNodeOutlineContainerSetFixture: container-set children render under
// the set; the set itself has pod potential, children (Container type) do
// not claim separate pods.
func TestNodeOutlineContainerSetFixture(t *testing.T) {
	wf := containerSetFixture()
	out := BuildNodeOutline(wf, OutlineOptions{})
	if got := renderOutlineShape(out); got != "root\n  cset\n    cset-app\n    cset-sidecar\n" {
		t.Fatalf("container-set outline =\n%s", got)
	}
	if !out.Rows[0].Children[0].HasPod {
		t.Error("ContainerSet must report pod potential (docs/development.md)")
	}
}

// TestNodeOutlineSkippedOmittedFixture: skipped/omitted nodes remain
// visible with their phase — never dropped or relabeled Succeeded.
func TestNodeOutlineSkippedOmittedFixture(t *testing.T) {
	wf := skippedFixture()
	out := BuildNodeOutline(wf, OutlineOptions{})
	if got := renderOutlineShape(out); got != "root\n  when-omitted\n  when-skipped\n" {
		t.Fatalf("skipped outline =\n%s", got)
	}
	kids := out.Rows[0].Children
	// stable order ties: no timestamps → sorted by ID (when-omitted first).
	if kids[0].Phase != "Omitted" || kids[1].Phase != "Skipped" {
		t.Fatalf("phases = %q, %q; must pass through verbatim", kids[0].Phase, kids[1].Phase)
	}
	// HasPod is type-potential (protocol §8); an Omitted Pod node still has
	// Pod type, so potential stays true — the phase explains it never ran.
	// The important bit is that the node remains visible with its verbatim
	// phase (asserted above).
}

// TestNodeOutlineRetainFixture reuses the F1 retry fixture: retry children
// (retry-1, retry-2) nest under the Retry boundary node.
func TestNodeOutlineRetainFixture(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	out := BuildNodeOutline(wf, OutlineOptions{})
	// find the Retry row under root
	var retry *OutlineRow
	for i := range out.Rows[0].Children {
		if out.Rows[0].Children[i].Type == "Retry" {
			retry = &out.Rows[0].Children[i]
		}
	}
	if retry == nil {
		t.Fatal("Retry row missing")
	}
	if len(retry.Children) != 2 || retry.Children[0].DisplayName != "task-retry(0)" {
		t.Fatalf("retry children wrong: %+v", retry.Children)
	}
	if !retry.Children[0].HasPod {
		t.Error("retry attempt pods must report pod potential")
	}
}

// TestNodeOutlineSuspendFixture: Suspend nodes render structurally with no
// pod potential and no timestamps (not-yet-resumed).
func TestNodeOutlineSuspendFixture(t *testing.T) {
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf-susp", UID: "u-susp"},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf-susp", DisplayName: "wf-susp", Type: "DAG",
				Phase: "Running", Children: []string{"approve"}},
			"approve": {ID: "approve", Name: "wf-susp.approve", DisplayName: "approve",
				Type: "Suspend", Phase: "Pending", BoundaryID: "root"},
		},
		NodesAvailable: true,
	}
	out := BuildNodeOutline(wf, OutlineOptions{})
	row := out.Rows[0].Children[0]
	if row.Type != "Suspend" || row.HasPod {
		t.Fatalf("suspend row wrong: %+v", row)
	}
	if row.StartedAt != nil {
		t.Error("suspend node with no start must not fabricate a timestamp")
	}
}

// TestNodeOutlineUnknownPhasePassthrough: unknown future phases remain
// displayable verbatim (LIST-11 analog for nodes).
func TestNodeOutlineUnknownPhasePassthrough(t *testing.T) {
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "u1"},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf", DisplayName: "wf", Type: "DAG",
				Phase: "Running", Children: []string{"future"}},
			"future": {ID: "future", Name: "wf.future", DisplayName: "future",
				Type: "Pod", Phase: "WeirdFuturePhase", BoundaryID: "root"},
		},
		NodesAvailable: true,
	}
	out := BuildNodeOutline(wf, OutlineOptions{})
	if got := out.Rows[0].Children[0].Phase; got != "WeirdFuturePhase" {
		t.Fatalf("phase = %q, want verbatim passthrough", got)
	}
}
