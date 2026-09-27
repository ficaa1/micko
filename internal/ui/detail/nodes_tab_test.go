package detail

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// cursorTo moves the cursor to the row with this display name.
func cursorTo(t *testing.T, m *Model, name string) FlatRow {
	t.Helper()
	for i, r := range m.nodes {
		if r.Row.DisplayName == name {
			m.nodeCursor = i
			return r
		}
	}
	t.Fatalf("no row %q among %d rows", name, len(m.nodes))
	return FlatRow{}
}

func rowNames(m *Model) []string {
	out := make([]string, 0, len(m.nodes))
	for _, r := range m.nodes {
		out = append(out, r.Row.DisplayName)
	}
	return out
}

func body(m *Model) string { return ansi.Strip(strings.Join(m.BodyLines(), "\n")) }

// space folds the subtree under the cursor and opens it again. A folded row
// shows ▸ and how many rows it hides, an open one ▾, and the status line
// says what the folds hold.
func TestSpaceFoldsAndUnfolds(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	before := len(m.nodes)

	m.handleKey("space")
	r, _ := m.cursorRow()
	if !r.Folded || r.FoldedCount != 3 || r.Row.DisplayName != "transform" {
		t.Fatalf("after space: %+v", r)
	}
	if len(m.nodes) != before-3 {
		t.Fatalf("rows = %d, want %d", len(m.nodes), before-3)
	}
	if !strings.Contains(r.Indent, markerFolded) {
		t.Errorf("folded row indent %q lacks %s", r.Indent, markerFolded)
	}
	b := body(m)
	if !strings.Contains(b, "├▸ ✗ transform Retry ↻ 2 +3") {
		t.Errorf("folded row not drawn with its count:\n%s", b)
	}
	if !strings.Contains(m.tabStatusLine(), "3 in 1 fold") {
		t.Errorf("status %q does not state the fold", m.tabStatusLine())
	}

	m.handleKey("space")
	r, _ = m.cursorRow()
	if r.Folded || len(m.nodes) != before || !strings.Contains(r.Indent, markerOpen) {
		t.Fatalf("after second space: %+v, %d rows", r, len(m.nodes))
	}
	if strings.Contains(m.tabStatusLine(), "fold") {
		t.Errorf("status %q still mentions a fold", m.tabStatusLine())
	}
}

// left folds an open row and climbs from a folded row or a leaf; right opens
// a folded row and steps into an open one. A leaf is not foldable.
func TestLeftAndRightWalkTheTree(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform(1)")

	m.handleKey("space") // a leaf: nothing to fold
	if r, _ := m.cursorRow(); r.Folded || r.Row.DisplayName != "transform(1)" {
		t.Fatalf("space on a leaf changed something: %+v", r)
	}
	m.handleKey("left") // leaf: to the parent
	if r, _ := m.cursorRow(); r.Row.DisplayName != "transform" || r.Folded {
		t.Fatalf("left on a leaf went to %+v", r.Row.DisplayName)
	}
	m.handleKey("left") // open: folds
	if r, _ := m.cursorRow(); !r.Folded {
		t.Fatal("left on an open row did not fold it")
	}
	m.handleKey("left") // folded: to the parent
	if r, _ := m.cursorRow(); r.Row.DisplayName != "demo-nightly-report" {
		t.Fatalf("left on a folded row went to %q", r.Row.DisplayName)
	}
	cursorTo(t, m, "transform")
	m.handleKey("right") // folded: opens
	if r, _ := m.cursorRow(); r.Folded {
		t.Fatal("right did not open the fold")
	}
	m.handleKey("right") // open: first child
	if r, _ := m.cursorRow(); r.Row.DisplayName != "transform(0)" {
		t.Fatalf("right on an open row went to %q", r.Row.DisplayName)
	}
	m.handleKey("right") // leaf: stays
	if r, _ := m.cursorRow(); r.Row.DisplayName != "transform(0)" {
		t.Fatalf("right on a leaf moved to %q", r.Row.DisplayName)
	}
}

