package detail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The template, bar, duration and message columns come and go with the pane width.
func TestColumnsForWidth(t *testing.T) {
	cases := []struct {
		width                       int
		template, bar, dur, message bool
	}{
		{160, true, true, true, true},
		{136, true, true, true, true},
		{120, false, true, true, true},
		{96, false, true, true, true},
		{80, false, true, true, true},
		{76, false, true, true, true},
		{70, false, false, true, true},
		{56, false, false, true, true},
		{34, false, false, true, false},
		{20, false, false, false, false},
	}
	for _, tc := range cases {
		c := columnsFor(tc.width, 30, 12)
		got := [4]bool{c.template > 0, c.bar > 0, c.duration > 0, c.message > 0}
		want := [4]bool{tc.template, tc.bar, tc.dur, tc.message}
		if got != want {
			t.Errorf("width %d: template/bar/duration/message = %v, want %v (%+v)", tc.width, got, want, c)
		}
		total := c.name
		for _, w := range []int{c.template, c.duration, c.bar, c.message} {
			if w > 0 {
				total += colGap + w
			}
		}
		if total > tc.width {
			t.Errorf("width %d: columns add up to %d", tc.width, total)
		}
	}
	// The bar grows with a wide pane, within a bound.
	if a, b := columnsFor(136, 30, 0).bar, columnsFor(176, 30, 0).bar; b <= a {
		t.Errorf("bar at 176 (%d) is not wider than at 136 (%d)", b, a)
	}
	if got := columnsFor(400, 30, 0).bar; got > 40 {
		t.Errorf("bar at 400 is %d cells", got)
	}
	// Unbounded, for the raw view: every column at its natural width.
	c := columnsFor(0, 50, 30)
	if c.name != 50 || c.template != rawTemplateW || c.bar != rawBarW || c.message >= 0 {
		t.Errorf("unbounded columns = %+v", c)
	}
}

// The name gives way last: the dependencies go first, then the type tag, and the name keeps a minimum.
func TestNameCellGivesWayInOrder(t *testing.T) {
	rr := rowRenderer{theme: shared.NewTheme(true)}
	row := FlatRow{Indent: "├─ ", Row: OutlineRow{
		DisplayName: "evaluate-the-model", Type: "DAG", Phase: "Running",
		Deps: []string{"train-shard(0:0)", "train-shard(1:1)", "train-shard(2:2)"},
	}}
	full := rr.nameCell(row, 200, false).plain()
	if full != "├─ ● evaluate-the-model DAG ← train-shard(0:0), train-shard(1:1) +1" {
		t.Fatalf("full cell = %q", full)
	}
	// The annotation steps down through shorter forms before it goes.
	if got := rr.nameCell(row, 50, false).plain(); got != "├─ ● evaluate-the-model DAG ← train-shard(0:0) +2" {
		t.Errorf("50-cell cell = %q", got)
	}
	if got := rr.nameCell(row, 40, false).plain(); got != "├─ ● evaluate-the-model DAG ← 3 tasks" {
		t.Errorf("40-cell cell = %q", got)
	}
	if got := rr.nameCell(row, 30, false).plain(); got != "├─ ● evaluate-the-model DAG" {
		t.Errorf("narrow cell = %q, want the annotation dropped first", got)
	}
	if got := rr.nameCell(row, 24, false).plain(); got != "├─ ● evaluate-the-model" {
		t.Errorf("narrower cell = %q, want the tag dropped next", got)
	}
	got := rr.nameCell(row, 8, false).plain()
	name := strings.TrimPrefix(got, "├─ ● ")
	if cellWidth(name) < minNameText {
		t.Errorf("name cut to %q, below the %d-cell minimum", name, minNameText)
	}
}

// A row carries its type tag, role, retry count and a failing exit code; exit 0 is not news.
func TestRowTagsAndFacts(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 200, 40)
	lines := map[string]string{}
	for _, r := range m.nodes {
		lines[r.Row.DisplayName] = m.renderer(200, m.theme).render(r, false, false)
	}
	checks := []struct {
		row, has, hasNot string
	}{
		{"demo-nightly-report", "demo-nightly-report DAG", ""},
		{"extract", "", "Pod"},
		{"extract", "", "exit 0"},
		{"transform", "transform Retry ↻ 2 ← extract", ""},
		{"load", "load ← transform", ""},
		{"onExit", "onExit exit handler", "Pod"},
	}
	for _, c := range checks {
		line, ok := lines[c.row]
		if !ok {
			t.Fatalf("no row %q in %v", c.row, lines)
		}
		if c.has != "" && !strings.Contains(line, c.has) {
			t.Errorf("%s: %q lacks %q", c.row, line, c.has)
		}
		if c.hasNot != "" && strings.Contains(line, c.hasNot) {
			t.Errorf("%s: %q has %q", c.row, line, c.hasNot)
		}
	}
	oom := demoModel(t, "demo-oom-backfill", 200, 40)
	var found bool
	for _, r := range oom.nodes {
		if strings.Contains(oom.renderer(200, oom.theme).render(r, false, false), "backfill-2026-q2 exit 137") {
			found = true
		}
	}
	if !found {
		t.Error("the OOM-killed pod does not state exit 137")
	}
}

