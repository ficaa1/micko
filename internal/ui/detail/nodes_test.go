package detail

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// outlineFixture returns the F1 DAG fixture (retry, suspend, skipped-style
// phases, boundary grouping) for outline tests.
func outlineFixture(t *testing.T) core.Workflow {
	t.Helper()
	return testkit.FixtureDAGWorkflow("ns", "fixture-dag")
}

// TestNodeOutlineDAGGroupsSharedChildrenOnce pins DET-06: children of one
// boundary group appear once, in deterministic order, under their boundary
// node — never recursively duplicated.
func TestNodeOutlineDAGGroupsSharedChildrenOnce(t *testing.T) {
	wf := outlineFixture(t)
	out := BuildNodeOutline(wf, OutlineOptions{})

	// The outline must contain every node exactly once.
	counts := map[string]int{}
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			counts[r.NodeID]++
			walk(r.Children)
		}
	}
	walk(out.Rows)

	for id := range wf.Nodes {
		if counts[id] != 1 {
			t.Errorf("node %q appears %d times, want exactly 1", id, counts[id])
		}
	}
	root := out.Rows
	if len(root) == 0 || root[0].NodeID != "root" {
		t.Fatalf("outline must start at the workflow root, got %+v", root)
	}
	var childIDs []string
	for _, r := range root[0].Children {
		childIDs = append(childIDs, r.NodeID)
	}
	want := []string{"task-a", "task-b", "task-c", "task-retry"}
	if len(childIDs) != len(want) {
		t.Fatalf("root children = %v, want %v", childIDs, want)
	}
	for i, id := range want {
		if childIDs[i] != id {
			t.Errorf("root child[%d] = %q, want %q", i, childIDs[i], id)
		}
	}
	retry := root[0].Children[3]
	if len(retry.Children) != 2 || retry.Children[0].NodeID != "retry-1" || retry.Children[1].NodeID != "retry-2" {
		t.Fatalf("retry children wrong: %+v", retry.Children)
	}
}

// TestNodeOutlineStableOrder pins DET-10: siblings order by start time then
// node ID; nodes without timestamps sort after timestamped ones (explicit
// documented rule), still deterministically by ID.
func TestNodeOutlineStableOrder(t *testing.T) {
	epoch := testkit.FixtureEpoch
	late := epoch.Add(5 * time.Minute)
	mk := func(id string, start *time.Time) core.Node {
		return core.Node{
			ID: id, Name: "wf." + id, DisplayName: id, Type: "Pod",
			Phase: "Succeeded", BoundaryID: "root", StartedAt: start,
		}
	}
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "u1"},
			Phase: "Running", CreatedAt: epoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf", DisplayName: "wf", Type: "DAG", Phase: "Running",
				Children: []string{"zeta", "alpha", "beta", "notimestamp", "alpha"}},
			"zeta":        mk("zeta", &late),
			"alpha":       mk("alpha", &epoch),
			"beta":        mk("beta", &epoch),
			"notimestamp": mk("notimestamp", nil),
		},
		NodesAvailable: true,
	}
	out := BuildNodeOutline(wf, OutlineOptions{})
	var got []string
	for _, r := range out.Rows[0].Children {
		got = append(got, r.NodeID)
	}
	// alpha and beta share the earliest start; tie broken by ID. The
	// duplicate child reference "alpha" must not create a second row.
	want := []string{"alpha", "beta", "zeta", "notimestamp"}
	if len(got) != len(want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("child[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestNodeOutlineDefensiveCycleAndMissing pins DET-08: cycles are cut and
// missing node references are surfaced in an explicit ungrouped section
// instead of dropping or hanging.
func TestNodeOutlineDefensiveCycleAndMissing(t *testing.T) {
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "u1"},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf", DisplayName: "wf", Type: "DAG", Phase: "Running",
				Children: []string{"a", "ghost"}},
			"a": {ID: "a", Name: "wf.a", DisplayName: "a", Type: "Pod", Phase: "Running",
				BoundaryID: "root",
				// cycle: a lists root as child
				Children: []string{"root"}},
			// "orphan" is present in the map but referenced by nobody.
			"orphan": {ID: "orphan", Name: "wf.orphan", DisplayName: "orphan",
				Type: "Pod", Phase: "Succeeded"},
		},
		NodesAvailable: true,
	}
	out := BuildNodeOutline(wf, OutlineOptions{})

	counts := map[string]int{}
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			counts[r.NodeID]++
			walk(r.Children)
		}
	}
	walk(out.Rows)
	if counts["a"] != 1 {
		t.Errorf("node a appears %d times (cycle must be cut once)", counts["a"])
	}
	if counts["root"] != 1 {
		t.Errorf("root appears %d times; cycle back-edge must not duplicate it", counts["root"])
	}
	if counts["ghost"] != 0 {
		t.Errorf("ghost is a dangling reference; must appear in ungrouped section, not as a row (got %d)", counts["ghost"])
	}
	// ghost referenced by root.Children but missing from the map: must be
	// listed in the explicit ungrouped/dangling section.
	foundGhost := false
	for _, d := range out.Dangling {
		if d.NodeID == "ghost" {
			foundGhost = true
		}
	}
	if !foundGhost {
		t.Fatalf("dangling references not surfaced: %+v", out.Dangling)
	}
	// "orphan" is present in the map but referenced by nobody and has no
	// boundary: it surfaces as a top-level row (all relevant nodes must be
	// reachable or listed in an ungrouped section — plan gate). It must
	// appear exactly once, not dropped.
	if counts["orphan"] != 1 {
		t.Fatalf("unreferenced node %q must appear exactly once as a top-level row (got %d)", "orphan", counts["orphan"])
	}
}

