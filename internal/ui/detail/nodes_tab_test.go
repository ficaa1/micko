package detail

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// gateWorkflow has a few nodes that ran, a skipped branch and a Suspend node holding everything up.
func gateWorkflow() core.Workflow {
	f := newTreeFixture("deploy").
		node("root", "deploy", "deploy", "Steps", "Running", "", 0, "plan", "skip-a", "approval").
		node("plan", "deploy.plan-prd", "plan-prd", "Pod", "Succeeded", "root", 0).
		node("skip-a", "plan-dev", "plan-dev", "Skipped", "Skipped", "root", -1, "skip-b").
		node("skip-b", "apply-dev", "apply-dev", "Skipped", "Skipped", "skip-a", -1).
		node("approval", "approval", "approval", "Steps", "Running", "root", 1, "suspend").
		node("suspend", "suspend", "suspend", "Suspend", "Running", "approval", 1).
		with("plan", func(n *core.Node) { n.PodName = "deploy-terraform-1234" })
	wf := f.workflow()
	wf.Summary.Suspended = true
	return wf
}

// runningUnderSkippedWorkflow has a Skipped step whose subtree still ran, as Argo does.
func runningUnderSkippedWorkflow() core.Workflow {
	return newTreeFixture("deploy").
		node("root", "deploy", "deploy", "Steps", "Running", "", 0, "skipped-parent", "dead-branch").
		node("skipped-parent", "plan-backend-tst", "plan-backend-tst", "Skipped", "Skipped", "root", -1, "approval").
		node("approval", "approval", "approval", "Steps", "Succeeded", "skipped-parent", 0, "apply-int").
		node("apply-int", "apply-int", "apply-int", "Pod", "Running", "approval", 1).
		node("dead-branch", "plan-dev", "plan-dev", "Skipped", "Skipped", "root", -1, "dead-child").
		node("dead-child", "apply-dev", "apply-dev", "Skipped", "Skipped", "dead-branch", -1).
		workflow()
}

// Skipped subtrees in which nothing ran are hidden and counted until h; the path to work that ran stays.
func TestHideSkipped(t *testing.T) {
	for _, c := range []struct {
		name   string
		wf     core.Workflow
		hidden int
		shown  string
	}{
		{"a skipped branch", gateWorkflow(), 2, "deploy plan-prd approval suspend"},
		{"running work under a skipped step", runningUnderSkippedWorkflow(), 2, "deploy plan-backend-tst approval apply-int"},
		{"the demo deploy", demoWorkflow(t, "demo-deploy-multi-layer"), 16,
			"demo-deploy-multi-layer plan network apply-eu-west database apply-eu-west compute apply-eu-west edge apply-eu-west"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := workflowModel(c.wf, 120, 40)
			if got := strings.Join(rowNames(m), " "); got != c.shown {
				t.Errorf("rows = %s, want %s", got, c.shown)
			}
			want := strconv.Itoa(c.hidden) + " skipped hidden"
			if s := m.tabStatusLine(); !strings.Contains(s, want) || strings.Contains(s, "(h ") {
				t.Errorf("status %q, want %q and no key the footer already shows", s, want)
			}
			shown := len(m.nodes)
			press(m, "h")
			if len(m.nodes) != shown+c.hidden || strings.Contains(m.tabStatusLine(), "skipped") {
				t.Errorf("after h: %d rows, status %q", len(m.nodes), m.tabStatusLine())
			}
		})
	}
}

// space folds the subtree under the cursor and opens it again, and the status line counts the folds.
func TestSpaceFoldsAndUnfolds(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	before := len(m.nodes)

	press(m, "space")
	r, _ := m.cursorRow()
	if !r.Folded || r.FoldedCount != 3 || r.Row.DisplayName != "transform" || len(m.nodes) != before-3 {
		t.Fatalf("after space: %+v, %d rows", r, len(m.nodes))
	}
	if !strings.Contains(body(m), "├▸ ✗ transform Retry ↻ 2 +3") {
		t.Errorf("folded row not drawn with its count:\n%s", body(m))
	}
	if !strings.Contains(m.tabStatusLine(), "3 in 1 fold") {
		t.Errorf("status %q does not state the fold", m.tabStatusLine())
	}

	press(m, "space")
	r, _ = m.cursorRow()
	if r.Folded || len(m.nodes) != before || !strings.Contains(r.Indent, markerOpen) {
		t.Fatalf("after second space: %+v, %d rows", r, len(m.nodes))
	}
	if strings.Contains(m.tabStatusLine(), "fold") {
		t.Errorf("status %q still mentions a fold", m.tabStatusLine())
	}
}

