package detail

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// shape draws the tree as indented names with dependencies and roles, then the ungrouped nodes.
func shape(out Outline) string {
	var b strings.Builder
	var walk func(rows []OutlineRow, depth int)
	walk = func(rows []OutlineRow, depth int) {
		for _, r := range rows {
			b.WriteString(strings.Repeat("  ", depth) + r.DisplayName)
			if len(r.Deps) > 0 {
				b.WriteString(" <- " + strings.Join(r.Deps, ","))
			}
			if r.Role != "" {
				b.WriteString(" [" + r.Role + "]")
			}
			b.WriteString("\n")
			walk(r.Children, depth+1)
		}
	}
	walk(out.Rows, 0)
	for _, d := range out.Dangling {
		b.WriteString("dangling " + d.NodeID + "\n")
	}
	for _, u := range out.Unreachable {
		b.WriteString("unreachable " + u.NodeID + "\n")
	}
	return b.String()
}

// placedOnce checks every node is drawn once, listed as ungrouped, or a StepGroup drawn as its steps.
func placedOnce(t *testing.T, wf core.Workflow, out Outline) {
	t.Helper()
	counts := map[string]int{}
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for _, r := range rows {
			counts[r.NodeID]++
			walk(r.Children)
		}
	}
	walk(out.Rows)
	for _, u := range out.Unreachable {
		counts[u.NodeID]++
	}
	groups := 0
	for id, n := range wf.Nodes {
		if n.Type == "StepGroup" && counts[id] == 0 {
			groups++
			continue
		}
		if counts[id] != 1 {
			t.Errorf("node %q placed %d times, want 1", id, counts[id])
		}
	}
	if groups != out.StepGroups {
		t.Errorf("StepGroups = %d, but %d StepGroups are not drawn", out.StepGroups, groups)
	}
}

