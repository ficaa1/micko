package detail

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// pathNames is the critical path as display names, in run order.
func pathNames(m *Model) []string {
	names := map[string]string{}
	for _, r := range m.tl.rows {
		names[r.Row.NodeID] = r.Row.DisplayName
	}
	var out []string
	for _, id := range m.tl.path {
		out = append(out, names[id])
	}
	return out
}

func tlRowNamed(t *testing.T, m *Model, name string) tlRow {
	t.Helper()
	for _, r := range m.tl.rows {
		if r.Row.DisplayName == name {
			return r
		}
	}
	t.Fatalf("no timeline row %q", name)
	return tlRow{}
}

// The tick step is the smallest round step whose labels fit with room between them.
func TestAxisStepFitsTheSpan(t *testing.T) {
	cases := []struct {
		span  time.Duration
		width int
		want  time.Duration
	}{
		{3 * time.Second, 60, time.Second},
		{45 * time.Second, 30, 10 * time.Second},
		{45 * time.Second, 90, 5 * time.Second},
		{4 * time.Minute, 34, time.Minute},
		{4 * time.Minute, 96, 30 * time.Second},
		{12 * time.Minute, 34, 2 * time.Minute},
		{12 * time.Minute, 90, time.Minute},
		{40 * time.Minute, 36, 10 * time.Minute},
		{2 * time.Hour, 60, 15 * time.Minute},
		{17 * time.Hour, 80, 2 * time.Hour},
		{3 * 24 * time.Hour, 60, 12 * time.Hour},
		{3 * 24 * time.Hour, 30, 24 * time.Hour},
		{0, 60, 0},
		{time.Minute, 0, 0},
	}
	for _, tc := range cases {
		if got := axisStep(tc.span, tc.width); got != tc.want {
			t.Errorf("axisStep(%v, %d) = %v, want %v", tc.span, tc.width, got, tc.want)
		}
	}
}

func TestTickLabel(t *testing.T) {
	cases := map[time.Duration]string{
		0:                               "0",
		45 * time.Second:                "45s",
		2 * time.Minute:                 "2m",
		90 * time.Second:                "1m30s",
		65 * time.Second:                "1m05s",
		time.Hour:                       "1h",
		65 * time.Minute:                "1h05m",
		48 * time.Hour:                  "2d",
		36 * time.Hour:                  "1d12h",
		24*time.Hour + 30*time.Minute:   "1d00h30m",
		500 * time.Millisecond:          "0",
		3*time.Hour + 30*time.Second:    "3h00m30s",
		2*24*time.Hour + 2*time.Hour:    "2d02h",
		time.Duration(3) * time.Second:  "3s",
		time.Duration(10) * time.Minute: "10m",
	}
	for d, want := range cases {
		if got := tickLabel(d); got != want {
			t.Errorf("tickLabel(%v) = %q, want %q", d, got, want)
		}
	}
}

// From seconds to days the axis labels sit in order inside the chart, the end label flush right.
func TestAxisTicksFromSecondsToDays(t *testing.T) {
	spans := []time.Duration{
		3 * time.Second, 10 * time.Second, 45 * time.Second, 4 * time.Minute, 12 * time.Minute,
		40 * time.Minute, 2 * time.Hour, 17 * time.Hour, 3 * 24 * time.Hour,
	}
	start := testkit.FixtureEpoch
	for _, d := range spans {
		for _, w := range []int{20, 34, 60, 96} {
			span := timeSpan{start: start, end: start.Add(d)}
			ticks := axisTicks(span, w, w+1, "now")
			if len(ticks) < 2 && w >= 34 {
				t.Errorf("span %v width %d: only %d ticks", d, w, len(ticks))
			}
			last := ticks[len(ticks)-1]
			if last.label != "now" || last.col+3 != w+1 {
				t.Errorf("span %v width %d: end tick %+v", d, w, last)
			}
			next := 0
			for _, tk := range ticks {
				if tk.col < next {
					t.Errorf("span %v width %d: ticks overlap: %+v", d, w, ticks)
				}
				next = tk.col + cellWidth(tk.label) + 1
			}
			if ticks[0].label != "0" && len(ticks) > 1 {
				t.Errorf("span %v width %d: first tick %q, want 0", d, w, ticks[0].label)
			}
			if line := axisLine(ticks, w+1); cellWidth(line) > w+1 {
				t.Errorf("span %v width %d: axis line %q is too wide", d, w, line)
			}
		}
	}
	// The three-day axis steps in half days.
	span := timeSpan{start: start, end: start.Add(3 * 24 * time.Hour)}
	got := axisLine(axisTicks(span, 60, 60, "3d00h"), 60)
	if !strings.Contains(got, "12h") || !strings.Contains(got, "1d12h") {
		t.Errorf("three-day axis = %q", got)
	}
}

