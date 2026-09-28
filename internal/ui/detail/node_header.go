package detail

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// node_header.go draws the progress line above the node tree: how far the
// workflow is, what state its work is in, and how long it has taken.
//
//	progress 3/7 ████████·········  ✓ 2  ● 3  ○ 1  ✗ 1  elapsed 4m12s of ~9m

// phaseCount is one glyph of the header's tally.
type phaseCount struct {
	glyph string
	phase string // for the style; "" draws the glyph muted
	n     int
}

// countPhases tallies the nodes that do the work, by the glyph their row
// shows. Structural nodes (the DAG, Steps and Retry nodes and the groups)
// only hold other nodes, and counting them would count one piece of work
// twice.
func countPhases(nodes map[string]core.Node) []phaseCount {
	counts := []phaseCount{
		{glyph: shared.PhaseSymbol("Succeeded"), phase: "Succeeded"},
		{glyph: shared.PhaseSymbol("Running"), phase: "Running"},
		{glyph: shared.PhaseSymbol("Suspended"), phase: "Suspended"},
		{glyph: shared.PhaseSymbol("Pending"), phase: "Pending"},
		{glyph: shared.PhaseSymbol("Failed"), phase: "Failed"},
		{glyph: glyphSkipped},
		{glyph: shared.PhaseSymbol("?"), phase: "?"},
	}
	index := map[string]int{}
	for i, c := range counts {
		index[c.glyph] = i
	}
	for _, n := range nodes {
		switch n.Type {
		case "DAG", "Steps", "StepGroup", "Retry", "TaskGroup", "Container":
			continue
		}
		r := FlatRow{Row: OutlineRow{Type: n.Type, Phase: n.Phase}}
		counts[index[nodeGlyph(r)]].n++
	}
	out := counts[:0]
	for _, c := range counts {
		if c.n > 0 {
			out = append(out, c)
		}
	}
	return out
}

// progressLine is the header. Its parts are the progress with its bar, the
// tally, and the elapsed time with the estimate; the bar shrinks with the
// pane and the line is cut to width after it is laid out.
func progressLine(wf core.Workflow, now time.Time, width int, t shared.Theme) string {
	var p pieces
	sep := func() {
		if len(p) > 0 {
			p.add("  ", lipgloss.NewStyle())
		}
	}
	if done, total, ok := parseProgress(wf.Summary.Progress); ok {
		p.add("progress ", t.Muted)
		p.add(strconv.Itoa(done)+"/"+strconv.Itoa(total), t.Text)
		barW := 0
		switch {
		case width <= 0 || width >= tierWideBar:
			barW = 16
		case width >= infoMinWidth:
			barW = 10
		}
		if barW > 0 {
			running := 0
			for _, n := range wf.Nodes {
				if n.Type == "Pod" && n.Phase == "Running" {
					running++
				}
			}
			p.add(" ", lipgloss.NewStyle())
			for _, s := range progressBar(done, running, total, barW, barsASCII(t)) {
				p.add(s.text, s.kind.style(t))
			}
		}
	}
	for _, c := range countPhases(wf.Nodes) {
		sep()
		style := t.Muted
		if c.phase != "" {
			style = t.PhaseStyle(c.phase)
		}
		p.add(c.glyph, style)
		p.add(" "+strconv.Itoa(c.n), t.Text)
	}
	if e := elapsedText(wf.Summary, workflowEstimate(wf), now); e != "" {
		sep()
		label, rest, _ := strings.Cut(e, " ")
		p.add(label+" ", t.Muted)
		p.add(rest, t.Text)
	}
	if width > 0 {
		p = p.truncate(width)
	}
	return p.trimRight().render()
}

// elapsedText is the header's time: "elapsed 4m12s of ~9m" while the
// workflow runs, "took 11m00s" once it finished, and nothing before it
// starts. The estimate is the controller's, shown only when it sent one.
func elapsedText(s core.Summary, estimate time.Duration, now time.Time) string {
	if s.StartedAt == nil {
		return ""
	}
	if s.FinishedAt != nil {
		return "took " + shared.ShortDuration(s.FinishedAt.Sub(*s.StartedAt))
	}
	out := "elapsed " + shared.ShortDuration(now.Sub(*s.StartedAt))
	if estimate > 0 {
		out += " of ~" + humanDuration(estimate)
	}
	return out
}

// workflowEstimate is the controller's estimate for the whole run: the
// workflow's own, or else its root node's, which the controller fills from
// the same history.
func workflowEstimate(wf core.Workflow) time.Duration {
	if wf.Summary.EstimatedDuration > 0 {
		return wf.Summary.EstimatedDuration
	}
	for _, n := range wf.Nodes {
		if n.Name == wf.Summary.Ref.Name && n.EstimatedDuration > 0 {
			return n.EstimatedDuration
		}
	}
	return 0
}

// parseProgress reads the server's "done/total". Anything else, including a
// total of zero, is no progress to draw: the count is the controller's and
// is never made up here.
func parseProgress(s string) (done, total int, ok bool) {
	a, b, found := strings.Cut(strings.TrimSpace(s), "/")
	if !found {
		return 0, 0, false
	}
	d, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || n <= 0 || d < 0 {
		return 0, 0, false
	}
	return d, n, true
}