// The gate says it waits for a person at every width with a message column.
func TestGateRowSaysAwaitingResume(t *testing.T) {
	for _, w := range []int{160, 120, 80, 60} {
		m := demoModel(t, "demo-release-gate", w, 30)
		var line string
		for _, r := range m.nodes {
			if r.Suspended() {
				line = m.renderer(w, m.theme).render(r, false, false)
			}
		}
		if !strings.Contains(line, "AWAITING RESUME") {
			t.Errorf("width %d: gate row %q", w, line)
		}
		if !strings.Contains(line, "◐ approve-production") {
			t.Errorf("width %d: gate row lost its glyph: %q", w, line)
		}
	}
}

// A running node's duration is its elapsed time on the injected clock; an unstarted one has none.
func TestDurationColumn(t *testing.T) {
	m := demoModel(t, "demo-train-pipeline", 120, 40)
	want := map[string]string{
		"preprocess":       "1m10s",
		"train-shard(2:2)": "2m40s", // started at 1m20s, clock at 4m
		"train-shard(5:5)": "",
	}
	for _, r := range m.nodes {
		w, ok := want[r.Row.DisplayName]
		if !ok {
			continue
		}
		line := m.renderer(120, m.theme).render(r, false, false)
		c := m.renderer(120, m.theme).cols
		cell := strings.TrimSpace(sliceCells(line, c.name+colGap, c.duration))
		if cell != w {
			t.Errorf("%s: duration %q, want %q (%q)", r.Row.DisplayName, cell, w, line)
		}
	}
	m.SetNow(testkit.FixtureEpoch.Add(60 * 1e9))
	for _, r := range m.nodes {
		if r.Row.DisplayName == "train-shard(2:2)" {
			line := m.renderer(120, m.theme).render(r, false, false)
			if !strings.Contains(line, "3m40s") {
				t.Errorf("elapsed did not follow the clock: %q", line)
			}
		}
	}
}

// sliceCells returns n cells of s starting at cell from.
func sliceCells(s string, from, n int) string {
	return ansi.Cut(s, from, from+n)
}

// The column heads line up with the columns and give the bar its scale.
func TestColumnHeads(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 140, 40)
	rr := m.renderer(140, m.theme)
	h := rr.header()
	for _, want := range []string{"NAME", "TEMPLATE", "DURATION", "TIMELINE", "4m00s", "MESSAGE"} {
		if !strings.Contains(h, want) {
			t.Errorf("heads %q lack %q", h, want)
		}
	}
	if at := strings.Index(h, "TEMPLATE"); at != rr.cols.name+colGap {
		t.Errorf("TEMPLATE at %d, column at %d", at, rr.cols.name+colGap)
	}
}

// The progress header shows the progress, the tally by glyph and the time with its estimate.
func TestProgressHeader(t *testing.T) {
	plain := shared.NewTheme(true)
	train := demoWorkflow(t, "demo-train-pipeline")
	got := progressLine(train, testkit.FixtureEpoch, 120, plain)
	for _, want := range []string{"progress 3/8 ", "✓ 3", "● 3", "○ 2", "elapsed 4m00s of ~9m"} {
		if !strings.Contains(got, want) {
			t.Errorf("train header %q lacks %q", got, want)
		}
	}
	if !strings.Contains(got, "=") {
		t.Errorf("train header has no bar: %q", got)
	}
	nightly := progressLine(demoWorkflow(t, "demo-nightly-report"), testkit.FixtureEpoch, 120, plain)
	for _, want := range []string{"✗ 3", glyphSkipped + " 1", "took 4m00s"} {
		if !strings.Contains(nightly, want) {
			t.Errorf("nightly header %q lacks %q", nightly, want)
		}
	}
	gate := progressLine(demoWorkflow(t, "demo-release-gate"), testkit.FixtureEpoch, 120, plain)
	if !strings.Contains(gate, "◐ 1") {
		t.Errorf("gate header %q does not count the gate", gate)
	}
	// No progress from the server, no progress drawn; an unstarted workflow has no time.
	pending := progressLine(demoWorkflow(t, "demo-cleanup"), testkit.FixtureEpoch, 120, plain)
	if strings.Contains(pending, "progress") || strings.Contains(pending, "elapsed") {
		t.Errorf("pending header %q", pending)
	}
	if w := cellWidth(progressLine(train, testkit.FixtureEpoch, 30, plain)); w > 30 {
		t.Errorf("header is %d cells at 30", w)
	}
}