// A node's run sits on the timeline cell for cell where the nodes tab puts it.
func TestTimelineBarsSharePlacementWithTheNodesTab(t *testing.T) {
	for _, name := range []string{"demo-release-gate", "demo-nightly-report", "demo-train-pipeline", "demo-data-pull", "demo-deploy-multi-layer"} {
		wf := demoWorkflow(t, name)
		m := sectionModel(t, name, "timeline", 136, 40)
		span := workflowSpan(wf, m.now)
		for _, r := range m.tl.rows {
			if r.heading || r.Section != "" {
				continue
			}
			for _, w := range []int{12, 24, 40, 81} {
				for _, ascii := range []bool{false, true} {
					nc, nk := splitCells(nodeBar(span, r.Row, m.now, w, ascii))
					gc, gk := splitCells(ganttBar(span, r.Row, r.ready, m.now, w, ascii))
					if len(nc) != w || len(gc) != w {
						t.Fatalf("%s/%s width %d: %d and %d cells", name, r.Row.DisplayName, w, len(nc), len(gc))
					}
					for i := range nc {
						switch {
						case nk[i] != barTrack && gc[i] != nc[i]:
							t.Errorf("%s/%s width %d cell %d: timeline %q, nodes %q", name, r.Row.DisplayName, w, i, gc[i], nc[i])
						case nk[i] == barTrack && gk[i] != barTrack && gk[i] != barWait:
							t.Errorf("%s/%s width %d cell %d: timeline draws %q where the nodes tab has track", name, r.Row.DisplayName, w, i, gc[i])
						}
					}
				}
			}
		}
	}
}

// Bars take their phase's colour through the bar tokens.
func TestTimelineBarKinds(t *testing.T) {
	m := sectionModel(t, "demo-nightly-report", "timeline", 136, 40)
	span := workflowSpan(m.workflow(), m.now)
	kindsOf := func(name string) map[barKind]bool {
		r := tlRowNamed(t, m, name)
		_, kinds := splitCells(ganttBar(span, r.Row, r.ready, m.now, 96, false))
		out := map[barKind]bool{}
		for _, k := range kinds {
			out[k] = true
		}
		return out
	}
	if k := kindsOf("extract"); !k[barDone] || k[barFailed] {
		t.Errorf("extract kinds %v", k)
	}
	if k := kindsOf("transform(1)"); !k[barFailed] || !k[barWait] {
		t.Errorf("transform(1) kinds %v, want a failed bar after a wait", k)
	}
	th := skin(t)
	if barFailed.style(th).Render("x") != th.PhaseFailed.Render("x") ||
		barGate.style(th).Render("x") != th.Warning.Render("x") ||
		barWait.style(th).Render("x") != th.BarEmpty.Render("x") {
		t.Error("bar kinds do not map to their tokens")
	}

	g := sectionModel(t, "demo-release-gate", "timeline", 136, 40)
	gate := tlRowNamed(t, g, "approve-production")
	if barKindFor(gate.Row, true) != barGate {
		t.Error("the waiting gate is not drawn as a gate")
	}
	shard := tlRowNamed(t, sectionModel(t, "demo-train-pipeline", "timeline", 136, 40), "train-shard(2:2)")
	if barKindFor(shard.Row, true) != barLive {
		t.Error("a running shard is not drawn as live work")
	}
}

