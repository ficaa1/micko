package detail

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// treeFixture assembles a node map the way the controller writes it:
// full names, boundaries, children lists, start times in minutes after a
// fixed epoch (-1 for a node that has not started).
type treeFixture struct {
	name  string
	nodes map[string]core.Node
}

func newTreeFixture(name string) *treeFixture {
	return &treeFixture{name: name, nodes: map[string]core.Node{}}
}

func (f *treeFixture) node(id, name, display, typ, phase, boundary string, startMin int, children ...string) *treeFixture {
	n := core.Node{
		ID: id, Name: name, DisplayName: display, Type: typ, Phase: phase,
		BoundaryID: boundary, Children: children,
	}
	if startMin >= 0 {
		at := testkit.FixtureEpoch.Add(time.Duration(startMin) * time.Minute)
		n.StartedAt = &at
	}
	f.nodes[id] = n
	return f
}

func (f *treeFixture) with(id string, edit func(*core.Node)) *treeFixture {
	n := f.nodes[id]
	edit(&n)
	f.nodes[id] = n
	return f
}

func (f *treeFixture) workflow() core.Workflow {
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: f.name, UID: "u-" + f.name}, Phase: "Running"},
		Nodes:          f.nodes,
		NodesAvailable: true,
	}
}

// shape draws the tree as indented display names with their dependency
// annotations, which is what a reader sees of the structure.
func shape(out Outline) string {
	var b strings.Builder
	var walk func(rows []OutlineRow, depth int)
	walk = func(rows []OutlineRow, depth int) {
		for _, r := range rows {
			b.WriteString(strings.Repeat("  ", depth) + r.DisplayName)
			if len(r.Deps) > 0 {
				b.WriteString(" <- " + strings.Join(r.Deps, ","))
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

// placedOnce checks the build's coverage guarantee: every node is drawn once,
// or listed in an ungrouped section, or is a StepGroup drawn as its steps.
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

// A steps template chains each group under every step of the group before.
// The tree drops the StepGroups and lists the steps in group order, so a
// four-group pipeline is four rows under its Steps node, not a staircase.
func TestDisplayTreeFlattensAStepsChain(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
		node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "build").
		node("build", "wf[0].build", "build", "Pod", "Succeeded", "wf", 0, "g1").
		node("g1", "wf[1]", "[1]", "StepGroup", "Succeeded", "wf", 3, "test-b", "test-a").
		node("test-b", "wf[1].test-b", "test-b", "Pod", "Succeeded", "wf", 3, "g2").
		node("test-a", "wf[1].test-a", "test-a", "Pod", "Succeeded", "wf", 3, "g2").
		node("g2", "wf[2]", "[2]", "StepGroup", "Running", "wf", 7, "approve").
		node("approve", "wf[2].approve", "approve", "Suspend", "Running", "wf", 7, "g3").
		node("g3", "wf[3]", "[3]", "StepGroup", "Pending", "wf", -1)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	// Inside a group the steps keep the template's order (test-b is listed
	// first), not the alphabet.
	want := "wf\n  build\n  test-b\n  test-a\n  approve\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	placedOnce(t, wf, out)
	if out.StepGroups != 4 {
		t.Errorf("StepGroups = %d, want 4", out.StepGroups)
	}
}

// A step that runs a steps template of its own nests that template's steps
// under it, and the next outer group, which the controller hangs off the
// inner template's last step, still lands under the outer Steps node.
func TestDisplayTreeNestsAStepsTemplateCalledFromAStep(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
		node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "inner").
		node("inner", "wf[0].inner", "inner", "Steps", "Succeeded", "wf", 0, "i0").
		node("i0", "wf[0].inner[0]", "[0]", "StepGroup", "Succeeded", "inner", 0, "leaf").
		node("leaf", "wf[0].inner[0].leaf", "leaf", "Pod", "Succeeded", "inner", 0, "g1").
		node("g1", "wf[1]", "[1]", "StepGroup", "Running", "wf", 2, "after").
		node("after", "wf[1].after", "after", "Pod", "Running", "wf", 2)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	want := "wf\n  inner\n    leaf\n  after\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	placedOnce(t, wf, out)
}

// A DAG task lists its dependents as children. The tree lists the tasks under
// their DAG in dependency order and names each task's dependencies, so a
// diamond's join is one row that says what it waits for.
func TestDisplayTreeListsDAGTasksInDependencyOrder(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "a").
		node("a", "wf.a", "a", "Pod", "Succeeded", "wf", 0, "c", "b").
		// c started before b, so it comes first among the free tasks.
		node("b", "wf.b", "b", "Pod", "Succeeded", "wf", 3, "d").
		node("c", "wf.c", "c", "Pod", "Succeeded", "wf", 2, "d").
		node("d", "wf.d", "d", "Pod", "Running", "wf", 5)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	want := "wf\n  a\n  c <- a\n  b <- a\n  d <- c,b\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	placedOnce(t, wf, out)
}

// Dependency order holds even where start times disagree: a task that has
// not started but waits on nothing comes before the tasks that wait on it,
// and a join that has not started comes after the tasks it waits for.
func TestDisplayTreeDependencyOrderBeatsStartTime(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "late-root", "early").
		node("late-root", "wf.late-root", "late-root", "Pod", "Pending", "wf", -1, "join").
		node("early", "wf.early", "early", "Pod", "Succeeded", "wf", 0, "join").
		node("join", "wf.join", "join", "Pod", "Pending", "wf", -1)
	out := BuildNodeOutline(f.workflow(), OutlineOptions{})
	want := "wf\n  early\n  late-root\n  join <- early,late-root\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
}