// The tree follows the pipeline, and malformed data is still placed once.
func TestNodeTree(t *testing.T) {
	cases := []struct {
		name   string
		f      *treeFixture
		want   string
		groups int
	}{
		{"a steps chain is flat, steps in template order", newTreeFixture("wf").
			node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
			node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "build").
			node("build", "wf[0].build", "build", "Pod", "Succeeded", "wf", 0, "g1").
			node("g1", "wf[1]", "[1]", "StepGroup", "Succeeded", "wf", 3, "test-b", "test-a").
			node("test-b", "wf[1].test-b", "test-b", "Pod", "Succeeded", "wf", 3, "g2").
			node("test-a", "wf[1].test-a", "test-a", "Pod", "Succeeded", "wf", 3, "g2").
			node("g2", "wf[2]", "[2]", "StepGroup", "Running", "wf", 7, "approve").
			node("approve", "wf[2].approve", "approve", "Suspend", "Running", "wf", 7, "g3").
			node("g3", "wf[3]", "[3]", "StepGroup", "Pending", "wf", -1),
			"wf\n  build\n  test-b\n  test-a\n  approve\n", 4},
		{"a steps template called from a step nests", newTreeFixture("wf").
			node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
			node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "inner").
			node("inner", "wf[0].inner", "inner", "Steps", "Succeeded", "wf", 0, "i0").
			node("i0", "wf[0].inner[0]", "[0]", "StepGroup", "Succeeded", "inner", 0, "leaf").
			node("leaf", "wf[0].inner[0].leaf", "leaf", "Pod", "Succeeded", "inner", 0, "g1").
			node("g1", "wf[1]", "[1]", "StepGroup", "Running", "wf", 2, "after").
			node("after", "wf[1].after", "after", "Pod", "Running", "wf", 2),
			"wf\n  inner\n    leaf\n  after\n", 3},
		{"DAG tasks in dependency order, earlier start first", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "a").
			node("a", "wf.a", "a", "Pod", "Succeeded", "wf", 0, "c", "b").
			node("b", "wf.b", "b", "Pod", "Succeeded", "wf", 3, "d").
			node("c", "wf.c", "c", "Pod", "Succeeded", "wf", 2, "d").
			node("d", "wf.d", "d", "Pod", "Running", "wf", 5),
			"wf\n  a\n  c <- a\n  b <- a\n  d <- c,b\n", 0},
		{"dependency order beats start time", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "late-root", "early").
			node("late-root", "wf.late-root", "late-root", "Pod", "Pending", "wf", -1, "join").
			node("early", "wf.early", "early", "Pod", "Succeeded", "wf", 0, "join").
			node("join", "wf.join", "join", "Pod", "Pending", "wf", -1),
			"wf\n  early\n  late-root\n  join <- early,late-root\n", 0},
		{"a dependency cycle terminates", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "a").
			node("a", "wf.a", "a", "Pod", "Running", "wf", 0, "b").
			node("b", "wf.b", "b", "Pod", "Running", "wf", 1, "a"),
			"wf\n  a <- b\n  b <- a\n", 0},
		{"ties order by name, numbers as numbers", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "id-90", "id-98", "id-99", "id-91").
			node("id-90", "wf.shard(10)", "shard(10)", "Pod", "Running", "wf", 1).
			node("id-98", "wf.shard(2)", "shard(2)", "Pod", "Running", "wf", 1).
			node("id-99", "wf.shard(1)", "shard(1)", "Pod", "Running", "wf", 1).
			node("id-91", "wf.shard(9)", "shard(9)", "Pod", "Running", "wf", 1),
			"wf\n  shard(1)\n  shard(2)\n  shard(9)\n  shard(10)\n", 0},
		{"untimed after timed, ties by ID, a repeated child once", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "zeta", "alpha", "beta", "untimed", "alpha").
			node("zeta", "wf.zeta", "zeta", "Pod", "Succeeded", "wf", 5).
			node("alpha", "wf.alpha", "alpha", "Pod", "Succeeded", "wf", 0).
			node("beta", "wf.beta", "beta", "Pod", "Succeeded", "wf", 0).
			node("untimed", "wf.untimed", "untimed", "Pod", "Succeeded", "wf", -1),
			"wf\n  alpha\n  beta\n  zeta\n  untimed\n", 0},
		{"retry attempts nest in run order", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Failed", "", 0, "extract").
			node("extract", "wf.extract", "extract", "Pod", "Succeeded", "wf", 0, "retry").
			node("retry", "wf.transform", "transform", "Retry", "Failed", "wf", 1, "t0", "t1", "t2").
			node("t0", "wf.transform(0)", "transform(0)", "Pod", "Failed", "wf", 1).
			node("t1", "wf.transform(1)", "transform(1)", "Pod", "Failed", "wf", 2).
			node("t2", "wf.transform(2)", "transform(2)", "Pod", "Failed", "wf", 3, "load").
			node("load", "wf.load", "load", "Pod", "Omitted", "wf", -1),
			"wf\n  extract\n  transform <- extract\n    transform(0)\n    transform(1)\n    transform(2)\n  load <- transform\n", 0},
		{"an attempt by its flag, a dependent on the Retry node apart", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Failed", "", 0, "retry").
			node("retry", "wf.flaky", "flaky", "Retry", "Failed", "wf", 0, "first", "next").
			node("first", "wf.flaky-attempt", "attempt", "Pod", "Failed", "wf", 0).
			node("next", "wf.next", "next", "Pod", "Omitted", "wf", -1).
			with("first", func(n *core.Node) { n.Retried = true }),
			"wf\n  flaky\n    attempt\n  next <- flaky\n", 0},
		{"loop items under their TaskGroup, in creation order", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "prep").
			node("prep", "wf.prep", "prep", "Pod", "Succeeded", "wf", 0, "fan").
			node("fan", "wf.fan", "fan", "TaskGroup", "Running", "wf", 1, "fan1", "fan0").
			node("fan0", "wf.fan(0:a)", "fan(0:a)", "Pod", "Succeeded", "wf", 1, "join").
			node("fan1", "wf.fan(1:b)", "fan(1:b)", "Pod", "Running", "wf", 1, "join").
			node("join", "wf.join", "join", "Pod", "Pending", "wf", -1),
			"wf\n  prep\n  fan <- prep\n    fan(1:b)\n    fan(0:a)\n  join <- fan\n", 0},
		{"a container set holds its containers", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "cset").
			node("cset", "wf.cset", "cset", "ContainerSet", "Running", "wf", 0, "app", "sidecar").
			node("app", "wf.cset.app", "app", "Container", "Running", "cset", 0).
			node("sidecar", "wf.cset.sidecar", "sidecar", "Container", "Running", "cset", 0),
			"wf\n  cset\n    app\n    sidecar\n", 0},
		{"outbound nodes are not children", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Succeeded", "", 0, "a").
			node("a", "wf.a", "a", "Pod", "Succeeded", "wf", 0).
			node("b", "wf.b", "b", "Pod", "Succeeded", "wf", 1).
			with("wf", func(n *core.Node) { n.OutboundNodes = []string{"b"} }),
			"wf\n  a\n  b\n", 0},
		{"the exit handler is a tree of its own after the workflow's", newTreeFixture("wf").
			node("stray", "stray", "stray", "Pod", "Succeeded", "", 0).
			node("wf", "wf", "wf", "Steps", "Succeeded", "", 1).
			node("exit", "wf.onExit", "wf.onExit", "Pod", "Succeeded", "", 9).
			with("exit", func(n *core.Node) { n.Hooked = true }),
			"wf\nwf.onExit [exit handler]\nstray\n", 0},
		{"a missing boundary: under the lister, else at the top", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "listed").
			node("listed", "wf.listed", "listed", "Pod", "Running", "gone", 1).
			node("alone", "wf.alone", "alone", "Pod", "Running", "gone", 2).
			node("group", "wf[0]", "[0]", "StepGroup", "Running", "gone", 3, "step").
			node("step", "wf[0].step", "step", "Pod", "Running", "", 3),
			"wf\n  listed\nalone\n[0]\n  step\n", 0},
		{"an ownership cycle is cut and a missing child listed", newTreeFixture("wf").
			node("a", "wf.a", "a", "DAG", "Running", "b", 0, "ghost").
			node("b", "wf.b", "b", "DAG", "Running", "a", 1).
			node("self", "wf.self", "self", "Pod", "Running", "self", 2, "self"),
			"a\n  b\nself\ndangling ghost\n", 0},
		{"a child cycle back to the root", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "a", "ghost").
			node("a", "wf.a", "a", "Pod", "Running", "wf", 0, "wf").
			node("orphan", "wf.orphan", "orphan", "Pod", "Succeeded", "", 1),
			"wf\n  a\norphan\ndangling ghost\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wf := c.f.workflow()
			out := BuildNodeOutline(wf, OutlineOptions{})
			if got := shape(out); got != c.want {
				t.Fatalf("tree =\n%s\nwant\n%s", got, c.want)
			}
			placedOnce(t, wf, out)
			if out.StepGroups != c.groups {
				t.Errorf("StepGroups = %d, want %d", out.StepGroups, c.groups)
			}
		})
	}
}

