package detail

import (
	"testing"

	"argo-tui/internal/core"
)

// runningUnderSkippedWorkflow has a Skipped step whose subtree still holds
// an approval that ran and a pod that is Running. Argo skips the step itself
// while its children proceed.
func runningUnderSkippedWorkflow() core.Workflow {
	node := func(id, name, typ, phase, boundary string, children ...string) core.Node {
		return core.Node{
			ID: id, Name: name, DisplayName: name, Type: typ, Phase: phase,
			BoundaryID: boundary, Children: children,
		}
	}
	nodes := map[string]core.Node{
		"root":           node("root", "deploy", "Steps", "Running", "", "skipped-parent", "dead-branch"),
		"skipped-parent": node("skipped-parent", "plan-backend-tst", "Skipped", "Skipped", "root", "approval"),
		"approval":       node("approval", "approval", "Steps", "Succeeded", "skipped-parent", "apply-int"),
		"apply-int":      node("apply-int", "apply-int", "Pod", "Running", "approval"),
		"dead-branch":    node("dead-branch", "plan-dev", "Skipped", "Skipped", "root", "dead-child"),
		"dead-child":     node("dead-child", "apply-dev", "Skipped", "Skipped", "dead-branch"),
	}
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: "deploy", UID: "u1"}, Phase: "Running"},
		Nodes:          nodes,
		NodesAvailable: true,
	}
}

// A node that ran must never disappear because an ancestor was skipped:
// hiding the parent would take the running pod with it, leaving h as the
// only way to see running work.
func TestFlattenKeepsRunningWorkUnderASkippedParent(t *testing.T) {
	out := BuildNodeOutline(runningUnderSkippedWorkflow(), OutlineOptions{})

	shown, hidden := FlattenOutline(out, true)

	names := map[string]string{}
	for _, r := range shown {
		names[r.Row.DisplayName] = r.Row.Phase
	}
	if _, ok := names["apply-int"]; !ok {
		t.Error("apply-int (Running) is hidden while hideSkipped is on")
	}
	if _, ok := names["approval"]; !ok {
		t.Error("approval (Succeeded) is hidden while hideSkipped is on")
	}
	// The skipped parent stays as the path to the running node.
	if _, ok := names["plan-backend-tst"]; !ok {
		t.Error("the skipped parent is hidden, so the running node has no path")
	}
	// A subtree where nothing ran is still hidden; that is the whole point
	// of the key.
	if _, ok := names["plan-dev"]; ok {
		t.Error("a fully skipped subtree survived hiding")
	}
	if hidden != 2 {
		t.Errorf("hidden = %d, want 2 (the fully skipped branch and its child)", hidden)
	}
}