// A DAG in the data can still hold a dependency cycle. The order must place
// every task once and terminate.
func TestDisplayTreeSurvivesADependencyCycle(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "a").
		node("a", "wf.a", "a", "Pod", "Running", "wf", 0, "b").
		node("b", "wf.b", "b", "Pod", "Running", "wf", 1, "a")
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	placedOnce(t, wf, out)
	if got := len(out.Rows[0].Children); got != 2 {
		t.Fatalf("DAG children = %d, want 2", got)
	}
}

// Parallel tasks that start together order by name with numbers read as
// numbers, so shard(10) follows shard(9) rather than shard(1).
func TestDisplayTreeOrdersTiesNaturally(t *testing.T) {
	f := newTreeFixture("wf").node("wf", "wf", "wf", "DAG", "Running", "", 0)
	var kids []string
	for _, i := range []int{10, 2, 1, 9} {
		id := fmt.Sprintf("id-%d", 100-i)
		kids = append(kids, id)
		f.node(id, fmt.Sprintf("wf.shard(%d)", i), fmt.Sprintf("shard(%d)", i), "Pod", "Running", "wf", 1)
	}
	f.with("wf", func(n *core.Node) { n.Children = kids })
	out := BuildNodeOutline(f.workflow(), OutlineOptions{})
	want := "wf\n  shard(1)\n  shard(2)\n  shard(9)\n  shard(10)\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
}

// A retried task nests its attempts under the Retry node, in the order they
// ran, and counts the retries. The task that waits for it hangs off the last
// attempt in the controller's data, but it is drawn under the DAG with the
// Retry node as its dependency.
func TestDisplayTreeNestsRetryAttempts(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Failed", "", 0, "extract").
		node("extract", "wf.extract", "extract", "Pod", "Succeeded", "wf", 0, "retry").
		node("retry", "wf.transform", "transform", "Retry", "Failed", "wf", 1, "t0", "t1", "t2").
		node("t0", "wf.transform(0)", "transform(0)", "Pod", "Failed", "wf", 1).
		node("t1", "wf.transform(1)", "transform(1)", "Pod", "Failed", "wf", 2).
		node("t2", "wf.transform(2)", "transform(2)", "Pod", "Failed", "wf", 3, "load").
		node("load", "wf.load", "load", "Pod", "Omitted", "wf", -1)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	want := "wf\n  extract\n  transform <- extract\n    transform(0)\n    transform(1)\n    transform(2)\n  load <- transform\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	if got := out.Rows[0].Children[1].Retries; got != 2 {
		t.Errorf("Retries = %d, want 2", got)
	}
	placedOnce(t, wf, out)
}

// Attempts are recognised by the retried flag as well as by name, and a
// dependent listed on the Retry node itself is not mistaken for an attempt.
func TestDisplayTreeTellsAttemptsFromDependents(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Failed", "", 0, "retry").
		node("retry", "wf.flaky", "flaky", "Retry", "Failed", "wf", 0, "first", "next").
		node("first", "wf.flaky-attempt", "attempt", "Pod", "Failed", "wf", 0).
		node("next", "wf.next", "next", "Pod", "Omitted", "wf", -1).
		with("first", func(n *core.Node) { n.Retried = true })
	out := BuildNodeOutline(f.workflow(), OutlineOptions{})
	want := "wf\n  flaky\n    attempt\n  next <- flaky\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
}

// A loop's items sit under their TaskGroup, and a task waiting for the loop
// depends on the TaskGroup, whichever item the controller linked it from.
func TestDisplayTreeGroupsLoopItemsUnderTheirTaskGroup(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "prep").
		node("prep", "wf.prep", "prep", "Pod", "Succeeded", "wf", 0, "fan").
		node("fan", "wf.fan", "fan", "TaskGroup", "Running", "wf", 1, "fan1", "fan0").
		node("fan0", "wf.fan(0:a)", "fan(0:a)", "Pod", "Succeeded", "wf", 1, "join").
		node("fan1", "wf.fan(1:b)", "fan(1:b)", "Pod", "Running", "wf", 1, "join").
		node("join", "wf.join", "join", "Pod", "Pending", "wf", -1)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	// Items keep the order the controller created them in.
	want := "wf\n  prep\n  fan <- prep\n    fan(1:b)\n    fan(0:a)\n  join <- fan\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	placedOnce(t, wf, out)
}