// TestNodeOutlineComplexCycleStillCompletes: a two-node cycle must terminate
// quickly; guard against exponential re-walks (DET-08, PERF-02 spirit).
func TestNodeOutlineComplexCycleStillCompletes(t *testing.T) {
	nodes := map[string]core.Node{
		"root": {ID: "root", Name: "wf", DisplayName: "wf", Type: "DAG", Phase: "Running",
			Children: []string{"c1", "c2"}},
	}
	// c1 <-> c2 cycle.
	nodes["c1"] = core.Node{ID: "c1", Name: "wf.c1", DisplayName: "c1", Type: "Pod",
		Phase: "Running", BoundaryID: "root", Children: []string{"c2"}}
	nodes["c2"] = core.Node{ID: "c2", Name: "wf.c2", DisplayName: "c2", Type: "Pod",
		Phase: "Running", BoundaryID: "root", Children: []string{"c1"}}
	wf := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "u1"},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes:          nodes,
		NodesAvailable: true,
	}
	done := make(chan struct{})
	go func() {
		_ = BuildNodeOutline(wf, OutlineOptions{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("outline traversal did not terminate (cycle guard missing)")
	}
}

// TestNodeOutlineOffloadedUnavailable pins DET-04: an offloaded/unhydrated
// node map must produce the explicit unavailable state, never an empty
// outline.
func TestNodeOutlineOffloadedUnavailable(t *testing.T) {
	wf := testkit.FixtureOffloadedWorkflow("ns", "fixture-offloaded")
	out := BuildNodeOutline(wf, OutlineOptions{})
	if out.Available {
		t.Fatal("out.Available must be false for offloaded node status")
	}
	if out.UnavailableReason != wf.NodesUnavailableReason {
		t.Fatalf("reason = %q, want %q", out.UnavailableReason, wf.NodesUnavailableReason)
	}
	if len(out.Rows) != 0 {
		t.Fatalf("unavailable outline must not synthesize rows: %+v", out.Rows)
	}
}

// TestNodeOutlineEmptyButAvailable: a not-yet-started workflow has zero
// nodes and that is meaningful (distinct from unavailable).
func TestNodeOutlineEmptyButAvailable(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "pending-wf", "Pending", testkit.FixtureEpoch)
	out := BuildNodeOutline(wf, OutlineOptions{})
	if !out.Available {
		t.Fatal("empty node map with NodesAvailable=true is available")
	}
	if len(out.Rows) != 0 {
		t.Fatalf("expected no rows, got %+v", out.Rows)
	}
	if len(out.Dangling) != 0 || len(out.Unreachable) != 0 {
		t.Fatal("empty outline must have no dangling/unreachable entries")
	}
}

// TestNodeOutlineOutboundNotChildren pins DET-09: outboundNodes are never
// used as outline grouping edges. task-b appears exactly once via its
// boundaryID (root) grouping — the outline must be identical to a workflow
// whose root carries no outboundNodes at all.
func TestNodeOutlineOutboundNotChildren(t *testing.T) {
	base := core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "u1"},
			Phase: "Succeeded", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes: map[string]core.Node{
			"root": {ID: "root", Name: "wf", DisplayName: "wf", Type: "DAG", Phase: "Succeeded",
				Children: []string{"task-a"}},
			"task-a": {ID: "task-a", Name: "wf.task-a", DisplayName: "task-a", Type: "Pod",
				Phase: "Succeeded", BoundaryID: "root"},
			"task-b": {ID: "task-b", Name: "wf.task-b", DisplayName: "task-b", Type: "Pod",
				Phase: "Succeeded", BoundaryID: "root"},
		},
		NodesAvailable: true,
	}
	withOutbound := base
	rootNode := base.Nodes["root"]
	rootNode.OutboundNodes = []string{"task-b"}
	withOutbound.Nodes = map[string]core.Node{
		"root":   rootNode,
		"task-a": base.Nodes["task-a"],
		"task-b": base.Nodes["task-b"],
	}

	outBase := BuildNodeOutline(base, OutlineOptions{})
	outOut := BuildNodeOutline(withOutbound, OutlineOptions{})

	// Adding outboundNodes must not change the rendered outline.
	if renderOutlineShape(outBase) != renderOutlineShape(outOut) {
		t.Fatalf("outline changed when outboundNodes added:\nbase:   %s\noutbound: %s",
			renderOutlineShape(outBase), renderOutlineShape(outOut))
	}
	// And task-b must still be reachable exactly once via boundary grouping.
	counts := map[string]int{}
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			counts[r.NodeID]++
			walk(r.Children)
		}
	}
	walk(outOut.Rows)
	if counts["task-b"] != 1 {
		t.Errorf("task-b appears %d times; must appear exactly once via boundary grouping", counts["task-b"])
	}
}

// renderOutlineShape renders the outline as a deterministic text shape for
// equality comparison in tests.
func renderOutlineShape(out Outline) string {
	s := ""
	var walk func(rows []OutlineRow, depth int)
	walk = func(rows []OutlineRow, depth int) {
		for _, r := range rows {
			for i := 0; i < depth; i++ {
				s += "  "
			}
			s += r.NodeID + "\n"
			walk(r.Children, depth+1)
		}
	}
	walk(out.Rows, 0)
	return s
}
