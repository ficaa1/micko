package detail

import (
	"fmt"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// BenchmarkNodeOutline pins PERF-02: the outline build over a 1,000-node
// synthetic workflow must be linear (no exponential DAG traversal) and fast
// enough to stay keyboard-responsive.
func BenchmarkNodeOutline(b *testing.B) {
	wf := syntheticWideDAG("bench", 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := BuildNodeOutline(wf, OutlineOptions{})
		if len(out.Rows) == 0 {
			b.Fatal("empty outline")
		}
	}
}

// TestNodeOutlineScale1000Nodes is the deterministic companion to the
// benchmark (PERF-02 evidence in ordinary `go test` runs): a 1,000-node
// DAG outline completes and places every node exactly once.
func TestNodeOutlineScale1000Nodes(t *testing.T) {
	wf := syntheticWideDAG("scale", 1000)
	out := BuildNodeOutline(wf, OutlineOptions{})
	if !out.Available {
		t.Fatal("outline unavailable")
	}
	counts := map[string]int{}
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			counts[r.NodeID]++
			walk(r.Children)
		}
	}
	walk(out.Rows)
	if len(counts) != 1001 { // root + 1000 tasks
		t.Fatalf("distinct nodes in outline = %d, want 1001", len(counts))
	}
	for id, c := range counts {
		if c != 1 {
			t.Fatalf("node %s appears %d times", id, c)
		}
	}
}

// syntheticWideDAG builds a deterministic DAG with one root and n task
// children (plus n/2 diamond cross-references from earlier tasks to later
// tasks via children lists of task nodes — exercising the visited-set
// against diamond re-walks).
func syntheticWideDAG(prefix string, n int) core.Workflow {
	nodes := make(map[string]core.Node, n+1)
	root := core.Node{
		ID: prefix + "-root", Name: prefix, DisplayName: prefix, Type: "DAG",
		Phase: "Running", Children: make([]string, 0, n),
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-task-%04d", prefix, i)
		root.Children = append(root.Children, id)
		nd := core.Node{
			ID: id, Name: prefix + "." + id, DisplayName: id, Type: "Pod",
			Phase: "Succeeded", BoundaryID: root.ID,
		}
		// Diamond edges: every 2nd task references a later task as a child
		// (creating overlapping paths that would go exponential without a
		// visited set).
		if i%2 == 0 && i+2 < n {
			later := fmt.Sprintf("%s-task-%04d", prefix, i+2)
			nd.Children = []string{later}
		}
		nodes[id] = nd
	}
	nodes[root.ID] = root
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: prefix, UID: "u-" + prefix},
			Phase: "Running", CreatedAt: testkit.FixtureEpoch,
		},
		Nodes:          nodes,
		NodesAvailable: true,
	}
}