// Folds are keyed by node ID, so a refresh of the same workflow keeps them
// and keeps the cursor on its node even when the refresh adds rows above it.
// Another workflow starts unfolded.
func TestFoldsSurviveARefresh(t *testing.T) {
	wf := demoWorkflow(t, "demo-nightly-report")
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	m.handleKey("space")
	cursorTo(t, m, "load")

	// The refresh brings a node that sorts before everything else.
	next := copyWorkflow(wf)
	early := testkit.FixtureEpoch.Add(-10 * time.Hour)
	next.Nodes["early"] = core.Node{ID: "early", Name: "early", DisplayName: "early", Type: "Pod", Phase: "Succeeded", StartedAt: &early}
	m.SetWorkflow(next, testkit.FixtureEpoch)

	r, _ := m.cursorRow()
	if r.Row.DisplayName != "load" {
		t.Errorf("cursor moved to %q on refresh", r.Row.DisplayName)
	}
	folded := false
	for _, r := range m.nodes {
		if r.Row.DisplayName == "transform" && r.Folded {
			folded = true
		}
	}
	if !folded {
		t.Error("the fold did not survive the refresh")
	}

	other := copyWorkflow(wf)
	other.Summary.Ref.UID = "another-run"
	m.SetWorkflow(other, testkit.FixtureEpoch)
	if len(m.folded) != 0 || m.folds != 0 {
		t.Errorf("another workflow kept folds: %v", m.folded)
	}
}

func copyWorkflow(wf core.Workflow) core.Workflow {
	out := wf
	out.Nodes = make(map[string]core.Node, len(wf.Nodes))
	for k, v := range wf.Nodes {
		out.Nodes[k] = v
	}
	return out
}

// retryOfSteps is a DAG whose task retries a steps template: each attempt
// is a Steps node with a pod of its own.
func retryOfSteps(attempts int, lastRunning bool) core.Workflow {
	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Running", "", 0, "r").
		node("r", "wf.job", "job", "Retry", "Running", "wf", 0)
	var kids []string
	for i := 0; i < attempts; i++ {
		id := "a" + itoaDetail(i)
		kids = append(kids, id)
		phase := "Failed"
		if lastRunning && i == attempts-1 {
			phase = "Running"
		}
		f.node(id, "wf.job("+itoaDetail(i)+")", "job("+itoaDetail(i)+")", "Steps", phase, "wf", i, id+"-pod")
		f.node(id+"-pod", "wf.job("+itoaDetail(i)+").pod", "pod", "Pod", phase, id, i)
		f.with(id, func(n *core.Node) { n.Retried = true })
		if phase != "Running" {
			end := testkit.FixtureEpoch.Add(time.Duration(i)*time.Minute + 30*time.Second)
			f.with(id, func(n *core.Node) { n.FinishedAt = &end })
		}
	}
	f.with("r", func(n *core.Node) { n.Children = kids })
	return f.workflow()
}

// Finished attempts other than the last start folded; the last attempt is
// the one that decided the outcome. The default applies once per node: an
// attempt the reader opens stays open through refreshes, and an attempt that
// stops being the last is folded when it does.
func TestOlderRetryAttemptsStartFolded(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 30)
	m.SetWorkflow(retryOfSteps(3, true), testkit.FixtureEpoch)
	m.handleKey("tab")
	if !m.folded["a0"] || !m.folded["a1"] || m.folded["a2"] {
		t.Fatalf("default folds = %v, want a0 and a1", m.folded)
	}

	cursorTo(t, m, "job(0)")
	m.handleKey("space") // the reader opens the first attempt
	m.SetWorkflow(retryOfSteps(4, true), testkit.FixtureEpoch)
	if m.folded["a0"] {
		t.Error("a refresh folded an attempt the reader opened")
	}
	if !m.folded["a2"] || m.folded["a3"] {
		t.Errorf("after a new attempt, folds = %v, want a2 folded and a3 open", m.folded)
	}
}

