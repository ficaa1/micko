package detail

import (
	"fmt"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
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

// BenchmarkNodeOutline5000 is the same build at 5,000 tasks. Against the
// 1,000-task benchmark it shows the build grows linearly, apart from the
// sorting.
func BenchmarkNodeOutline5000(b *testing.B) {
	wf := syntheticWideDAG("bench", 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := BuildNodeOutline(wf, OutlineOptions{}); len(out.Rows) == 0 {
			b.Fatal("empty outline")
		}
	}
}

// BenchmarkNodeOutlineSteps5000 builds a 5,000-step steps template, where
// every step lists the next StepGroup as its child: the chain the tree
// flattens.
func BenchmarkNodeOutlineSteps5000(b *testing.B) {
	wf := syntheticStepsChain("bench", 5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := BuildNodeOutline(wf, OutlineOptions{}); len(out.Rows) == 0 {
			b.Fatal("empty outline")
		}
	}
}

// BenchmarkNodesTabFold5000 is one fold key on a 5,000-node tab: the
// re-flatten and the column measure that follow every fold, find and
// toggle.
func BenchmarkNodesTabFold5000(b *testing.B) {
	m := New()
	m.SetSize(160, 50)
	m.SetWorkflow(syntheticWideDAG("bench", 5000), testkit.FixtureEpoch)
	m.handleKey("tab")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.handleKey("space")
	}
}

// BenchmarkNodesTabRender5000 is one frame of a 5,000-node tab: the header,
// the status line and a screenful of rows.
func BenchmarkNodesTabRender5000(b *testing.B) {
	m := New()
	m.SetSize(160, 50)
	m.SetWorkflow(syntheticWideDAG("bench", 5000), testkit.FixtureEpoch)
	m.handleKey("tab")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(m.BodyLines()) == 0 {
			b.Fatal("empty body")
		}
	}
}

// Every node of a 5,000-task DAG and of a 5,000-step steps chain is placed
// once, and a chain of 2,000 nested DAGs, the deepest tree the build can be
// handed, completes too.
func TestNodeOutlineScale5000Nodes(t *testing.T) {
	for name, wf := range map[string]core.Workflow{
		"dag":    syntheticWideDAG("scale", 5000),
		"steps":  syntheticStepsChain("scale", 5000),
		"nested": syntheticNestedDAGs("scale", 2000),
	} {
		t.Run(name, func(t *testing.T) {
			out := BuildNodeOutline(wf, OutlineOptions{})
			placedOnce(t, wf, out)
			f := Flatten(out, FlattenOptions{})
			if want := len(wf.Nodes) - out.StepGroups; len(f.Rows) != want {
				t.Fatalf("flat rows = %d, want %d", len(f.Rows), want)
			}
		})
	}
}

// syntheticStepsChain builds a steps template of n steps in groups of two,
// in the controller's shape: every step of a group lists the next group.
func syntheticStepsChain(prefix string, n int) core.Workflow {
	nodes := make(map[string]core.Node, n+n/2+1)
	root := core.Node{ID: prefix, Name: prefix, DisplayName: prefix, Type: "Steps", Phase: "Running"}
	groupID := func(g int) string { return fmt.Sprintf("%s-g%05d", prefix, g) }
	root.Children = []string{groupID(0)}
	nodes[root.ID] = root
	groups := (n + 1) / 2
	for g := 0; g < groups; g++ {
		group := core.Node{
			ID: groupID(g), Name: fmt.Sprintf("%s[%d]", prefix, g), DisplayName: fmt.Sprintf("[%d]", g),
			Type: "StepGroup", Phase: "Succeeded", BoundaryID: prefix,
		}
		for s := 0; s < 2 && g*2+s < n; s++ {
			id := fmt.Sprintf("%s-s%05d", prefix, g*2+s)
			step := core.Node{
				ID: id, Name: fmt.Sprintf("%s[%d].step-%d", prefix, g, s), DisplayName: fmt.Sprintf("step-%d", g*2+s),
				Type: "Pod", Phase: "Succeeded", BoundaryID: prefix,
			}
			if g+1 < groups {
				step.Children = []string{groupID(g + 1)}
			}
			group.Children = append(group.Children, id)
			nodes[id] = step
		}
		nodes[group.ID] = group
	}
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: prefix, UID: "u-" + prefix}, Phase: "Running"},
		Nodes:          nodes,
		NodesAvailable: true,
	}
}

// syntheticNestedDAGs builds depth DAGs, each the only task of the one
// above it, with a pod at the bottom.
func syntheticNestedDAGs(prefix string, depth int) core.Workflow {
	nodes := make(map[string]core.Node, depth+1)
	parent := ""
	for d := 0; d < depth; d++ {
		id := fmt.Sprintf("%s-d%05d", prefix, d)
		n := core.Node{ID: id, Name: id, DisplayName: id, Type: "DAG", Phase: "Running", BoundaryID: parent}
		if d == 0 {
			n.Name = prefix
		}
		nodes[id] = n
		if parent != "" {
			p := nodes[parent]
			p.Children = []string{id}
			nodes[parent] = p
		}
		parent = id
	}
	leaf := prefix + "-leaf"
	nodes[leaf] = core.Node{ID: leaf, Name: leaf, DisplayName: "leaf", Type: "Pod", Phase: "Running", BoundaryID: parent}
	p := nodes[parent]
	p.Children = []string{leaf}
	nodes[parent] = p
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: prefix, UID: "u-" + prefix}, Phase: "Running"},
		Nodes:          nodes,
		NodesAvailable: true,
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

// BenchmarkTimelineFold5000 is one fold key on the timeline of a 5,000-node
// workflow: the tree copy, the critical path and the re-flatten.
func BenchmarkTimelineFold5000(b *testing.B) {
	m := New()
	m.SetSize(160, 50)
	m.SetWorkflow(syntheticWideDAG("bench", 5000), testkit.FixtureEpoch)
	m.SetSection("timeline")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.handleKey("space")
	}
}

// BenchmarkTimelineRender5000 is one frame of that timeline.
func BenchmarkTimelineRender5000(b *testing.B) {
	m := New()
	m.SetSize(160, 50)
	m.SetWorkflow(syntheticWideDAG("bench", 5000), testkit.FixtureEpoch)
	m.SetSection("timeline")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(m.BodyLines()) == 0 {
			b.Fatal("empty body")
		}
	}
}
