package detail

import (
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// events.go draws the Events section: the Kubernetes events about the
// workflow and its pods, as a table.
//
//	AGE    TYPE       REASON              OBJECT          COUNT  MESSAGE
//	2m04s  ▲ Warning  WorkflowFailed      workflow            1  child 'transform' failed
//	2m04s  ▲ Warning  BackOff             transform(2)        3  Back-off restarting failed…
//	2m54s  ◇ Normal   Started             transform(2)        1  Started container main
//
// The type is a glyph and a word, so a warning reads without colour. A pod
// is named by its node's display name, the name the Nodes tab shows. The
// rows are built from the events the root has streamed in, matched against
// the pods the workflow's node map names, so an event arrives on screen as
// soon as the node it belongs to does.

// eventsCap bounds the events kept for one workflow. A pod that fails in a
// loop for hours can report thousands; the oldest go first.
const eventsCap = 2000

// eventRow is one line of the table.
type eventRow struct {
	ev core.Event
	// object is "workflow" for the workflow's own events and the node's
	// display name for a pod's.
	object string
}

// eventOrder is the table's order, toggled by s.
type eventOrder int

const (
	// eventsNewest lists the most recent event first.
	eventsNewest eventOrder = iota
	// eventsWarningsFirst lists warnings before normal events, each
	// newest first.
	eventsWarningsFirst
)

func (o eventOrder) String() string {
	if o == eventsWarningsFirst {
		return "warnings first"
	}
	return "newest first"
}

// Event type glyphs, one cell each, beside the type's word.
const (
	glyphEventWarning = "▲"
	glyphEventNormal  = "◇"
)

// eventType is the TYPE cell: glyph and word, or the glyph alone when the
// column has no room for the word.
func eventType(t string, word bool) string {
	g := glyphEventNormal
	if t == "Warning" {
		g = glyphEventWarning
	}
	if !word {
		return g
	}
	if t == "" {
		t = "Normal"
	}
	return g + " " + oneLine(t)
}

// candidateEvent reports whether an event may belong to the workflow: its
// own events, and pod events whose pod name starts with the workflow's
// name, which every pod name the controller gives does. The node map
// decides which of those pods are really the workflow's when rows are
// built; a pod the map does not name yet is kept until it does.
func candidateEvent(e core.Event, workflow string) bool {
	switch e.ObjectKind {
	case "Workflow":
		return e.ObjectName == workflow
	case "Pod":
		return strings.HasPrefix(e.ObjectName, workflow+"-")
	}
	return false
}

// buildEventRows turns the kept events into table rows: the workflow's
// events and those of the pods its node map names, narrowed by the filter
// and sorted. It also counts the kept pod events whose pod the map does not
// name.
func buildEventRows(events map[string]core.Event, workflow string, nodes map[string]core.Node, filter string, order eventOrder) (rows []eventRow, unmatched int) {
	pods := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.PodName != "" {
			pods[n.PodName] = nodeDisplay(n)
		}
	}
	q := strings.ToLower(strings.TrimSpace(filter))
	for _, e := range events {
		var object string
		switch {
		case e.ObjectKind == "Workflow" && e.ObjectName == workflow:
			object = "workflow"
		case e.ObjectKind == "Pod":
			name, ok := pods[e.ObjectName]
			if !ok {
				unmatched++
				continue
			}
			object = name
		default:
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Type+" "+e.Reason+" "+object+" "+e.Message), q) {
			continue
		}
		rows = append(rows, eventRow{ev: e, object: object})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i].ev, rows[j].ev
		if order == eventsWarningsFirst && (a.Type == "Warning") != (b.Type == "Warning") {
			return a.Type == "Warning"
		}
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.After(b.LastSeen)
		}
		if a.FirstSeen != b.FirstSeen {
			return a.FirstSeen.After(b.FirstSeen)
		}
		return a.UID < b.UID
	})
	return rows, unmatched
}

// eventColumns is the table's column set for one width. A zero width is
// unbounded: every column at its natural width, for the raw view.
type eventColumns struct {
	age, typ, reason, object, count int
	// typeWord shows the type's word beside its glyph.
	typeWord bool
	// message is the room left for the message; 0 means unbounded.
	message int
	// width is the whole line's room; 0 means unbounded.
	width int
}

const (
	eventAgeW      = 6
	eventCountW    = 5
	eventReasonW   = 24
	eventObjectW   = 28
	eventMidText   = 14
	eventMinText   = 8
	eventMinMsg    = 18
	eventGoodMsg   = 24
	eventColumnGap = 2
)