// Each row keeps the server's facts and invents none.
func TestNodeTreeRowFacts(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "pod", "cset", "gate", "future", "omitted", "skipped", "retry").
		node("pod", "wf.pod", "pod", "Pod", "Running", "wf", 0).
		node("cset", "wf.cset", "cset", "ContainerSet", "Running", "wf", 0, "ctr").
		node("ctr", "wf.cset.ctr", "ctr", "Container", "Running", "cset", 0).
		node("gate", "wf.gate", "gate", "Suspend", "Pending", "wf", -1).
		node("future", "wf.future", "future", "Pod", "WeirdFuturePhase", "wf", 0).
		node("omitted", "wf.omitted", "omitted", "Pod", "Omitted", "wf", -1).
		node("skipped", "wf.skipped", "skipped", "Skipped", "Skipped", "wf", -1).
		node("retry", "wf.retry", "retry", "Retry", "Failed", "wf", 0, "r0", "r1").
		node("r0", "wf.retry(0)", "retry(0)", "Pod", "Failed", "wf", 0).
		node("r1", "wf.retry(1)", "retry(1)", "Pod", "Failed", "wf", 1)
	rows := map[string]OutlineRow{}
	var walk func([]OutlineRow)
	walk = func(rs []OutlineRow) {
		for _, r := range rs {
			rows[r.DisplayName] = r
			walk(r.Children)
		}
	}
	walk(BuildNodeOutline(f.workflow(), OutlineOptions{}).Rows)
	for _, c := range []struct {
		name, phase string
		hasPod      bool
		started     bool
	}{
		{"wf", "Running", false, true},
		{"pod", "Running", true, true},
		{"cset", "Running", true, true},
		{"ctr", "Running", false, true},
		{"gate", "Pending", false, false},
		{"future", "WeirdFuturePhase", true, true},
		{"omitted", "Omitted", true, false},
		{"skipped", "Skipped", false, false},
		{"retry(1)", "Failed", true, true},
	} {
		r, ok := rows[c.name]
		switch {
		case !ok:
			t.Errorf("no row %q", c.name)
		case r.Phase != c.phase || r.HasPod != c.hasPod || (r.StartedAt != nil) != c.started:
			t.Errorf("%s: phase %q, has pod %v, started %v", c.name, r.Phase, r.HasPod, r.StartedAt != nil)
		case r.PodName != "":
			t.Errorf("%s: pod name %q derived from its ID", c.name, r.PodName)
		}
	}
	if got := rows["retry"].Retries; got != 1 {
		t.Errorf("Retries = %d, want 1", got)
	}
}