// A node's wait runs from the end of what it waited for to its start.
func TestTimelineReadyTimes(t *testing.T) {
	m := sectionModel(t, "demo-nightly-report", "timeline", 136, 40)
	end := func(name string) time.Time { return *tlRowNamed(t, m, name).Row.FinishedAt }
	start := func(name string) time.Time { return *tlRowNamed(t, m, name).Row.StartedAt }
	cases := []struct{ name, want string }{
		{"transform(1)", "transform(0) end"},
		{"transform(2)", "transform(1) end"},
		{"transform(0)", "transform start"},
		{"transform", "extract end"},
		{"extract", "demo-nightly-report start"},
	}
	for _, tc := range cases {
		ref, what, _ := strings.Cut(tc.want, " ")
		want := start(ref)
		if what == "end" {
			want = end(ref)
		}
		if got := tlRowNamed(t, m, tc.name).ready; !got.Equal(want) {
			t.Errorf("%s ready %v, want %s (%v)", tc.name, got, tc.want, want)
		}
	}

	s := sectionModel(t, "demo-data-pull", "timeline", 136, 40)
	inc := tlRowNamed(t, s, "incremental-sync")
	if got := tlRowNamed(t, s, "pull(us)").ready; !got.Equal(*inc.Row.FinishedAt) {
		t.Errorf("pull(us) ready %v, want the end of group [1] %v", got, *inc.Row.FinishedAt)
	}
}

// The critical path of every demo workflow.
func TestTimelineCriticalPathOfTheDemo(t *testing.T) {
	cases := map[string]string{
		"demo-release-gate":          "build → integration-test → approve-production",
		"demo-nightly-report":        "extract → transform(0) → transform(1) → transform(2)",
		"demo-train-pipeline":        "preprocess → train-shard(2:2)",
		"demo-data-pull":             "list-sources → incremental-sync → pull(us)",
		"demo-deploy-multi-layer":    "plan → apply-eu-west",
		"demo-hello-world":           "demo-hello-world",
		"demo-oom-backfill":          "backfill-2026-q2",
		"demo-etl-hourly-1790000000": "ingest → quality-check",
		"demo-param-check":           "",
		"demo-cleanup":               "",
	}
	for name, want := range cases {
		m := sectionModel(t, name, "timeline", 136, 40)
		if got := strings.Join(pathNames(m), " → "); got != want {
			t.Errorf("%s: critical path %q, want %q", name, got, want)
		}
		for _, r := range m.tl.rows {
			on := strings.Contains(" → "+want+" → ", " → "+r.Row.DisplayName+" → ")
			if r.critical != on && !(name == "demo-deploy-multi-layer" && r.Row.DisplayName == "apply-eu-west") {
				t.Errorf("%s/%s: critical = %v", name, r.Row.DisplayName, r.critical)
			}
		}
	}
	// Of the four layers' apply-eu-west, only the last layer's is on it.
	m := sectionModel(t, "demo-deploy-multi-layer", "timeline", 136, 40)
	var marked []string
	for _, r := range m.tl.rows {
		if r.critical {
			marked = append(marked, r.Row.Name)
		}
	}
	if len(marked) != 2 || !strings.Contains(marked[1], ".edge.") {
		t.Errorf("deploy critical rows %v, want plan and the edge layer's apply", marked)
	}
}

// critFixture is a timeline of f with start and end minutes per node; an end of -1 is still running.
func critFixture(t *testing.T, f *treeFixture, times map[string][2]float64) (*Model, core.Workflow) {
	t.Helper()
	at := func(min float64) *time.Time {
		v := testkit.FixtureEpoch.Add(time.Duration(min * float64(time.Minute)))
		return &v
	}
	for id, se := range times {
		f.with(id, func(n *core.Node) {
			n.StartedAt = at(se[0])
			if se[1] >= 0 {
				n.FinishedAt = at(se[1])
			} else {
				n.FinishedAt = nil
			}
		})
	}
	wf := f.workflow()
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 30)
	m.SetWorkflow(wf, testkit.FixtureEpoch.Add(20*time.Minute))
	m.SetSection("timeline")
	return m, wf
}

