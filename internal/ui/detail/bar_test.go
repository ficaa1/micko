package detail

import (
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// sec is a time on a 100-second test span.
func sec(s int) *time.Time {
	t := testkit.FixtureEpoch.Add(time.Duration(s) * time.Second)
	return &t
}

var testSpan = timeSpan{start: *sec(0), end: *sec(100)}

// barText is the bar's plain text.
func barText(segs []barSeg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

func timedRow(phase string, start, end *time.Time) OutlineRow {
	return OutlineRow{NodeID: "n", Type: "Pod", Phase: phase, StartedAt: start, FinishedAt: end}
}

// The bar places a node where it started and as long as it ran, on the
// workflow's span: 10s to 30s of 100s is cells 1 and 2 of 10.
func TestNodeBarPlacesTheNodeOnTheSpan(t *testing.T) {
	cases := []struct {
		name      string
		row       OutlineRow
		want      string
		asciiWant string
		wantKind  barKind
	}{
		{"whole cells", timedRow("Succeeded", sec(10), sec(30)), "·██·······", "-==-------", barDone},
		{"half cells", timedRow("Succeeded", sec(5), sec(15)), "▐▌········", "==--------", barDone},
		{"eighths at the end", timedRow("Succeeded", sec(0), sec(3)), "▎·········", "=---------", barDone},
		{"from the start to the end", timedRow("Succeeded", sec(0), sec(100)), "██████████", "==========", barDone},
		{"zero length is a tick", timedRow("Succeeded", sec(40), sec(40)), "····▏·····", "----=-----", barDone},
		{"zero length at the very end", timedRow("Succeeded", sec(100), sec(100)), "·········▕", "---------=", barDone},
		{"clock skew counts as zero length", timedRow("Succeeded", sec(40), sec(20)), "····▏·····", "----=-----", barDone},
		{"not started is bare track", timedRow("Pending", nil, nil), "··········", "----------", barTrack},
		{"before the span clamps to its start", timedRow("Succeeded", sec(-50), sec(10)), "█·········", "=---------", barDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := *sec(100)
			if got := barText(nodeBar(testSpan, tc.row, now, 10, false)); got != tc.want {
				t.Errorf("bar = %q, want %q", got, tc.want)
			}
			if got := barText(nodeBar(testSpan, tc.row, now, 10, true)); got != tc.asciiWant {
				t.Errorf("ascii bar = %q, want %q", got, tc.asciiWant)
			}
			segs := nodeBar(testSpan, tc.row, now, 10, false)
			found := false
			for _, s := range segs {
				if s.kind == tc.wantKind {
					found = true
				}
			}
			if !found {
				t.Errorf("no %v segment in %+v", tc.wantKind, segs)
			}
		})
	}
}

// A running node's bar runs to now in the running kind, and its ASCII form
// ends in a head, because the plain theme has no colour to say "running".
func TestNodeBarForARunningNode(t *testing.T) {
	row := timedRow("Running", sec(50), nil)
	now := *sec(80)
	segs := nodeBar(testSpan, row, now, 10, false)
	if got := barText(segs); got != "·····███··" {
		t.Fatalf("bar = %q", got)
	}
	for _, s := range segs {
		if s.kind == barDone {
			t.Fatalf("a running node drew a finished segment: %+v", segs)
		}
	}
	if got := barText(nodeBar(testSpan, row, now, 10, true)); got != "-----==>--" {
		t.Fatalf("ascii bar = %q", got)
	}
	// A running node with no finish time ends now, not at the span's end.
	d, ok := nodeDuration(row, now)
	if !ok || d != 30*time.Second {
		t.Fatalf("duration = %v %v, want 30s", d, ok)
	}
}

// With no span (no node has started) every bar is bare track. A span of no
// length, where everything started and ended in one instant, fills the bar.
func TestNodeBarDegenerateSpans(t *testing.T) {
	row := timedRow("Succeeded", sec(0), sec(0))
	if got := barText(nodeBar(timeSpan{}, row, *sec(0), 5, false)); got != "·····" {
		t.Errorf("no span: %q", got)
	}
	flat := timeSpan{start: *sec(0), end: *sec(0)}
	if got := barText(nodeBar(flat, row, *sec(0), 5, false)); got != "█████" {
		t.Errorf("empty span: %q", got)
	}
	if got := nodeBar(testSpan, row, *sec(0), 0, false); got != nil {
		t.Errorf("zero width: %+v", got)
	}
}

// The span runs from the earliest start to the workflow's end, and to now
// while the workflow runs. A node that started before the workflow widens
// it; a clock that reads earlier than the last finish does not shrink it.
func TestWorkflowSpan(t *testing.T) {
	wf := core.Workflow{
		Summary: core.Summary{StartedAt: sec(10), FinishedAt: sec(60)},
		Nodes: map[string]core.Node{
			"a": {ID: "a", Phase: "Succeeded", StartedAt: sec(5), FinishedAt: sec(20)},
		},
	}
	s := workflowSpan(wf, *sec(500))
	if !s.start.Equal(*sec(5)) || !s.end.Equal(*sec(60)) {
		t.Errorf("finished span = %v..%v", s.start, s.end)
	}

	wf.Summary.FinishedAt = nil
	wf.Nodes["b"] = core.Node{ID: "b", Phase: "Running", StartedAt: sec(30)}
	s = workflowSpan(wf, *sec(90))
	if !s.end.Equal(*sec(90)) {
		t.Errorf("running span ends %v, want now", s.end)
	}
	s = workflowSpan(wf, *sec(15))
	if !s.end.Equal(*sec(30)) {
		t.Errorf("a clock behind the data shrank the span to %v", s.end)
	}

	if s := workflowSpan(core.Workflow{}, *sec(0)); s.ok() {
		t.Errorf("a workflow with no times has a span: %+v", s)
	}
}

// The header's bar shows done work, then running work, then the rest, in
// whole cells; a non-zero count is never rounded away.
func TestProgressBar(t *testing.T) {
	cases := []struct {
		done, running, total int
		want, ascii          string
	}{
		{3, 2, 8, "██████····", "=====>----"},
		{0, 0, 5, "··········", "----------"},
		{5, 0, 5, "██████████", "=========="},
		{1, 1, 100, "██········", "=>--------"},
		{9, 9, 10, "██████████", "=========>"},
	}
	for _, tc := range cases {
		segs := progressBar(tc.done, tc.running, tc.total, 10, false)
		if got := barText(segs); got != tc.want {
			t.Errorf("%d+%d/%d = %q, want %q", tc.done, tc.running, tc.total, got, tc.want)
		}
		if got := barText(progressBar(tc.done, tc.running, tc.total, 10, true)); got != tc.ascii {
			t.Errorf("%d+%d/%d ascii = %q, want %q", tc.done, tc.running, tc.total, got, tc.ascii)
		}
	}
	// Done and running are told apart by kind, so each takes its colour.
	segs := progressBar(3, 2, 8, 10, false)
	if len(segs) != 3 || segs[0].kind != barDone || segs[1].kind != barLive || segs[2].kind != barTrack {
		t.Errorf("segments = %+v", segs)
	}
}