// left folds an open row or climbs; right opens a folded row or steps into it; a leaf does not fold.
func TestLeftAndRightWalkTheTree(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform(1)")
	at := func() FlatRow { r, _ := m.cursorRow(); return r }
	for _, step := range []struct {
		key    string
		name   string
		folded bool
	}{
		{"space", "transform(1)", false},
		{"left", "transform", false},
		{"left", "transform", true},
		{"left", "demo-nightly-report", false},
	} {
		press(m, step.key)
		if r := at(); r.Row.DisplayName != step.name || r.Folded != step.folded {
			t.Fatalf("%s: on %q folded %v, want %q folded %v", step.key, r.Row.DisplayName, r.Folded, step.name, step.folded)
		}
	}
	cursorTo(t, m, "transform")
	for _, step := range []struct{ key, name string }{
		{"right", "transform"}, {"right", "transform(0)"}, {"right", "transform(0)"},
	} {
		press(m, step.key)
		if r := at(); r.Row.DisplayName != step.name || r.Folded {
			t.Fatalf("%s: on %q folded %v, want %q open", step.key, r.Row.DisplayName, r.Folded, step.name)
		}
	}
}

// A refresh keeps the folds and the cursor's node; another workflow starts unfolded.
func TestFoldsSurviveARefresh(t *testing.T) {
	wf := demoWorkflow(t, "demo-nightly-report")
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	press(m, "space")
	cursorTo(t, m, "load")

	next := copyWorkflow(wf)
	early := testkit.FixtureEpoch.Add(-10 * time.Hour)
	next.Nodes["early"] = core.Node{ID: "early", Name: "early", DisplayName: "early", Type: "Pod", Phase: "Succeeded", StartedAt: &early}
	m.SetWorkflow(next, testkit.FixtureEpoch)

	if r, _ := m.cursorRow(); r.Row.DisplayName != "load" {
		t.Errorf("cursor moved to %q on refresh", r.Row.DisplayName)
	}
	if !strings.Contains(body(m), "├▸ ✗ transform") {
		t.Errorf("the fold did not survive the refresh:\n%s", body(m))
	}

	other := copyWorkflow(wf)
	other.Summary.Ref.UID = "another-run"
	m.SetWorkflow(other, testkit.FixtureEpoch)
	if strings.Contains(body(m), "▸") {
		t.Errorf("another workflow kept the fold:\n%s", body(m))
	}
}

// retryOfSteps is a DAG whose task retries a steps template, each attempt with a pod of its own.
func retryOfSteps(attempts int) core.Workflow {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "r").
		node("r", "wf.job", "job", "Retry", "Running", "wf", 0)
	var kids []string
	for i := 0; i < attempts; i++ {
		id, n := "a"+strconv.Itoa(i), strconv.Itoa(i)
		kids = append(kids, id)
		phase := "Failed"
		if i == attempts-1 {
			phase = "Running"
		}
		f.node(id, "wf.job("+n+")", "job("+n+")", "Steps", phase, "wf", i, id+"-pod")
		f.node(id+"-pod", "wf.job("+n+").pod", "pod", "Pod", phase, id, i)
		f.with(id, func(n *core.Node) { n.Retried = true })
		if phase != "Running" {
			end := testkit.FixtureEpoch.Add(time.Duration(i)*time.Minute + 30*time.Second)
			f.with(id, func(n *core.Node) { n.FinishedAt = &end })
		}
	}
	f.with("r", func(n *core.Node) { n.Children = kids })
	return f.workflow()
}

