package detail

import (
	"math"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The nodes tab's timing and progress bars are built as done, live and track
// segments and styled last, so tests read the geometry as text.

// timeSpan is the stretch of time the timing bars are drawn across.
type timeSpan struct {
	start, end time.Time
}

// ok reports whether the span has a start. A workflow whose nodes have no
// start time has nothing to place, and its bars draw as bare track.
func (s timeSpan) ok() bool { return !s.start.IsZero() }

// length is the span's duration, never negative.
func (s timeSpan) length() time.Duration {
	if d := s.end.Sub(s.start); d > 0 {
		return d
	}
	return 0
}

// workflowSpan is the span a workflow's bars share: from the earliest start
// (the workflow's or any node's, whichever is first) to the latest end. A
// workflow with no finish time is still going, so its span runs to now and a
// running node's bar reaches the right edge. The span never ends before it
// starts, whatever the clocks say.
func workflowSpan(wf core.Workflow, now time.Time) timeSpan {
	var s timeSpan
	extend := func(t *time.Time) {
		if t == nil || t.IsZero() {
			return
		}
		if s.start.IsZero() || t.Before(s.start) {
			s.start = *t
		}
		if t.After(s.end) {
			s.end = *t
		}
	}
	extend(wf.Summary.StartedAt)
	extend(wf.Summary.FinishedAt)
	running := wf.Summary.StartedAt != nil && wf.Summary.FinishedAt == nil
	for _, n := range wf.Nodes {
		extend(n.StartedAt)
		extend(n.FinishedAt)
		if n.StartedAt != nil && n.FinishedAt == nil && !phaseFinished(n.Phase) {
			running = true
		}
	}
	if !s.ok() {
		return timeSpan{}
	}
	if running && now.After(s.end) {
		s.end = now
	}
	return s
}

// phaseFinished reports whether a phase is final. A node in any other phase
// without a finish time is still running.
func phaseFinished(p string) bool {
	switch p {
	case "Succeeded", "Failed", "Error", "Skipped", "Omitted":
		return true
	default:
		return false
	}
}

// nodeInterval is when a node ran. ok is false for a node that has not
// started. A node still running ends now. A finish time before the start,
// which clock skew between the controller's replicas can produce, and a
// finished node with no finish time both count as zero-length at the start.
func nodeInterval(r OutlineRow, now time.Time) (start, end time.Time, running, ok bool) {
	if r.StartedAt == nil || r.StartedAt.IsZero() {
		return time.Time{}, time.Time{}, false, false
	}
	start, end = *r.StartedAt, *r.StartedAt
	switch {
	case r.FinishedAt != nil:
		end = *r.FinishedAt
	case !phaseFinished(r.Phase):
		running = true
		end = now
	}
	if end.Before(start) {
		end = start
	}
	return start, end, running, true
}

// nodeDuration is the DURATION column: how long the node ran, or has been
// running. ok is false for a node that has not started.
func nodeDuration(r OutlineRow, now time.Time) (time.Duration, bool) {
	s, e, _, ok := nodeInterval(r, now)
	if !ok {
		return 0, false
	}
	return e.Sub(s), true
}

// barKind is what a bar segment stands for; the theme maps each kind to one
// of its bar tokens.
type barKind int

const (
	barTrack   barKind = iota // the empty track (BarEmpty)
	barDone                   // finished work (BarFill)
	barLive                   // work still running (BarRunning)
	barFailed                 // work that failed (PhaseFailed)
	barGate                   // a gate waiting for a person (Warning)
	barWait                   // time a node waited before it started (BarEmpty)
	barBracket                // the span of a group of nodes (TreeGuide)
	barBlank                  // blank cells, unstyled
)

// barSeg is a run of bar cells of one kind.
type barSeg struct {
	text string
	kind barKind
}

// style maps a kind to its token. The bar tokens carry finished, running
// and empty; a failed bar and a waiting gate take the phase colours their
// row's glyph already has, so the bar and the glyph agree.
func (k barKind) style(t shared.Theme) lipgloss.Style {
	switch k {
	case barDone:
		return t.BarFill
	case barLive:
		return t.BarRunning
	case barFailed:
		return t.PhaseFailed
	case barGate:
		return t.Warning
	case barBracket:
		return t.TreeGuide
	case barBlank:
		return lipgloss.NewStyle()
	default:
		return t.BarEmpty
	}
}

// Glyphs. The eighth blocks fill a cell from the left, which gives a bar's
// end one-eighth-cell precision. A bar that starts inside a cell can only use
// the right half block or the right eighth, because Unicode has no finer
// right-aligned blocks. The ASCII set is for the plain theme, where no colour
// separates a bar from its track and a block would read the same as a gap.
const (
	glyphFull     = "█"
	glyphTrack    = "·"
	glyphRightHlf = "▐"
	glyphRight8th = "▕"
	asciiFill     = "="
	asciiHead     = ">"
	asciiTrack    = "-"
)

var leftEighths = [...]string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

// barEighths places the interval [start, end] on a bar of width cells, in
// eighths of a cell from the left edge. Positions outside the span clamp to
// its edges. A zero-length interval still gets one eighth, so a node that
// finished in no time is a visible tick rather than nothing. An empty span,
// where every node started and ended in the same instant, fills the bar.
func barEighths(span timeSpan, start, end time.Time, width int) (from, to int) {
	total := width * 8
	if total <= 0 {
		return 0, 0
	}
	d := span.length()
	if d <= 0 {
		return 0, total
	}
	pos := func(t time.Time) int {
		x := math.Round(float64(t.Sub(span.start)) / float64(d) * float64(total))
		switch {
		case x < 0:
			return 0
		case x > float64(total):
			return total
		}
		return int(x)
	}
	from, to = pos(start), pos(end)
	if to < from {
		to = from
	}
	if to == from {
		if from >= total {
			from = total - 1
		}
		to = from + 1
	}
	return from, to
}

// nodeBar draws one node's timing bar. A node that has not started, or a
// workflow with no span, is bare track.
func nodeBar(span timeSpan, r OutlineRow, now time.Time, width int, ascii bool) []barSeg {
	if width <= 0 {
		return nil
	}
	start, end, running, ok := nodeInterval(r, now)
	if !ok || !span.ok() {
		return intervalBar(width, 0, 0, barDone, ascii)
	}
	from, to := barEighths(span, start, end, width)
	kind := barDone
	if running {
		kind = barLive
	}
	return intervalBar(width, from, to, kind, ascii)
}

// intervalBar draws width cells with the interval [from, to), in eighths,
// filled with kind and the rest as track. from == to draws bare track.
func intervalBar(width, from, to int, kind barKind, ascii bool) []barSeg {
	cells := make([]string, width)
	kinds := make([]barKind, width)
	lastFill := -1
	for i := 0; i < width; i++ {
		lo, hi := i*8, i*8+8
		a, b := max(from, lo), min(to, hi)
		cov := b - a
		if cov <= 0 || from == to {
			cells[i], kinds[i] = glyphTrack, barTrack
			if ascii {
				cells[i] = asciiTrack
			}
			continue
		}
		kinds[i] = kind
		switch {
		case ascii:
			// A cell counts as filled when the bar covers most of it, and
			// the cell a bar starts in always does, so every started node
			// shows at least one mark.
			if cov >= 4 || (from >= lo && from < hi) {
				cells[i] = asciiFill
			} else {
				cells[i], kinds[i] = asciiTrack, barTrack
			}
		case cov == 8:
			cells[i] = glyphFull
		case a == lo || b < hi:
			cells[i] = leftEighths[cov]
		case cov >= 4:
			cells[i] = glyphRightHlf
		default:
			cells[i] = glyphRight8th
		}
		if kinds[i] != barTrack {
			lastFill = i
		}
	}
	if ascii && (kind == barLive || kind == barGate) && lastFill >= 0 {
		cells[lastFill] = asciiHead
	}
	return mergeCells(cells, kinds)
}

// progressBar draws done and running out of total as whole cells: done
// work first, running work next, then track. Whole cells keep the two kinds
// apart, since a cell can carry only one colour.
func progressBar(done, running, total, width int, ascii bool) []barSeg {
	if width <= 0 {
		return nil
	}
	if total <= 0 {
		return intervalBar(width, 0, 0, barDone, ascii)
	}
	done = min(max(done, 0), total)
	running = min(max(running, 0), total-done)
	d := int(math.Round(float64(done) / float64(total) * float64(width)))
	r := int(math.Round(float64(done+running)/float64(total)*float64(width))) - d
	if done > 0 && d == 0 {
		d = 1
	}
	if running > 0 && r == 0 && d < width {
		r = 1
	}
	if d+r > width {
		r = width - d
	}
	cells := make([]string, width)
	kinds := make([]barKind, width)
	fill, track := glyphFull, glyphTrack
	if ascii {
		fill, track = asciiFill, asciiTrack
	}
	for i := range cells {
		switch {
		case i < d:
			cells[i], kinds[i] = fill, barDone
		case i < d+r:
			cells[i], kinds[i] = fill, barLive
			if ascii && i == d+r-1 {
				cells[i] = asciiHead
			}
		default:
			cells[i], kinds[i] = track, barTrack
		}
	}
	return mergeCells(cells, kinds)
}

func mergeCells(cells []string, kinds []barKind) []barSeg {
	var out []barSeg
	for i, c := range cells {
		if n := len(out); n > 0 && out[n-1].kind == kinds[i] {
			out[n-1].text += c
			continue
		}
		out = append(out, barSeg{text: c, kind: kinds[i]})
	}
	return out
}

// barsASCII reports whether a theme draws bars in ASCII: the plain theme,
// and the zero theme the unstyled renders use.
func barsASCII(t shared.Theme) bool {
	return t.Skin == "plain" || t.Skin == ""
}