// Each node names the siblings it waited for.
func TestNodeTreeRecordsWhatEachNodeWaitedFor(t *testing.T) {
	after := func(f *treeFixture) map[string]string {
		m := map[string]string{}
		var walk func(rows []OutlineRow)
		walk = func(rows []OutlineRow) {
			for _, r := range rows {
				m[r.NodeID] = strings.Join(r.After, ",")
				walk(r.Children)
			}
		}
		walk(BuildNodeOutline(f.workflow(), OutlineOptions{}).Rows)
		return m
	}
	for _, c := range []struct {
		name string
		f    *treeFixture
		want map[string]string
	}{
		{"steps", newTreeFixture("wf").
			node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
			node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "build").
			node("build", "wf[0].build", "build", "Pod", "Succeeded", "wf", 0, "g1").
			node("g1", "wf[1]", "[1]", "StepGroup", "Succeeded", "wf", 3, "test-b", "test-a").
			node("test-b", "wf[1].test-b", "test-b", "Pod", "Succeeded", "wf", 3, "g2").
			node("test-a", "wf[1].test-a", "test-a", "Pod", "Succeeded", "wf", 3, "g2").
			node("g2", "wf[2]", "[2]", "StepGroup", "Running", "wf", 7, "approve").
			node("approve", "wf[2].approve", "approve", "Suspend", "Running", "wf", 7),
			map[string]string{"wf": "", "build": "", "test-b": "build", "test-a": "build", "approve": "test-b,test-a"}},
		{"dag", newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Failed", "", 0, "extract").
			node("extract", "wf.extract", "extract", "Pod", "Succeeded", "wf", 0, "retry", "side").
			node("retry", "wf.transform", "transform", "Retry", "Failed", "wf", 1, "t0", "t1", "t2").
			node("t0", "wf.transform(0)", "transform(0)", "Pod", "Failed", "wf", 1).
			node("t1", "wf.transform(1)", "transform(1)", "Pod", "Failed", "wf", 2).
			node("t2", "wf.transform(2)", "transform(2)", "Pod", "Failed", "wf", 3, "load").
			node("side", "wf.side", "side", "Pod", "Succeeded", "wf", 1, "load").
			node("load", "wf.load", "load", "Pod", "Omitted", "wf", -1),
			map[string]string{"extract": "", "retry": "extract", "side": "extract", "load": "side,retry", "t0": "", "t1": "t0", "t2": "t1"}},
	} {
		got := after(c.f)
		for id, w := range c.want {
			if got[id] != w {
				t.Errorf("%s: %s after %q, want %q", c.name, id, got[id], w)
			}
		}
	}
}

// Every demo workflow builds the same tree every time, each node once, steps flat under their Steps.
func TestNodeTreeOfEveryDemoWorkflow(t *testing.T) {
	for _, name := range demoNames() {
		wf := demoWorkflow(t, name)
		out := BuildNodeOutline(wf, OutlineOptions{})
		placedOnce(t, wf, out)
		if len(out.Dangling) > 0 || len(out.Unreachable) > 0 {
			t.Errorf("%s: ungrouped rows: %+v %+v", name, out.Dangling, out.Unreachable)
		}
		for _, row := range out.Rows {
			if row.Type != "Steps" {
				continue
			}
			for _, c := range row.Children {
				if len(c.Children) > 0 {
					t.Errorf("%s: step %s has children", name, c.DisplayName)
				}
			}
		}
		first := shape(out)
		for i := 0; i < 20; i++ {
			if got := shape(BuildNodeOutline(wf, OutlineOptions{})); got != first {
				t.Fatalf("%s: build %d differs:\n%s\nvs\n%s", name, i, got, first)
			}
		}
	}
}

// Each layer of the demo's multi-layer deploy is a task of the outer DAG that waits for the plan.
func TestNodeTreeOfTheDemoDeploy(t *testing.T) {
	out := BuildNodeOutline(demoWorkflow(t, "demo-deploy-multi-layer"), OutlineOptions{})
	var layers []string
	for _, c := range out.Rows[0].Children {
		layers = append(layers, c.DisplayName+"<-"+strings.Join(c.Deps, ","))
	}
	if got := strings.Join(layers, " "); got != "plan<- network<-plan database<-plan compute<-plan edge<-plan" {
		t.Fatalf("layers = %s", got)
	}
}

// Trees of thousands of nodes build in time and place every node once.
func TestNodeTreeAtScale(t *testing.T) {
	for name, wf := range map[string]core.Workflow{
		"dag":    syntheticWideDAG("scale", 5000),
		"steps":  syntheticStepsChain("scale", 5000),
		"nested": syntheticNestedDAGs("scale", 2000),
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			out := BuildNodeOutline(wf, OutlineOptions{})
			if d := time.Since(start); d > 5*time.Second {
				t.Fatalf("build took %v", d)
			}
			placedOnce(t, wf, out)
		})
	}
}

// syntheticStepsChain is n steps in groups of two, each step listing the next group.
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

// syntheticNestedDAGs is depth DAGs, each the only task of the one above, over one pod.
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

// syntheticWideDAG is n tasks under one root, every second listing the task two after it.
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
		if i%2 == 0 && i+2 < n {
			nd.Children = []string{fmt.Sprintf("%s-task-%04d", prefix, i+2)}
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