// The exit handler is its own top-level tree after the workflow's, labelled
// with its role; the workflow's own tree comes first even when a stray
// top-level node started earlier.
func TestDisplayTreeKeepsTheExitHandlerApart(t *testing.T) {
	f := newTreeFixture("wf").
		node("stray", "stray", "stray", "Pod", "Succeeded", "", 0).
		node("wf", "wf", "wf", "Steps", "Succeeded", "", 1).
		node("exit", "wf.onExit", "wf.onExit", "Pod", "Succeeded", "", 9).
		with("exit", func(n *core.Node) { n.Hooked = true })
	out := BuildNodeOutline(f.workflow(), OutlineOptions{})
	var got []string
	for _, r := range out.Rows {
		got = append(got, r.NodeID+":"+r.Role)
	}
	want := "wf: exit:exit handler stray:"
	if strings.Join(got, " ") != want {
		t.Fatalf("roots = %v, want %s", got, want)
	}
}

// The demo's multi-layer deploy is DAGs inside a DAG, and most of it was
// skipped. Each layer is a task of the outer DAG that waits for the plan,
// its region tasks sit under it, and hiding skipped rows leaves exactly the
// one region that ran in each layer.
func TestDisplayTreeOfTheDemoDeploy(t *testing.T) {
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	var wf core.Workflow
	for ref, w := range r.Workflows {
		if ref.Name == "demo-deploy-multi-layer" {
			wf = w
		}
	}
	out := BuildNodeOutline(wf, OutlineOptions{})
	placedOnce(t, wf, out)
	root := out.Rows[0]
	var layers []string
	for _, c := range root.Children {
		layers = append(layers, c.DisplayName+"<-"+strings.Join(c.Deps, ","))
	}
	want := "plan<- network<-plan database<-plan compute<-plan edge<-plan"
	if strings.Join(layers, " ") != want {
		t.Fatalf("layers = %v, want %s", layers, want)
	}
	rows, hidden := FlattenOutline(out, true)
	if hidden != 16 {
		t.Errorf("hidden = %d, want the 16 skipped regions", hidden)
	}
	var shown []string
	for _, r := range rows {
		shown = append(shown, r.Row.DisplayName)
	}
	wantShown := "demo-deploy-multi-layer plan network apply-eu-west database apply-eu-west compute apply-eu-west edge apply-eu-west"
	if strings.Join(shown, " ") != wantShown {
		t.Fatalf("shown = %v", shown)
	}
}

// Every demo workflow places each node once, and the steps workflows are no
// deeper than their templates: every step sits directly under its Steps
// node.
func TestDisplayTreeOfEveryDemoWorkflow(t *testing.T) {
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	for ref, wf := range r.Workflows {
		out := BuildNodeOutline(wf, OutlineOptions{})
		placedOnce(t, wf, out)
		if len(out.Dangling) > 0 || len(out.Unreachable) > 0 {
			t.Errorf("%s: ungrouped rows in a well-formed workflow: %+v %+v", ref.Name, out.Dangling, out.Unreachable)
		}
		for _, row := range out.Rows {
			if row.Type != "Steps" {
				continue
			}
			for _, c := range row.Children {
				if len(c.Children) > 0 {
					t.Errorf("%s: step %s has children %v", ref.Name, c.DisplayName, c.Children)
				}
			}
		}
	}
}

// A node whose boundary is missing from the map still appears: under the
// node that lists it when one does, at the top level otherwise. A StepGroup
// whose Steps node is missing is drawn itself, since there is nothing to
// fold it into.
func TestDisplayTreeMissingBoundary(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "listed").
		node("listed", "wf.listed", "listed", "Pod", "Running", "gone", 1).
		node("alone", "wf.alone", "alone", "Pod", "Running", "gone", 2).
		node("group", "wf[0]", "[0]", "StepGroup", "Running", "gone", 3, "step").
		node("step", "wf[0].step", "step", "Pod", "Running", "", 3)
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	want := "wf\n  listed\nalone\n[0]\n  step\n"
	if got := shape(out); got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	if out.StepGroups != 0 {
		t.Errorf("StepGroups = %d, want 0: the group has no Steps node to fold into", out.StepGroups)
	}
	placedOnce(t, wf, out)
}