// The info panel sits to the right of a wide pane and under the tree of a
// narrower one, and gives way on a pane too narrow for either. Every line
// fits the pane in both placements.
func TestInfoPanelPlacement(t *testing.T) {
	cases := []struct {
		width int
		place infoPlacement
	}{{160, infoRight}, {136, infoRight}, {120, infoBottom}, {76, infoBottom}, {56, infoBottom}, {50, infoHidden}}
	for _, tc := range cases {
		m := demoModel(t, "demo-nightly-report", tc.width, 30)
		cursorTo(t, m, "extract")
		m.handleKey("i")
		if got := m.nodesLayout().place; got != tc.place {
			t.Errorf("width %d: placement %v, want %v", tc.width, got, tc.place)
		}
		lines := m.BodyLines()
		if len(lines) > 30 {
			t.Errorf("width %d: %d lines for a height of 30", tc.width, len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > tc.width {
				t.Errorf("width %d: a %d-cell line: %q", tc.width, w, ansi.Strip(l))
			}
		}
		b := body(m)
		switch tc.place {
		case infoRight:
			if !strings.Contains(b, "│ NODE extract") || !strings.Contains(b, "│ host") {
				t.Errorf("width %d: no right panel:\n%s", tc.width, b)
			}
		case infoBottom:
			if !strings.Contains(b, "─ NODE extract") || !strings.Contains(b, "host") {
				t.Errorf("width %d: no bottom panel:\n%s", tc.width, b)
			}
		case infoHidden:
			if strings.Contains(b, "NODE extract") {
				t.Errorf("width %d: the panel is drawn:\n%s", tc.width, b)
			}
			if !strings.Contains(m.tabStatusLine(), "no room for info") {
				t.Errorf("width %d: status %q does not say why", tc.width, m.tabStatusLine())
			}
		}
		m.handleKey("i")
		if strings.Contains(body(m), "NODE extract") {
			t.Errorf("width %d: i did not close the panel", tc.width)
		}
	}
}

// The panel states the run facts the model holds for the node, and never
// shows a parameter value until v reveals it, with the resource tab's marker
// and the same session-only reveal.
func TestInfoPanelFactsAndRedaction(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 160, 40)
	m.SetRedactByDefault(true)
	cursorTo(t, m, "extract")
	m.handleKey("i")
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
	for _, secret := range []string{"2026-09-07", "warehouse", "184022"} {
		if strings.Contains(b, secret) {
			t.Errorf("panel shows %q before reveal", secret)
		}
	}

	m.handleKey("v")
	b = body(m)
	for _, want := range []string{"date = 2026-09-07", "source = warehouse", "rows = 184022", "values shown (v redacts)"} {
		if !strings.Contains(b, want) {
			t.Errorf("revealed panel lacks %q:\n%s", want, b)
		}
	}
	if !m.Reveal() {
		t.Error("v on the nodes tab did not set the shared reveal state")
	}
	other := demoWorkflow(t, "demo-nightly-report")
	other.Summary.Ref.UID = "another-run"
	m.SetWorkflow(other, testkit.FixtureEpoch)
	cursorTo(t, m, "extract")
	if strings.Contains(body(m), "2026-09-07") {
		t.Error("the reveal outlived the workflow")
	}
}

// The panel of a retry attempt and of the gate carry their flags and state.
func TestInfoPanelFlags(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 160, 40)
	cursorTo(t, m, "transform(1)")
	m.handleKey("i")
	if b := body(m); !strings.Contains(b, "flags     retried") || !strings.Contains(b, "exit code 1") {
		t.Errorf("attempt panel:\n%s", b)
	}
	cursorTo(t, m, "onExit")
	if b := body(m); !strings.Contains(b, "flags     hooked") || !strings.Contains(b, "type      Pod (exit handler)") {
		t.Errorf("exit handler panel:\n%s", b)
	}
	gate := demoModel(t, "demo-release-gate", 160, 40)
	cursorTo(t, gate, "approve-production")
	gate.handleKey("i")
	if b := body(gate); !strings.Contains(b, "AWAITING RESUME") || !strings.Contains(b, "duration  4m50s (running)") {
		t.Errorf("gate panel:\n%s", b)
	}
}