// The critical path follows the dependency that finished last, into nested work and the exit handler.
func TestTimelineCriticalPathRules(t *testing.T) {
	t.Run("the slower side of a diamond", func(t *testing.T) {
		f := newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Succeeded", "", 0, "a").
			node("a", "wf.a", "a", "Pod", "Succeeded", "wf", 0, "b", "c").
			node("b", "wf.b", "b", "Pod", "Succeeded", "wf", 0, "d").
			node("c", "wf.c", "c", "Pod", "Succeeded", "wf", 0, "d").
			node("d", "wf.d", "d", "Pod", "Succeeded", "wf", 0)
		m, _ := critFixture(t, f, map[string][2]float64{
			"wf": {0, 10}, "a": {0, 2}, "b": {2, 5}, "c": {2, 9}, "d": {9, 10},
		})
		if got := strings.Join(pathNames(m), " "); got != "a c d" {
			t.Errorf("path %q, want a c d", got)
		}
	})
	t.Run("into a nested template and out again", func(t *testing.T) {
		f := newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Succeeded", "", 0, "x").
			node("x", "wf.x", "x", "Pod", "Succeeded", "wf", 0, "inner").
			node("inner", "wf.inner", "inner", "Steps", "Succeeded", "wf", 0, "g0").
			node("g0", "wf.inner[0]", "[0]", "StepGroup", "Succeeded", "inner", 0, "s1").
			node("s1", "wf.inner[0].s1", "s1", "Pod", "Succeeded", "inner", 0, "g1").
			node("g1", "wf.inner[1]", "[1]", "StepGroup", "Succeeded", "inner", 0, "s2").
			node("s2", "wf.inner[1].s2", "s2", "Pod", "Succeeded", "inner", 0, "y").
			node("y", "wf.y", "y", "Pod", "Succeeded", "wf", 0).
			node("side", "wf.side", "side", "Pod", "Succeeded", "wf", 0)
		f.with("wf", func(n *core.Node) { n.Children = []string{"x", "side"} })
		m, _ := critFixture(t, f, map[string][2]float64{
			"wf": {0, 7}, "x": {0, 1}, "inner": {1, 5}, "g0": {1, 3}, "s1": {1, 3},
			"g1": {3, 5}, "s2": {3, 5}, "y": {5.5, 7}, "side": {0, 6},
		})
		if got := strings.Join(pathNames(m), " "); got != "x s1 s2 y" {
			t.Errorf("path %q, want x s1 s2 y", got)
		}
		if got := tlRowNamed(t, m, "y").ready; !got.Equal(*tlRowNamed(t, m, "inner").Row.FinishedAt) {
			t.Errorf("y ready %v, want the end of inner", got)
		}
	})
	t.Run("the exit handler that ran last", func(t *testing.T) {
		f := newTreeFixture("wf").
			node("wf", "wf", "wf", "Steps", "Failed", "", 0, "g0").
			node("g0", "wf[0]", "[0]", "StepGroup", "Failed", "wf", 0, "only").
			node("only", "wf[0].only", "only", "Pod", "Failed", "wf", 0).
			node("exit", "wf.onExit", "onExit", "Pod", "Succeeded", "", 0).
			with("exit", func(n *core.Node) { n.Hooked = true })
		m, _ := critFixture(t, f, map[string][2]float64{
			"wf": {0, 4}, "g0": {0, 4}, "only": {0, 4}, "exit": {4.5, 5},
		})
		if got := strings.Join(pathNames(m), " "); got != "only onExit" {
			t.Errorf("path %q, want only onExit", got)
		}
		if got := tlRowNamed(t, m, "onExit").ready; !got.Equal(*tlRowNamed(t, m, "wf").Row.FinishedAt) {
			t.Errorf("exit handler ready %v, want the workflow tree's end", got)
		}
	})
	t.Run("running work, first in pipeline order", func(t *testing.T) {
		f := newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Running", "", 0, "a").
			node("a", "wf.a", "a", "Pod", "Succeeded", "wf", 0, "b", "c").
			node("b", "wf.b", "b", "Pod", "Running", "wf", 0).
			node("c", "wf.c", "c", "Pod", "Running", "wf", 0)
		m, _ := critFixture(t, f, map[string][2]float64{
			"wf": {0, -1}, "a": {0, 2}, "b": {2, -1}, "c": {2, -1},
		})
		if got := strings.Join(pathNames(m), " "); got != "a b" {
			t.Errorf("path %q, want a b", got)
		}
		if note := m.criticalNote(); !strings.Contains(note, "so far") {
			t.Errorf("running note %q does not say so far", note)
		}
	})
	t.Run("nothing started", func(t *testing.T) {
		f := newTreeFixture("wf").
			node("wf", "wf", "wf", "DAG", "Pending", "", -1, "a").
			node("a", "wf.a", "a", "Pod", "Pending", "wf", -1)
		wf := f.workflow()
		m := New()
		m.SetWorkflow(wf, testkit.FixtureEpoch)
		if len(m.tl.path) != 0 || m.criticalNote() != "" {
			t.Errorf("path %v note %q, want none", m.tl.path, m.criticalNote())
		}
	})
}

