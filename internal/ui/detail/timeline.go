package detail

import (
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// timeline.go builds and draws the Timeline section: a Gantt chart of the
// workflow's work against a time axis.
//
//	NAME                         DURATION   0        1m       2m       3m  4m00s
//	▾ ✗ demo-nightly-report DAG     4m00s   ├──────────────────────────────────┤
//	├─ ✓ extract                      50s ◆ ▕███████▎···························
//	├▾ ✗ transform Retry            2m30s           ░├─────────────────────┤
//	│  ├─ ✗ transform(0)              40s ◆ ·········██████·····················
//	│  ├─ ✗ transform(1)              45s ◆ ···············░▕██████▌············
//	│  └─ ✗ transform(2)              50s ◆ ·······················░▕███████▏···
//	└─ ⊘ load                               ····································
//	  ✓ onExit exit handler           20s   ································▐██▎
//
// The rows are the node tree in pipeline order. Pods and suspend gates are
// bars; DAG, Steps, Retry and TaskGroup nodes only group them, so they are
// drawn as a bracket over the time their group took. The stretch between the
// moment a node could have started and the moment it did is shaded, which is
// where a step waited: for the controller, for a retry's backoff, or for a
// slot. ◆ marks the critical path. While the workflow runs, the chart ends
// at a now line. Bar geometry is the nodes tab's, so a node sits at the same
// place in both sections.

// structuralType reports whether a node type only groups other nodes. Such
// a node does no work of its own, so the timeline draws it as a heading.
func structuralType(t string) bool {
	switch t {
	case "DAG", "Steps", "StepGroup", "Retry", "TaskGroup":
		return true
	default:
		return false
	}
}

// tlRow is one line of the chart.
type tlRow struct {
	FlatRow
	// heading marks a structural node, drawn as the bracket of its group.
	heading bool
	// ready is when the node could have started: when the last node it
	// waited for finished, or when its parent started. Zero when unknown.
	ready time.Time
	// critical marks a node on the critical path, and a folded row that
	// hides one.
	critical bool
}

// timeline is the chart's content for one workflow at one instant.
type timeline struct {
	rows []tlRow
	// running reports that the workflow is still going, so the chart ends
	// at now and draws the now line.
	running bool
	// leftOut counts the skipped nodes the chart does not draw: a condition
	// kept them from running, so they have no time to place.
	leftOut int
	// folds and foldedNodes count the folded rows drawn and the rows they
	// hide.
	folds, foldedNodes int
	// path is the critical path, in the order it ran.
	path []string
}

// buildTimeline lays the workflow out for the chart. folded holds the node
// IDs whose subtrees are folded away, shared with the nodes tab.
func buildTimeline(out Outline, wf core.Workflow, folded map[string]bool, now time.Time) timeline {
	var tl timeline
	if !out.Available {
		return tl
	}
	tree, left := timelineTree(out)
	tl.leftOut = left
	tl.running = workflowRunning(wf)

	idx := indexTimeline(tree.Rows, now)
	tl.path = idx.criticalPath()
	onPath := make(map[string]bool, len(tl.path))
	for _, id := range tl.path {
		onPath[id] = true
		for p := idx.parent[id]; p != nil; p = idx.parent[p.NodeID] {
			idx.hidesCritical[p.NodeID] = true
		}
	}

	f := Flatten(tree, FlattenOptions{Folded: folded})
	tl.folds, tl.foldedNodes = f.Folds, f.FoldedNodes
	tl.rows = make([]tlRow, len(f.Rows))
	for i, r := range f.Rows {
		row := tlRow{FlatRow: r, heading: r.Section == "" && structuralType(r.Row.Type)}
		row.ready = idx.readyAt(r.Row.NodeID)
		row.critical = onPath[r.Row.NodeID] || (r.Folded && idx.hidesCritical[r.Row.NodeID])
		tl.rows[i] = row
	}
	return tl
}

// workflowRunning reports whether the workflow is still going: it has no
// finish time, its phase is not final, and it or one of its nodes started.
func workflowRunning(wf core.Workflow) bool {
	s := wf.Summary
	if s.FinishedAt != nil || phaseFinished(s.Phase) {
		return false
	}
	if s.StartedAt != nil {
		return true
	}
	for _, n := range wf.Nodes {
		if n.StartedAt != nil {
			return true
		}
	}
	return false
}

// timelineTree copies the outline in pipeline order and leaves out every
// subtree in which nothing ran because a condition skipped it. A skipped
// node that holds work which did run stays, as the heading of that work.
// It returns the copy and how many nodes it left out.
func timelineTree(out Outline) (Outline, int) {
	left := 0
	var copyRow func(r OutlineRow) (OutlineRow, bool)
	copyRow = func(r OutlineRow) (OutlineRow, bool) {
		all := rowSkipped(r)
		kids := make([]OutlineRow, 0, len(r.Children))
		var dropped []int
		for _, c := range r.Children {
			cc, skipped := copyRow(c)
			if !skipped {
				all = false
			}
			kids = append(kids, cc)
			if skipped {
				dropped = append(dropped, len(kids)-1)
			}
		}
		if all {
			// The whole subtree goes; its size is counted by the caller.
			r.Children = nil
			return r, true
		}
		keep := kids[:0]
		d := 0
		for i, c := range kids {
			if d < len(dropped) && dropped[d] == i {
				d++
				left += 1 + countRows(r.Children[i].Children)
				continue
			}
			keep = append(keep, c)
		}
		sort.SliceStable(keep, func(i, j int) bool { return keep[i].Seq < keep[j].Seq })
		r.Children = keep
		return r, false
	}
	t := Outline{Available: true, Dangling: out.Dangling, Unreachable: out.Unreachable}
	for _, r := range out.Rows {
		c, skipped := copyRow(r)
		if skipped {
			left += 1 + countRows(r.Children)
			continue
		}
		t.Rows = append(t.Rows, c)
	}
	sort.SliceStable(t.Rows, func(i, j int) bool { return t.Rows[i].Seq < t.Rows[j].Seq })
	return t, left
}

// tlIndex indexes the timeline tree by node ID for the ready times and the
// critical path.
type tlIndex struct {
	now    time.Time
	rows   map[string]*OutlineRow
	parent map[string]*OutlineRow
	// roots are the top-level trees; main is the workflow's own, the first.
	roots []*OutlineRow
	// hidesCritical marks the ancestors of the nodes on the critical path,
	// so a fold that hides one can still show the marker.
	hidesCritical map[string]bool
}

func indexTimeline(roots []OutlineRow, now time.Time) *tlIndex {
	x := &tlIndex{
		now:           now,
		rows:          map[string]*OutlineRow{},
		parent:        map[string]*OutlineRow{},
		hidesCritical: map[string]bool{},
	}
	var walk func(r *OutlineRow)
	walk = func(r *OutlineRow) {
		x.rows[r.NodeID] = r
		for i := range r.Children {
			c := &r.Children[i]
			x.parent[c.NodeID] = r
			walk(c)
		}
	}
	for i := range roots {
		x.roots = append(x.roots, &roots[i])
		walk(&roots[i])
	}
	return x
}

// interval is when a node ran, with a running node ending now.
func (x *tlIndex) interval(r *OutlineRow) (start, end time.Time, ok bool) {
	s, e, _, ok := nodeInterval(*r, x.now)
	return s, e, ok
}

// latestFinished is the node among ids that finished last, which is the one
// that let the waiting node go. Ties go to the first in pipeline order.
// Nodes the chart does not draw, and nodes that never started, are ignored.
func (x *tlIndex) latestFinished(ids []string) *OutlineRow {
	var best *OutlineRow
	var bestEnd time.Time
	for _, id := range ids {
		r, ok := x.rows[id]
		if !ok {
			continue
		}
		if _, end, ok := x.interval(r); ok && (best == nil || end.After(bestEnd)) {
			best, bestEnd = r, end
		}
	}
	return best
}

// predecessor is the node whose end let r start: the last to finish of the
// nodes r waited for; for a node that waited for none, its parent's
// predecessor, since it started with its parent. A top-level tree beside
// the workflow's own, such as the exit handler, runs once the workflow's
// own tree is done.
func (x *tlIndex) predecessor(r *OutlineRow) *OutlineRow {
	for r != nil {
		if p := x.latestFinished(r.After); p != nil {
			return p
		}
		if p := x.parent[r.NodeID]; p != nil {
			r = p
			continue
		}
		if r.Role != "" && len(x.roots) > 0 && x.roots[0] != r {
			return x.roots[0]
		}
		return nil
	}
	return nil
}

// readyAt is when a node could have started: when its predecessor
// finished, or, for a node that waited for nothing, when its parent
// started. Zero when neither is known.
func (x *tlIndex) readyAt(id string) time.Time {
	r, ok := x.rows[id]
	if !ok {
		return time.Time{}
	}
	if p := x.latestFinished(r.After); p != nil {
		_, end, _ := x.interval(p)
		return end
	}
	if p := x.parent[id]; p != nil {
		if s, _, ok := x.interval(p); ok {
			return s
		}
	}
	if r.Role != "" && len(x.roots) > 0 && x.roots[0] != r {
		if _, end, ok := x.interval(x.roots[0]); ok {
			return end
		}
	}
	return time.Time{}
}

// lastWork descends from r to the piece of work that finished last inside
// it: r itself when r does work, otherwise the child that finished last,
// repeatedly. A group none of whose members started holds no work, and the
// result is nil.
func (x *tlIndex) lastWork(r *OutlineRow) *OutlineRow {
	for r != nil {
		if !structuralType(r.Type) {
			return r
		}
		var best *OutlineRow
		var bestEnd time.Time
		for i := range r.Children {
			c := &r.Children[i]
			if _, end, ok := x.interval(c); ok && (best == nil || end.After(bestEnd)) {
				best, bestEnd = c, end
			}
		}
		r = best
	}
	return nil
}

// criticalPath is the chain of work that set the workflow's end time. It
// starts at the piece of work that finished last and walks back: each step
// is the work that finished last inside the node the current one waited
// for. Any delay on this chain delays the whole run, and no delay anywhere
// else does. While the workflow runs, the chain ends at the work still
// going that has the latest end, which is now. The result is in run order.
func (x *tlIndex) criticalPath() []string {
	var target *OutlineRow
	var targetEnd time.Time
	for _, r := range x.roots {
		if _, end, ok := x.interval(r); ok && (target == nil || end.After(targetEnd)) {
			target, targetEnd = r, end
		}
	}
	var path []string
	seen := map[string]bool{}
	for cur := x.lastWork(target); cur != nil && !seen[cur.NodeID]; {
		if _, _, ok := x.interval(cur); !ok {
			break
		}
		seen[cur.NodeID] = true
		path = append(path, cur.NodeID)
		cur = x.lastWork(x.predecessor(cur))
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// Axis. The tick step is the smallest round step whose labels fit the chart
// with room between them, so the axis reads in seconds for a quick run and
// in hours or days for a long one.
var axisSteps = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour, 2 * 24 * time.Hour, 7 * 24 * time.Hour,
}

// axisLabelGap is the fewest blank cells between two tick labels.
const axisLabelGap = 2

// axisStep picks the tick step for a span drawn across width cells, and 0
// when no step fits.
func axisStep(span time.Duration, width int) time.Duration {
	if span <= 0 || width <= 0 {
		return 0
	}
	for _, step := range axisSteps {
		n := int(span / step)
		if n > width {
			continue
		}
		need := 0
		for i := 0; i <= n; i++ {
			need = max(need, cellWidth(tickLabel(time.Duration(i)*step)))
		}
		if float64(width)*float64(step)/float64(span) >= float64(need+axisLabelGap) {
			return step
		}
	}
	return 0
}

// tickLabel writes an offset from the start compactly: the units from the
// largest to the smallest that is not zero, the later ones in two digits as
// shortDuration writes them. 0, 45s, 2m, 1m30s, 1h, 1h05m, 2d, 1d12h.
func tickLabel(d time.Duration) string {
	if d < time.Second {
		return "0"
	}
	s := int64(d / time.Second)
	units := []struct {
		n    int64
		unit string
	}{{s / 86400, "d"}, {s % 86400 / 3600, "h"}, {s % 3600 / 60, "m"}, {s % 60, "s"}}
	first, last := -1, -1
	for i, u := range units {
		if u.n != 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	var b strings.Builder
	for i := first; i <= last; i++ {
		if i == first {
			b.WriteString(itoaDetail(int(units[i].n)))
		} else {
			b.WriteString(padZero(units[i].n))
		}
		b.WriteString(units[i].unit)
	}
	return b.String()
}

func padZero(n int64) string {
	if n < 10 {
		return "0" + itoaDetail(int(n))
	}
	return itoaDetail(int(n))
}

// axisTick is one label on the axis, at a cell of the chart.
type axisTick struct {
	col   int
	label string
}

// axisTicks places the tick labels on a chart of width cells, whose axis
// line is lineW cells: the chart and the now line after it, if any. The end
// carries its own label, end, right-aligned to the line; ticks that would
// run into it or into each other are left out.
func axisTicks(span timeSpan, width, lineW int, end string) []axisTick {
	if width <= 0 {
		return nil
	}
	var out []axisTick
	endCol := lineW - cellWidth(end)
	if endCol < 0 {
		return nil
	}
	d := span.length()
	if step := axisStep(d, width); step > 0 && span.ok() {
		next := 0
		for t := time.Duration(0); t < d; t += step {
			from, _ := barEighths(span, span.start.Add(t), span.start.Add(t), width)
			col := from / 8
			label := tickLabel(t)
			if col < next || col+cellWidth(label)+1 > endCol {
				continue
			}
			out = append(out, axisTick{col: col, label: label})
			next = col + cellWidth(label) + axisLabelGap
		}
	}
	return append(out, axisTick{col: endCol, label: end})
}

// axisLine draws the tick labels as one line of width cells.
func axisLine(ticks []axisTick, width int) string {
	buf := []rune(strings.Repeat(" ", width))
	for _, t := range ticks {
		for i, r := range []rune(t.label) {
			if t.col+i < width && t.col+i >= 0 {
				buf[t.col+i] = r
			}
		}
	}
	return strings.TrimRight(string(buf), " ")
}

// Glyphs of the chart beyond the nodes tab's bars.
const (
	glyphWait      = "░"
	glyphCritical  = "◆"
	glyphNow       = "│"
	glyphBracketL  = "├"
	glyphBracketR  = "┤"
	glyphBracket   = "─"
	glyphBracket1  = "│"
	asciiWait      = "~"
	asciiCritical  = "*"
	asciiNow       = "|"
	asciiBracketLR = "|"
	asciiBracket   = "-"
)

// barKindFor is the bar kind a node's phase draws in.
func barKindFor(r OutlineRow, running bool) barKind {
	switch {
	case r.Type == "Suspend" && r.Phase == "Running":
		return barGate
	case r.Phase == "Failed" || r.Phase == "Error":
		return barFailed
	case running:
		return barLive
	default:
		return barDone
	}
}

// ganttBar draws a work node's bar with the time it waited before it: the
// wait from ready to the start is shaded, the run is the nodes tab's bar
// geometry in the node's phase, and the rest is track. A cell shared by the
// wait and the run goes to the run.
func ganttBar(span timeSpan, r OutlineRow, ready time.Time, now time.Time, width int, ascii bool) []barSeg {
	if width <= 0 {
		return nil
	}
	start, end, running, ok := nodeInterval(r, now)
	if !ok || !span.ok() {
		return intervalBar(width, 0, 0, barDone, ascii)
	}
	from, to := barEighths(span, start, end, width)
	segs := intervalBar(width, from, to, barKindFor(r, running), ascii)
	if ready.IsZero() || !ready.Before(start) {
		return segs
	}
	wf, _ := barEighths(span, ready, ready, width)
	if wf >= from {
		return segs
	}
	cells, kinds := splitCells(segs)
	wait := glyphWait
	if ascii {
		wait = asciiWait
	}
	for i := range cells {
		lo, hi := i*8, i*8+8
		// A cell is waiting when the wait covers at least half of it and
		// the run does not reach into it.
		cov := min(from, hi) - max(wf, lo)
		if kinds[i] == barTrack && cov >= 4 {
			cells[i], kinds[i] = wait, barWait
		}
	}
	return mergeCells(cells, kinds)
}

// headingBar draws a structural node as a bracket over the time its group
// took, with the time it waited shaded before it. The rest of the line is
// blank, so a heading never reads as a bar.
func headingBar(span timeSpan, r OutlineRow, ready time.Time, now time.Time, width int, ascii bool) []barSeg {
	if width <= 0 {
		return nil
	}
	cells := make([]string, width)
	kinds := make([]barKind, width)
	for i := range cells {
		cells[i], kinds[i] = " ", barBlank
	}
	start, end, running, ok := nodeInterval(r, now)
	if ok && span.ok() {
		from, to := barEighths(span, start, end, width)
		a, b := from/8, (to-1)/8
		l, rr, mid, one := glyphBracketL, glyphBracketR, glyphBracket, glyphBracket1
		if ascii {
			l, rr, mid, one = asciiBracketLR, asciiBracketLR, asciiBracket, asciiBracketLR
		}
		if running {
			rr = mid
		}
		for i := a; i <= b && i < width; i++ {
			kinds[i] = barBracket
			switch {
			case a == b:
				cells[i] = one
			case i == a:
				cells[i] = l
			case i == b:
				cells[i] = rr
			default:
				cells[i] = mid
			}
		}
		if !ready.IsZero() && ready.Before(start) {
			wf, _ := barEighths(span, ready, ready, width)
			wait := glyphWait
			if ascii {
				wait = asciiWait
			}
			for i := wf / 8; i < a; i++ {
				cells[i], kinds[i] = wait, barWait
			}
		}
	}
	return mergeCells(cells, kinds)
}

// splitCells turns bar segments back into one glyph and one kind per cell.
func splitCells(segs []barSeg) ([]string, []barKind) {
	var cells []string
	var kinds []barKind
	for _, s := range segs {
		for _, r := range s.text {
			cells = append(cells, string(r))
			kinds = append(kinds, s.kind)
		}
	}
	return cells, kinds
}

// Chart columns.
const (
	// tlMinChart is the narrowest chart worth drawing.
	tlMinChart = 12
	// tlDropDuration is the chart width below which the duration column
	// gives its room to the chart.
	tlDropDuration = 24
	// tlMinName is the narrowest the name column gets.
	tlMinName = 16
	// tlRawChart is the chart width of the raw full-screen view.
	tlRawChart = 60
)

// tlColumns is the chart's column set for one pane width. A zero width is
// unbounded, for the raw full-screen view.
type tlColumns struct {
	width    int
	name     int
	duration int
	chart    int
	// now draws the now line after the chart.
	now bool
}

// tlColumnsFor lays the columns out: the name column takes what its rows
// need up to two fifths of the pane, the duration stays while the chart
// keeps tlDropDuration cells, and the chart takes the rest.
func tlColumnsFor(width, nameNeed int, running bool) tlColumns {
	c := tlColumns{width: width, duration: durationW, now: running}
	if width <= 0 {
		c.name = nameNeed
		c.chart = tlRawChart
		return c
	}
	nowW := 0
	if running {
		nowW = 1
	}
	c.name = min(nameNeed, max(tlMinName, width*2/5))
	// name, gap, duration, space, marker, space, chart, now
	chart := func() int {
		n := width - c.name - colGap - 2 - nowW
		if c.duration > 0 {
			n -= c.duration + 1
		}
		return n
	}
	if chart() < tlDropDuration {
		c.duration = 0
	}
	if chart() < tlMinChart {
		c.name = max(width-colGap-2-nowW-tlMinChart, minNameText)
	}
	c.chart = max(chart(), 0)
	return c
}

// tlRenderer draws the chart's lines for one layout.
type tlRenderer struct {
	theme shared.Theme
	cols  tlColumns
	span  timeSpan
	now   time.Time
}

func (tr tlRenderer) ascii() bool { return barsASCII(tr.theme) }

// nameCellWidth is the width a row's name cell needs to show everything.
func tlNameCellWidth(r tlRow) int {
	w := cellWidth(r.Indent) + 2 + cellWidth(oneLine(rowDisplayName(r.Row)))
	if tag := nodeTag(r.FlatRow); tag != "" {
		w += 1 + cellWidth(oneLine(tag))
	}
	if r.Folded {
		w += 1 + len("+") + len(itoaDetail(r.FoldedCount))
	}
	return w
}

// nameCell draws the tree, the phase glyph, the name, the type tag and the
// fold count, cut to width. The type tag gives way first, then the name,
// which keeps at least minNameText cells.
func (tr tlRenderer) nameCell(r tlRow) pieces {
	t := tr.theme
	rr := rowRenderer{theme: t}
	name := oneLine(rowDisplayName(r.Row))
	var extras []nameExtra
	over := 0
	if w := tr.cols.name; w > 0 {
		over = tlNameCellWidth(r) - w
	}
	if tag := nodeTag(r.FlatRow); tag != "" {
		if over > 0 {
			over -= 1 + cellWidth(oneLine(tag))
		} else {
			extras = append(extras, nameExtra{text: oneLine(tag), style: t.Muted})
		}
	}
	if r.Folded {
		extras = append(extras, nameExtra{text: "+" + itoaDetail(r.FoldedCount), style: t.Accent})
	}
	if over > 0 {
		name = truncCell(name, max(cellWidth(name)-over, minNameText))
	}
	var p pieces
	rr.addIndent(&p, r.FlatRow)
	p.add(nodeGlyph(r.FlatRow)+" ", rr.glyphStyle(r.FlatRow))
	style := t.Text
	switch {
	case r.critical:
		style = t.Accent
	case r.heading || r.Skipped():
		style = t.Muted
	}
	p.add(name, style)
	for _, e := range extras {
		p.add(" ", lipgloss.NewStyle())
		p.add(e.text, e.style)
	}
	if w := tr.cols.name; w > 0 && p.width() > w {
		p = p.truncate(w)
	}
	return p
}

// line builds one row of the chart as pieces, cut to the pane.
func (tr tlRenderer) line(r tlRow) pieces {
	t := tr.theme
	c := tr.cols
	plain := lipgloss.NewStyle()
	p := tr.nameCell(r)
	p.padTo(c.name)
	if c.duration > 0 {
		p.add(strings.Repeat(" ", colGap), plain)
		txt := ""
		style := t.Muted
		if r.Section == "" {
			if d, ok := nodeDuration(r.Row, tr.now); ok {
				txt = shortDuration(d)
				if _, _, running, _ := nodeInterval(r.Row, tr.now); running && !r.heading {
					style = t.PhaseRunning
				}
			}
		}
		p.add(strings.Repeat(" ", c.duration-cellWidth(txt))+txt, style)
	}
	if c.chart > 0 {
		p.add(" ", plain)
		if r.critical {
			mark := glyphCritical
			if tr.ascii() {
				mark = asciiCritical
			}
			p.add(mark, t.Accent)
		} else {
			p.add(" ", plain)
		}
		p.add(" ", plain)
		var segs []barSeg
		switch {
		case r.Section != "":
			segs = []barSeg{{text: strings.Repeat(" ", c.chart), kind: barBlank}}
		case r.heading:
			segs = headingBar(tr.span, r.Row, r.ready, tr.now, c.chart, tr.ascii())
		default:
			segs = ganttBar(tr.span, r.Row, r.ready, tr.now, c.chart, tr.ascii())
		}
		for _, s := range segs {
			p.add(s.text, s.kind.style(t))
		}
		if c.now {
			mark := glyphNow
			if tr.ascii() {
				mark = asciiNow
			}
			p.add(mark, t.Accent)
		}
	}
	if c.width > 0 {
		p = p.truncate(c.width)
	}
	return p
}

// render draws a row. The selected row is one bar across the pane in the
// selection style, so its pieces give up their own colours.
func (tr tlRenderer) render(r tlRow, selected bool) string {
	p := tr.line(r).trimRight()
	if selected {
		return tr.theme.SelectRow(p.plain(), tr.cols.width)
	}
	return p.render()
}

// header is the column heads and the axis: tick labels over the chart, the
// end labelled "now" while the workflow runs and with its length once it
// has finished.
func (tr tlRenderer) header() string {
	t := tr.theme
	c := tr.cols
	var p pieces
	p.add(padCell("NAME", c.name), t.TableHeader)
	if c.duration > 0 {
		p.add(strings.Repeat(" ", colGap)+padCell("", c.duration-cellWidth("DURATION"))+"DURATION", t.TableHeader)
	}
	if c.chart > 0 {
		p.add("   ", lipgloss.NewStyle())
		w := c.chart
		end := ""
		switch {
		case !tr.span.ok():
		case c.now:
			end = "now"
			w++
		default:
			end = shortDuration(tr.span.length())
		}
		p.add(axisLine(axisTicks(tr.span, c.chart, w, end), w), t.Muted)
	}
	if c.width > 0 {
		p = p.truncate(c.width)
	}
	return p.trimRight().render()
}