// Finished attempts but the last start folded, once per node: one the reader opens stays open.
func TestOlderRetryAttemptsStartFolded(t *testing.T) {
	m := workflowModel(retryOfSteps(3), 120, 30)
	if !m.folded["a0"] || !m.folded["a1"] || m.folded["a2"] {
		t.Fatalf("default folds = %v, want a0 and a1", m.folded)
	}

	cursorTo(t, m, "job(0)")
	press(m, "space")
	m.SetWorkflow(retryOfSteps(4), testkit.FixtureEpoch)
	if m.folded["a0"] {
		t.Error("a refresh folded an attempt the reader opened")
	}
	if !m.folded["a2"] || m.folded["a3"] {
		t.Errorf("after a new attempt, folds = %v, want a2 folded and a3 open", m.folded)
	}
}

// i opens the info panel beside a wide tree, under a narrower one, or not at all; i closes it.
func TestInfoPanelPlacement(t *testing.T) {
	for _, c := range []struct {
		width int
		place string
	}{{160, "│ NODE extract"}, {136, "│ NODE extract"}, {120, "─ NODE extract"}, {76, "─ NODE extract"}, {56, "─ NODE extract"}, {50, ""}} {
		m := demoModel(t, "demo-nightly-report", c.width, 30)
		cursorTo(t, m, "extract")
		press(m, "i")
		b := body(m)
		switch {
		case c.place == "" && (strings.Contains(b, "NODE extract") || !strings.Contains(m.tabStatusLine(), "no room for info")):
			t.Errorf("width %d: the panel is drawn or the status %q does not say why:\n%s", c.width, m.tabStatusLine(), b)
		case c.place != "" && !strings.Contains(b, c.place):
			t.Errorf("width %d: no %q panel:\n%s", c.width, c.place, b)
		}
		press(m, "i")
		if strings.Contains(body(m), "NODE extract") {
			t.Errorf("width %d: i did not close the panel", c.width)
		}
	}
}

// The info panel states the node's run facts, with the resource tab's marker for a hidden value.
func TestInfoPanelFacts(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 160, 40)
	m.SetRedactByDefault(true)
	cursorTo(t, m, "extract")
	press(m, "i")
	b := body(m)
	for _, want := range []string{
		"name      extract", "type      Pod", "template  extract", "phase     ✓ Succeeded",
		"started", "finished", "duration  50s", "progress  1/1", "pod       demo-nightly-report-extract-",
		"host      demo-worker-", "exit code 0", "cpu       13s × 1 cpu", "memory    2m31s × 100Mi",
		"inputs    date = " + redactedMarker, "source = " + redactedMarker,
		"outputs   rows = " + redactedMarker, "artifact raw-extract", "values redacted (v reveals)",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("panel lacks %q:\n%s", want, b)
		}
	}
}

// The panel of a retry attempt, the exit handler and the gate carry their flags and state.
func TestInfoPanelFlags(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 160, 40)
	cursorTo(t, m, "transform(1)")
	press(m, "i")
	if b := body(m); !strings.Contains(b, "flags     retried") || !strings.Contains(b, "exit code 1") {
		t.Errorf("attempt panel:\n%s", b)
	}
	cursorTo(t, m, "onExit")
	if b := body(m); !strings.Contains(b, "flags     hooked") || !strings.Contains(b, "type      Pod (exit handler)") {
		t.Errorf("exit handler panel:\n%s", b)
	}
	gate := demoModel(t, "demo-release-gate", 160, 40)
	cursorTo(t, gate, "approve-production")
	press(gate, "i")
	if b := body(gate); !strings.Contains(b, "AWAITING RESUME") || !strings.Contains(b, "duration  4m50s (running)") {
		t.Errorf("gate panel:\n%s", b)
	}
}