// Skipped subtrees in which nothing ran are left out and counted; omitted nodes keep an empty track.
func TestTimelineLeavesOutSkippedWork(t *testing.T) {
	m := sectionModel(t, "demo-deploy-multi-layer", "timeline", 136, 40)
	if m.tl.leftOut != 16 {
		t.Errorf("left out %d, want 16", m.tl.leftOut)
	}
	if s := m.timelineStatusLine(); !strings.Contains(s, "16 skipped not drawn") {
		t.Errorf("status %q", s)
	}
	for _, r := range m.tl.rows {
		if r.Skipped() {
			t.Errorf("skipped row %q drawn", r.Row.DisplayName)
		}
	}
	n := sectionModel(t, "demo-nightly-report", "timeline", 136, 40)
	load := tlRowNamed(t, n, "load")
	if got := barText(ganttBar(workflowSpan(n.workflow(), n.now), load.Row, load.ready, n.now, 20, false)); got != strings.Repeat(glyphTrack, 20) {
		t.Errorf("omitted load bar %q", got)
	}

	f := newTreeFixture("wf").
		node("wf", "wf", "wf", "Skipped", "Skipped", "", 0)
	all := New()
	all.SetSize(80, 20)
	all.SetWorkflow(f.workflow(), testkit.FixtureEpoch)
	all.SetSection("timeline")
	if got := strings.Join(all.BodyLines(), "\n"); !strings.Contains(got, "every node was skipped") {
		t.Errorf("all-skipped body:\n%s", got)
	}
}

// The timeline folds as the nodes tab does and shares its folds; a fold hiding critical work is marked.
func TestTimelineFolding(t *testing.T) {
	m := sectionModel(t, "demo-nightly-report", "timeline", 136, 40)
	for i, r := range m.tl.rows {
		if r.Row.DisplayName == "transform" {
			m.tlCursor = i
		}
	}
	before := len(m.tl.rows)
	press(m, "space")
	r, _ := m.tlCursorRow()
	if !r.Folded || r.FoldedCount != 3 || len(m.tl.rows) != before-3 {
		t.Fatalf("after space: %+v, %d rows", r.FlatRow, len(m.tl.rows))
	}
	if !r.critical {
		t.Error("the folded retry hides critical attempts but has no marker")
	}
	if !strings.Contains(m.timelineStatusLine(), "3 in 1 fold") {
		t.Errorf("status %q", m.timelineStatusLine())
	}
	if !m.folded[r.Row.NodeID] {
		t.Error("the fold is not shared with the nodes tab")
	}
	m.SetSection("nodes")
	if nr, _ := m.cursorRow(); !strings.Contains(strings.Join(rowNames(m), ","), "transform") || nr.Row.DisplayName == "" {
		t.Fatal("nodes tab lost its rows")
	}
	for _, fr := range m.nodes {
		if fr.Row.DisplayName == "transform" && !fr.Folded {
			t.Error("the nodes tab does not show the timeline's fold")
		}
	}
	m.SetSection("timeline")

	press(m, "right") // folded: opens
	if r, _ := m.tlCursorRow(); r.Folded || len(m.tl.rows) != before {
		t.Fatalf("right did not open the fold: %+v", r.FlatRow)
	}
	press(m, "right") // open: first child
	if r, _ := m.tlCursorRow(); r.Row.DisplayName != "transform(0)" {
		t.Fatalf("right on an open row went to %q", r.Row.DisplayName)
	}
	press(m, "left") // leaf: parent
	if r, _ := m.tlCursorRow(); r.Row.DisplayName != "transform" {
		t.Fatalf("left on a leaf went to %q", r.Row.DisplayName)
	}
	press(m, "left") // open: folds
	if r, _ := m.tlCursorRow(); !r.Folded {
		t.Fatal("left on an open row did not fold it")
	}
	press(m, "left") // folded: parent
	if r, _ := m.tlCursorRow(); r.Row.DisplayName != "demo-nightly-report" {
		t.Fatalf("left on a folded row went to %q", r.Row.DisplayName)
	}
	// The cursor stays on its node across a refresh.
	press(m, "j")
	id := m.tlSelectedID()
	m.SetWorkflow(demoWorkflow(t, "demo-nightly-report"), testkit.FixtureEpoch)
	if m.tlSelectedID() != id {
		t.Errorf("refresh moved the cursor from %s to %s", id, m.tlSelectedID())
	}
}