// / opens the find input. While it is open every printable key types into
// it: q, h, i, j and space change nothing but the query.
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

func press(m *Model, key string) {
	msg := tea.KeyPressMsg{Text: key}
	switch key {
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		msg.Code = []rune(key)[0]
	}
	m.Update(msg)
}

func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// enter jumps to the first match, n and N step through the matches and wrap,
// and the status line counts them. esc clears the find before it leaves the
// workflow.
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
	if r, _ := m.cursorRow(); r.Row.DisplayName != "train-shard(0:0)" {
		t.Fatalf("enter jumped to %q", r.Row.DisplayName)
	}
	if !strings.Contains(m.tabStatusLine(), "match 1/6") {
		t.Errorf("status %q", m.tabStatusLine())
	}
	press(m, "n")
	press(m, "n")
	if r, _ := m.cursorRow(); r.Row.DisplayName != "train-shard(2:2)" || !strings.Contains(m.tabStatusLine(), "match 3/6") {
		t.Errorf("n n: %q, %q", r.Row.DisplayName, m.tabStatusLine())
	}
	press(m, "N")
	press(m, "N")
	press(m, "N")
	if r, _ := m.cursorRow(); r.Row.DisplayName != "train-shard(5:5)" || !strings.Contains(m.tabStatusLine(), "match 6/6") {
		t.Errorf("N wrapped to %q, %q", r.Row.DisplayName, m.tabStatusLine())
	}
	if !strings.HasPrefix(m.Hints(), "n next  N previous  esc clear find") {
		t.Errorf("hints during a find = %q", m.Hints())
	}
	if cmd := m.handleKey("esc"); cmd != nil {
		t.Fatal("esc left the workflow while a find was active")
	}
	if m.find.active() || strings.Contains(m.tabStatusLine(), "match") {
		t.Errorf("esc did not clear the find: %q", m.tabStatusLine())
	}
	if cmd := m.handleKey("esc"); cmd == nil {
		t.Fatal("esc without a find no longer goes back")
	}
}

// A match under a folded parent is found, and jumping to it opens the folds
// above it.
func TestFindOpensFoldsAboveAMatch(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 30)
	cursorTo(t, m, "transform")
	m.handleKey("space")
	cursorTo(t, m, "demo-nightly-report")
	m.handleKey("space") // the root folded too: two folds above the match
	press(m, "/")
	typeText(m, "transform(1")
	press(m, "enter")
	r, _ := m.cursorRow()
	if r.Row.DisplayName != "transform(1)" {
		t.Fatalf("cursor on %q, rows %v", r.Row.DisplayName, rowNames(m))
	}
	if len(m.folded) != 0 {
		t.Errorf("folds above the match stayed: %v", m.folded)
	}
}

// A name that only a hidden skipped node carries is not jumped to, but the
// status says where it is.
func TestFindCountsMatchesAmongHiddenSkipped(t *testing.T) {
	m := demoModel(t, "demo-deploy-multi-layer", 120, 30)
	press(m, "/")
	typeText(m, "us-east")
	press(m, "enter")
	if s := m.tabStatusLine(); !strings.Contains(s, `no match for "us-east" (4 among hidden skipped)`) {
		t.Errorf("status %q", s)
	}
	m.handleKey("h")
	if s := m.tabStatusLine(); !strings.Contains(s, "match 1/4") {
		t.Errorf("after h: %q", s)
	}
}