// eventColumnsFor fits the columns to width. The message is what a reader
// came for, so it keeps room first: the reason and object shrink to a middle
// width, then the count goes, then the type keeps its glyph without its
// word, and last the reason and object shrink to their minimum. Below that
// every line is cut at the width.
func eventColumnsFor(rows []eventRow, width int) eventColumns {
	c := eventColumns{age: eventAgeW, typ: cellWidth(eventType("Warning", true)), count: eventCountW, typeWord: true, width: width}
	reason, object := len("REASON"), len("OBJECT")
	for _, r := range rows {
		reason = max(reason, cellWidth(oneLine(r.ev.Reason)))
		object = max(object, cellWidth(oneLine(r.object)))
	}
	if width <= 0 {
		c.reason, c.object = reason, object
		return c
	}
	c.reason, c.object = min(reason, eventReasonW), min(object, eventObjectW)
	used := func() int {
		n := c.age + c.typ + c.reason + c.object + 4*eventColumnGap
		if c.count > 0 {
			n += c.count + eventColumnGap
		}
		return n
	}
	shrink := func(floor, msg int) {
		for used()+msg > width && (c.reason > floor || c.object > floor) {
			if c.reason >= c.object && c.reason > floor {
				c.reason--
			} else {
				c.object--
			}
		}
	}
	shrink(eventMidText, eventGoodMsg)
	if used()+eventGoodMsg > width {
		c.count = 0
	}
	if used()+eventGoodMsg > width {
		c.typ, c.typeWord = 1, false
	}
	shrink(eventMinText, eventMinMsg)
	c.message = max(width-used(), 1)
	return c
}

// eventRenderer draws rows for one column set and clock.
type eventRenderer struct {
	theme shared.Theme
	cols  eventColumns
	now   time.Time
}

// cell adds a padded, cut column and the gap after it.
func (er eventRenderer) cell(p *pieces, s string, w int, style lipgloss.Style, right bool) {
	if er.cols.message > 0 {
		s = truncCell(s, w)
	}
	pad := strings.Repeat(" ", max(w-cellWidth(s), 0))
	if right {
		p.add(pad+s, style)
	} else {
		p.add(s+pad, style)
	}
	p.add(strings.Repeat(" ", eventColumnGap), lipgloss.NewStyle())
}

// header is the column heads.
func (er eventRenderer) header() string {
	t := er.theme
	var p pieces
	typ := "TYPE"
	if !er.cols.typeWord {
		typ = "T"
	}
	er.cell(&p, "AGE", er.cols.age, t.TableHeader, false)
	er.cell(&p, typ, er.cols.typ, t.TableHeader, false)
	er.cell(&p, "REASON", er.cols.reason, t.TableHeader, false)
	er.cell(&p, "OBJECT", er.cols.object, t.TableHeader, false)
	if er.cols.count > 0 {
		er.cell(&p, "COUNT", er.cols.count, t.TableHeader, true)
	}
	p.add("MESSAGE", t.TableHeader)
	if er.cols.width > 0 {
		p = p.truncate(er.cols.width)
	}
	return p.trimRight().render()
}

// age is how long ago the event was last seen.
func (er eventRenderer) age(e core.Event) string {
	if e.LastSeen.IsZero() {
		return "-"
	}
	return shared.ShortDuration(er.now.Sub(e.LastSeen))
}

// render draws one row.
func (er eventRenderer) render(r eventRow) string {
	t := er.theme
	e := r.ev
	var p pieces
	typeStyle, msgStyle := t.Muted, t.Text
	if e.Type == "Warning" {
		typeStyle, msgStyle = t.Warning, t.Warning
	}
	er.cell(&p, er.age(e), er.cols.age, t.Muted, false)
	er.cell(&p, eventType(e.Type, er.cols.typeWord), er.cols.typ, typeStyle, false)
	er.cell(&p, oneLine(e.Reason), er.cols.reason, t.Text, false)
	object := oneLine(r.object)
	if er.cols.message > 0 {
		object = truncCellLeft(object, er.cols.object)
	}
	er.cell(&p, object, er.cols.object, t.Accent, false)
	if er.cols.count > 0 {
		er.cell(&p, itoaDetail(max(e.Count, 1)), er.cols.count, t.Muted, true)
	}
	p.add(oneLine(e.Message), msgStyle)
	if er.cols.width > 0 {
		p = p.truncate(er.cols.width)
	}
	return p.trimRight().render()
}

// truncCellLeft clips s to n cells from the left, marking the cut. Node
// names differ at their ends (train-shard(3:3), transform(2)), so a narrow
// OBJECT column keeps the end.
func truncCellLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cellWidth(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && cellWidth(string(r)) > n-1 {
		r = r[1:]
	}
	return "…" + string(r)
}