// While the find input is open every printable key types into it and acts on nothing else.
func TestFindInputOwnsPrintableKeys(t *testing.T) {
	m := demoModel(t, "demo-train-pipeline", 120, 30)
	hide, cursor, info := m.hideSkipped, m.nodeCursor, m.showInfo
	press(m, "/")
	if !m.TextEntry() || !m.EscapeConsumed() {
		t.Fatal("/ did not open the find input")
	}
	for _, k := range []string{"q", "h", "i", "j", "space", "s", "p", "n", "N"} {
		press(m, k)
	}
	if m.find.query != "qhij spnN" {
		t.Errorf("query = %q", m.find.query)
	}
	if m.hideSkipped != hide || m.nodeCursor != cursor || m.showInfo != info || m.nodeSort != NodeSortPipeline || m.nodePhase != NodePhaseAll {
		t.Error("a key typed into the find input also acted on the tab")
	}
	if m.Hints() != "enter jump  esc cancel" {
		t.Errorf("hints while typing = %q", m.Hints())
	}
	press(m, "backspace")
	if m.find.query != "qhij spn" {
		t.Errorf("backspace left %q", m.find.query)
	}
	press(m, "esc")
	if m.TextEntry() || m.EscapeConsumed() || m.find.query != "" {
		t.Errorf("esc did not cancel the find: %+v", m.find)
	}
}

// enter, n and N step through the counted matches; esc clears the find before it leaves.
func TestFindJumpsBetweenMatches(t *testing.T) {
	m := demoModel(t, "demo-train-pipeline", 120, 30)
	press(m, "/")
	typeText(m, "SHARD")
	if !strings.Contains(m.tabStatusLine(), "6 matches") {
		t.Errorf("live count missing: %q", m.tabStatusLine())
	}
	press(m, "enter")
	if m.TextEntry() {
		t.Fatal("enter left the input open")
	}
	for _, step := range []struct{ keys, row, status string }{
		{"", "train-shard(0:0)", "match 1/6"},
		{"nn", "train-shard(2:2)", "match 3/6"},
		{"NNN", "train-shard(5:5)", "match 6/6"},
	} {
		typeText(m, step.keys)
		if r, _ := m.cursorRow(); r.Row.DisplayName != step.row || !strings.Contains(m.tabStatusLine(), step.status) {
			t.Errorf("after %q: on %q, status %q", step.keys, r.Row.DisplayName, m.tabStatusLine())
		}
	}
	if !strings.HasPrefix(m.Hints(), "n next  N previous  esc clear find") {
		t.Errorf("hints during a find = %q", m.Hints())
	}
	if cmd := press(m, "esc"); cmd != nil {
		t.Fatal("esc left the workflow while a find was active")
	}
	if m.find.active() || strings.Contains(m.tabStatusLine(), "match") {
		t.Errorf("esc did not clear the find: %q", m.tabStatusLine())
	}
	if cmd := press(m, "esc"); cmd == nil {
		t.Fatal("esc without a find does not go back")
	} else if _, ok := cmd().(BackMsg); !ok {
		t.Fatalf("esc emits %T, want BackMsg", cmd())
	}
}

// Jumping to a match opens the folds above it.
func TestFindOpensFoldsAboveAMatch(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	press(m, "space")
	cursorTo(t, m, "demo-nightly-report")
	press(m, "space")
	press(m, "/")
	typeText(m, "transform(1")
	press(m, "enter")
	if r, _ := m.cursorRow(); r.Row.DisplayName != "transform(1)" {
		t.Fatalf("cursor on %q, rows %v", r.Row.DisplayName, rowNames(m))
	}
	if len(m.folded) != 0 {
		t.Errorf("folds above the match stayed: %v", m.folded)
	}
}

// A name only hidden skipped nodes carry is not jumped to, but the status says where it is.
func TestFindCountsMatchesAmongHiddenSkipped(t *testing.T) {
	m := demoModel(t, "demo-deploy-multi-layer", 120, 30)
	press(m, "/")
	typeText(m, "us-east")
	press(m, "enter")
	if s := m.tabStatusLine(); !strings.Contains(s, `no match for "us-east" (4 among hidden skipped)`) {
		t.Errorf("status %q", s)
	}
	press(m, "h")
	if s := m.tabStatusLine(); !strings.Contains(s, "match 1/4") {
		t.Errorf("after h: %q", s)
	}
}