// The status line says what the marker means and what is left out; the hints name the keys.
func TestTimelineStatusAndHints(t *testing.T) {
	m := sectionModel(t, "demo-release-gate", "timeline", 136, 40)
	s := body(m)
	for _, want := range []string{"4 rows", "1/4", "chain ending last so far", "3 nodes"} {
		if !strings.Contains(s, want) {
			t.Errorf("status %q lacks %q", s, want)
		}
	}
	f := sectionModel(t, "demo-nightly-report", "timeline", 136, 40)
	if s := body(f); !strings.Contains(s, "chain ending last: 4 nodes") {
		t.Errorf("finished status %q", s)
	}
	h := m.Hints()
	for _, want := range []string{"tab section", "1-9 jump", "space fold", "i info", "l logs", "esc back"} {
		if !strings.Contains(h, want) {
			t.Errorf("hints %q lack %q", h, want)
		}
	}
	press(m, "i")
	if !strings.Contains(m.Hints(), "v reveal") {
		t.Errorf("hints with the panel open %q", m.Hints())
	}
}

// The raw view is the whole chart in plain text with ASCII glyphs in any skin.
func TestTimelineRawLines(t *testing.T) {
	m := sectionModel(t, "demo-nightly-report", "timeline", 60, 10)
	m.SetTheme(skin(t))
	raw := m.RawLines()
	if len(raw) != len(m.tl.rows)+2 {
		t.Fatalf("raw has %d lines, want %d", len(raw), len(m.tl.rows)+2)
	}
	if !strings.HasPrefix(raw[0], "* chain ending last") || !strings.HasPrefix(raw[1], "NAME") {
		t.Errorf("raw head %q / %q", raw[0], raw[1])
	}
	for _, l := range raw {
		if strings.ContainsAny(l, "█░◆\x1b") {
			t.Errorf("raw line has glyphs or escapes: %q", l)
		}
	}
}

// The timeline at 140 and 80 columns, with the info panel where each width puts it.
func TestTimelineGolden(t *testing.T) {
	workflows := []string{
		"demo-release-gate", "demo-nightly-report", "demo-train-pipeline",
		"demo-data-pull", "demo-deploy-multi-layer", "demo-param-check",
	}
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range workflows {
			m := sectionModel(t, name, "timeline", width-4, 18)
			b.WriteString("== " + name + "\n")
			b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		}
		m := sectionModel(t, "demo-nightly-report", "timeline", width-4, 30)
		for i, r := range m.tl.rows {
			if r.Row.DisplayName == "transform(1)" {
				m.tlCursor = i
			}
		}
		press(m, "i")
		b.WriteString("== demo-nightly-report, info on transform(1)\n")
		b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		golden(t, "timeline-"+strconv.Itoa(width), b.String())
	}
}

// The timeline shows folds and refreshes made while another section was on screen.
func TestTimelineFollowsChangesMadeElsewhere(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 136, 40)
	cursorTo(t, m, "transform")
	press(m, "space")
	press(m, "2")
	if r := tlRowNamed(t, m, "transform"); !r.Folded {
		t.Fatal("the nodes tab's fold is not on the timeline")
	}
	press(m, "1")
	press(m, "space")
	wf := demoWorkflow(t, "demo-nightly-report")
	n := wf.Nodes
	for id, node := range n {
		if node.DisplayName == "load" {
			node.Phase = "Running"
			n[id] = node
		}
	}
	m.SetWorkflow(wf, testkit.FixtureEpoch)
	press(m, "tab")
	if m.Section() != "timeline" {
		t.Fatalf("tab from nodes went to %q", m.Section())
	}
	if r := tlRowNamed(t, m, "transform"); r.Folded {
		t.Error("the fold opened on the nodes tab is still folded on the timeline")
	}
	if r := tlRowNamed(t, m, "load"); r.Row.Phase != "Running" {
		t.Errorf("the refresh did not reach the timeline: load is %s", r.Row.Phase)
	}
}