// Boundaries that point at each other form an ownership cycle. The cut lands
// on the back edge, both nodes appear once, and a dangling child is listed
// in its own section.
func TestDisplayTreeCutsAnOwnershipCycle(t *testing.T) {
	f := newTreeFixture("wf").
		node("a", "wf.a", "a", "DAG", "Running", "b", 0, "ghost").
		node("b", "wf.b", "b", "DAG", "Running", "a", 1).
		node("self", "wf.self", "self", "Pod", "Running", "self", 2, "self")
	wf := f.workflow()
	out := BuildNodeOutline(wf, OutlineOptions{})
	placedOnce(t, wf, out)
	if len(out.Dangling) != 1 || out.Dangling[0].NodeID != "ghost" {
		t.Fatalf("dangling = %+v, want ghost", out.Dangling)
	}
}

// The build is a pure function of the node map: repeated builds, which
// range over the map in a different order each time, draw the same tree.
func TestDisplayTreeIsDeterministic(t *testing.T) {
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	for ref, wf := range r.Workflows {
		first := shape(BuildNodeOutline(wf, OutlineOptions{}))
		for i := 0; i < 20; i++ {
			if got := shape(BuildNodeOutline(wf, OutlineOptions{})); got != first {
				t.Fatalf("%s: build %d differs:\n%s\nvs\n%s", ref.Name, i, got, first)
			}
		}
	}
}

// A node ID is never a pod name: a pod node the server gave no pod name keeps
// an empty one, however pod-like its ID looks.
func TestDisplayTreeNeverDerivesAPodName(t *testing.T) {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "wf-1234").
		node("wf-1234", "wf.a", "a", "Pod", "Running", "wf", 0)
	out := BuildNodeOutline(f.workflow(), OutlineOptions{})
	if got := out.Rows[0].Children[0].PodName; got != "" {
		t.Fatalf("PodName = %q, want empty", got)
	}
}

// Each node names the siblings it waited for: a step the steps of the group
// before its own, a DAG task its dependencies, a retry attempt the attempt
// before it. The first group, a task with no dependencies and the first
// attempt waited for nothing but their parent.
func TestDisplayTreeRecordsWhatEachNodeWaitedFor(t *testing.T) {
	after := func(out Outline) map[string]string {
		m := map[string]string{}
		var walk func(rows []OutlineRow)
		walk = func(rows []OutlineRow) {
			for _, r := range rows {
				m[r.NodeID] = strings.Join(r.After, ",")
				walk(r.Children)
			}
		}
		walk(out.Rows)
		return m
	}
	steps := newTreeFixture("wf").
		node("wf", "wf", "wf", "Steps", "Running", "", 0, "g0").
		node("g0", "wf[0]", "[0]", "StepGroup", "Succeeded", "wf", 0, "build").
		node("build", "wf[0].build", "build", "Pod", "Succeeded", "wf", 0, "g1").
		node("g1", "wf[1]", "[1]", "StepGroup", "Succeeded", "wf", 3, "test-b", "test-a").
		node("test-b", "wf[1].test-b", "test-b", "Pod", "Succeeded", "wf", 3, "g2").
		node("test-a", "wf[1].test-a", "test-a", "Pod", "Succeeded", "wf", 3, "g2").
		node("g2", "wf[2]", "[2]", "StepGroup", "Running", "wf", 7, "approve").
		node("approve", "wf[2].approve", "approve", "Suspend", "Running", "wf", 7)
	got := after(BuildNodeOutline(steps.workflow(), OutlineOptions{}))
	want := map[string]string{"wf": "", "build": "", "test-b": "build", "test-a": "build", "approve": "test-b,test-a"}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("steps: %s after %q, want %q", id, got[id], w)
		}
	}

	dag := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Failed", "", 0, "extract").
		node("extract", "wf.extract", "extract", "Pod", "Succeeded", "wf", 0, "retry", "side").
		node("retry", "wf.transform", "transform", "Retry", "Failed", "wf", 1, "t0", "t1", "t2").
		node("t0", "wf.transform(0)", "transform(0)", "Pod", "Failed", "wf", 1).
		node("t1", "wf.transform(1)", "transform(1)", "Pod", "Failed", "wf", 2).
		node("t2", "wf.transform(2)", "transform(2)", "Pod", "Failed", "wf", 3, "load").
		node("side", "wf.side", "side", "Pod", "Succeeded", "wf", 1, "load").
		node("load", "wf.load", "load", "Pod", "Omitted", "wf", -1)
	got = after(BuildNodeOutline(dag.workflow(), OutlineOptions{}))
	want = map[string]string{
		"extract": "", "retry": "extract", "side": "extract", "load": "side,retry",
		"t0": "", "t1": "t0", "t2": "t1",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("dag: %s after %q, want %q", id, got[id], w)
		}
	}
}