// The footer names the tab's keys and what h would change.
func TestNodesHints(t *testing.T) {
	m := demoModel(t, "demo-deploy-multi-layer", 120, 30)
	for _, key := range []string{"h show 16 skipped", "space fold", "i info", "/ find", "s sort", "p phase", "l logs"} {
		if !strings.Contains(m.Hints(), key) {
			t.Errorf("hints %q lack %q", m.Hints(), key)
		}
	}
	press(m, "h")
	if h := m.Hints(); !strings.Contains(h, "h hide skipped") {
		t.Errorf("hints after h: %q", h)
	}
}

// p narrows the tab to one phase, Failed first, says so, and comes back round to the whole tree.
func TestNodePhaseFilter(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 20)
	all := len(m.nodes)
	press(m, "p")
	if got := strings.Join(rowNames(m), " "); got != "demo-nightly-report transform transform(0) transform(1) transform(2) load" {
		t.Errorf("Failed rows = %s", got)
	}
	if !strings.Contains(m.tabStatusLine(), "phase Failed") {
		t.Errorf("status %q does not name the filter", m.tabStatusLine())
	}
	press(m, "p")
	if b := body(m); !strings.Contains(b, "(no Running nodes — p changes the filter)") || strings.Contains(b, "not started") {
		t.Errorf("empty filter body:\n%s", b)
	}
	for i := 0; i < len(nodePhaseCycle)-2; i++ {
		press(m, "p")
	}
	if m.nodePhase != NodePhaseAll || len(m.nodes) != all {
		t.Fatalf("rotation ends at %q with %d rows, want All with %d", m.nodePhase, len(m.nodes), all)
	}
}

// sortWorkflow gives one parent four children whose name, phase and start order all disagree.
func sortWorkflow() core.Workflow {
	return newTreeFixture("deploy").
		node("root", "deploy", "deploy", "Steps", "Running", "", 0, "c-zulu", "c-alpha", "c-mike", "c-bravo").
		node("c-zulu", "zulu", "zulu", "Pod", "Succeeded", "root", 1).
		node("c-alpha", "alpha", "alpha", "Pod", "Running", "root", 2).
		node("c-mike", "mike", "mike", "Pod", "Failed", "root", 3).
		node("c-bravo", "bravo", "bravo", "Pod", "Pending", "root", 4).
		workflow()
}

// s cycles the sibling order, keeps the cursor on its node, and returns to the tree's own order.
func TestNodeSort(t *testing.T) {
	m := workflowModel(sortWorkflow(), 120, 30)
	pipeline := strings.Join(rowNames(m), " ")
	cursorTo(t, m, "mike")
	for _, step := range []struct{ sort, rows string }{
		{"started", "deploy zulu alpha mike bravo"},
		{"name", "deploy alpha bravo mike zulu"},
		{"phase", "deploy mike alpha bravo zulu"},
		{"pipeline", pipeline},
	} {
		press(m, "s")
		if got := strings.Join(rowNames(m), " "); got != step.rows {
			t.Errorf("sort %s: rows %s, want %s", step.sort, got, step.rows)
		}
		if !strings.Contains(m.tabStatusLine(), "sort "+step.sort) {
			t.Errorf("status %q does not name sort %s", m.tabStatusLine(), step.sort)
		}
		if r, _ := m.cursorRow(); r.Row.DisplayName != "mike" {
			t.Errorf("sort %s moved the cursor to %q", step.sort, r.Row.DisplayName)
		}
	}
}