// The status line and the footer say what the tab is not showing: the
// skipped rows, the rows in folds, the phase filter.
func TestStatusAndHintsStateWhatIsHidden(t *testing.T) {
	m := demoModel(t, "demo-deploy-multi-layer", 120, 30)
	if s := m.tabStatusLine(); !strings.Contains(s, "16 skipped hidden") || !strings.Contains(s, "sort pipeline") {
		t.Errorf("status %q", s)
	}
	if h := m.Hints(); !strings.Contains(h, "h show 16 skipped") {
		t.Errorf("hints %q", h)
	}
	m.handleKey("h")
	if h := m.Hints(); !strings.Contains(h, "h hide skipped") {
		t.Errorf("hints after h: %q", h)
	}
	for _, key := range []string{"space fold", "i info", "/ find", "s sort", "p phase", "l logs"} {
		if !strings.Contains(m.Hints(), key) {
			t.Errorf("hints lack %q", key)
		}
	}
	m.handleKey("p")
	if s := m.tabStatusLine(); !strings.Contains(s, "phase Failed") {
		t.Errorf("status with a filter %q", s)
	}
}

// A phase filter that matches nothing says so; it does not claim the
// workflow has no nodes.
func TestEmptyPhaseFilterIsNotAnEmptyWorkflow(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 120, 20)
	m.handleKey("p") // Failed
	m.handleKey("p") // Running: nothing runs in a finished workflow
	b := body(m)
	if !strings.Contains(b, "(no Running nodes — p changes the filter)") || strings.Contains(b, "not started") {
		t.Fatalf("empty filter body:\n%s", b)
	}
}

// The body fits its height budget at every size, and the column heads,
// the header and the status line are all there when there is room.
func TestNodesBodyFitsItsBudget(t *testing.T) {
	for _, h := range []int{3, 5, 8, 12, 30} {
		for _, info := range []bool{false, true} {
			m := demoModel(t, "demo-deploy-multi-layer", 100, h)
			m.handleKey("h")
			if info {
				m.handleKey("i")
			}
			m.handleKey("G")
			lines := m.BodyLines()
			if len(lines) > h {
				t.Errorf("height %d info %v: %d lines", h, info, len(lines))
			}
			if h >= 12 {
				b := body(m)
				want := []string{"[Nodes]", "progress 5/5", "26 nodes"}
				if !info || h >= 30 {
					want = append(want, "NAME")
				}
				for _, want := range want {
					if !strings.Contains(b, want) {
						t.Errorf("height %d info %v: body lacks %q:\n%s", h, info, want, b)
					}
				}
				if !strings.Contains(b, "apply-eu-west") {
					t.Errorf("height %d info %v: the last row is not on screen after G:\n%s", h, info, b)
				}
			}
		}
	}
}

// The raw view is the whole tab as plain text: the header, the heads and
// every row at its natural width, with ASCII bars.
func TestNodesRawLines(t *testing.T) {
	m := demoModel(t, "demo-train-pipeline", 60, 10)
	m.SetTheme(nordTheme(t))
	lines := m.RawLines()
	if len(lines) != len(m.nodes)+2 {
		t.Fatalf("%d raw lines for %d rows", len(lines), len(m.nodes))
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "\x1b") {
		t.Error("raw lines carry escapes")
	}
	for _, want := range []string{"progress 3/8", "TEMPLATE", "TIMELINE", "evaluate ← train-shard(0:0), train-shard(1:1) +4", "train-shard"} {
		if !strings.Contains(joined, want) {
			t.Errorf("raw lines lack %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "=>") {
		t.Errorf("raw bars are not ASCII:\n%s", joined)
	}
}

// Without redactValues the panel shows the node's values from the start.
func TestInfoPanelShowsValuesByDefault(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 160, 40)
	cursorTo(t, m, "extract")
	m.handleKey("i")
	b := body(m)
	for _, want := range []string{"date = 2026-09-07", "source = warehouse", "rows = 184022", "values shown (v redacts)"} {
		if !strings.Contains(b, want) {
			t.Errorf("panel lacks %q:\n%s", want, b)
		}
	}
}