// l and enter open the logs of the selected node, or of the failing pod on Explain, when it has a pod.
func TestLogsKey(t *testing.T) {
	for _, c := range []struct {
		name    string
		model   func(t *testing.T) *Model
		key     string
		wantPod string
	}{
		{"a pod on the nodes tab", func(t *testing.T) *Model {
			m := workflowModel(gateWorkflow(), 120, 20)
			cursorTo(t, m, "plan-prd")
			return m
		}, "l", "deploy-terraform-1234"},
		{"enter on a pod", func(t *testing.T) *Model {
			m := workflowModel(gateWorkflow(), 120, 20)
			cursorTo(t, m, "plan-prd")
			return m
		}, "enter", "deploy-terraform-1234"},
		{"a Suspend node", func(t *testing.T) *Model {
			m := workflowModel(gateWorkflow(), 120, 20)
			cursorTo(t, m, "suspend")
			return m
		}, "l", ""},
		{"a pod on the timeline", func(t *testing.T) *Model {
			m := sectionModel(t, "demo-nightly-report", "timeline", 120, 30)
			press(m, "j")
			return m
		}, "l", "demo-nightly-report-extract-"},
		{"the failing pod on Explain", func(t *testing.T) *Model {
			return sectionModel(t, "demo-nightly-report", "explain", 120, 30)
		}, "l", "demo-nightly-report-transform-"},
		{"Explain with nothing failed", func(t *testing.T) *Model {
			return sectionModel(t, "demo-release-gate", "explain", 120, 30)
		}, "l", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := press(c.model(t), c.key)
			if c.wantPod == "" {
				if cmd != nil {
					t.Fatalf("%s emitted %#v", c.key, cmd())
				}
				return
			}
			if cmd == nil {
				t.Fatalf("%s emitted nothing", c.key)
			}
			if in, ok := cmd().(NodeLogsIntent); !ok || !strings.HasPrefix(in.PodName, c.wantPod) {
				t.Fatalf("%s emitted %#v, want pod %s", c.key, cmd(), c.wantPod)
			}
		})
	}
}

// gg is two presses: a single g neither jumps nor swallows the next key.
func TestGGPrefixDoesNotSwallowTheNextKey(t *testing.T) {
	m := workflowModel(gateWorkflow(), 120, 20)
	typeText(m, "jj")
	press(m, "g")
	if m.nodeCursor != 2 {
		t.Fatalf("a single g moved the cursor to %d", m.nodeCursor)
	}
	press(m, "g")
	if m.nodeCursor != 0 {
		t.Fatalf("gg left the cursor at %d, want 0", m.nodeCursor)
	}
	typeText(m, "jgj")
	if m.nodeCursor != 2 {
		t.Fatalf("j g j left the cursor at %d, want 2", m.nodeCursor)
	}
}

// The header, heads and status line keep their rows while there is room, and G shows the last row.
func TestNodesTabKeepsItsChrome(t *testing.T) {
	for _, h := range []int{12, 30} {
		for _, info := range []bool{false, true} {
			m := demoModel(t, "demo-deploy-multi-layer", 100, h)
			press(m, "h")
			if info {
				press(m, "i")
			}
			press(m, "G")
			b := body(m)
			want := []string{"[Nodes]", "progress 5/5", "26 nodes", "apply-eu-west"}
			if !info || h >= 30 {
				want = append(want, "NAME")
			}
			for _, w := range want {
				if !strings.Contains(b, w) {
					t.Errorf("height %d info %v: body lacks %q:\n%s", h, info, w, b)
				}
			}
		}
	}
}

// The raw view is every row at its natural width in plain text, with ASCII bars in any skin.
func TestNodesRawLines(t *testing.T) {
	m := demoModel(t, "demo-train-pipeline", 60, 10)
	m.SetTheme(skin(t))
	lines := m.RawLines()
	if len(lines) != len(m.nodes)+2 {
		t.Fatalf("%d raw lines for %d rows", len(lines), len(m.nodes))
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "\x1b") {
		t.Error("raw lines carry escapes")
	}
	for _, want := range []string{"progress 3/8", "TEMPLATE", "TIMELINE", "evaluate ← train-shard(0:0), train-shard(1:1) +4", "=>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("raw lines lack %q:\n%s", want, joined)
		}
	}
}

// The nodes tab at 140 and 80 columns, with the info panel where each width puts it.
func TestNodesTabGolden(t *testing.T) {
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range []string{
			"demo-release-gate", "demo-nightly-report", "demo-train-pipeline",
			"demo-data-pull", "demo-deploy-multi-layer",
		} {
			m := demoModel(t, name, width, 24)
			b.WriteString("== " + name + "\n" + strings.Join(m.BodyLines(), "\n") + "\n")
		}
		m := demoModel(t, "demo-nightly-report", width, 30)
		cursorTo(t, m, "extract")
		press(m, "i")
		b.WriteString("== demo-nightly-report, info on extract\n" + strings.Join(m.BodyLines(), "\n") + "\n")
		golden(t, "nodes-"+strconv.Itoa(width), b.String())
	}
}
