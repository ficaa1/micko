package detail

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// events_tab.go is the Events section's state: the events the root has
// streamed in, the order and filter, the stream's status and the scroll
// position. The section never opens a stream itself. It says it wants one
// (EventsWanted); the root streams, hands the events over (ApplyEvents)
// and reports how the stream is doing (SetEventsStatus).

// eventsState is the Events section's state for the workflow on screen.
type eventsState struct {
	// byUID holds the events kept for the workflow, keyed by UID, so an
	// event the stream sends again (a new count, a reconnect's replay)
	// replaces itself.
	byUID map[string]core.Event
	// rows is the table as last built; stale marks it out of date.
	rows      []eventRow
	unmatched int
	// Cached widths keep layout work independent of the number of stored rows.
	reasonW, objectW int
	stale            bool
	order            eventOrder
	// editing is the filter input open; filter is its text.
	editing bool
	filter  string
	// status says how the stream is doing; problem marks a status the
	// reader must see (stopped, retrying).
	status  string
	problem bool
	top     int
}

// EventsIntent is the stream the Events section wants: the workflow's
// events and its pods'.
type EventsIntent struct {
	Ref core.Ref
}

// EventsWanted reports the stream the Events section wants, and false when
// it wants none: another section is on screen or no workflow is loaded.
func (m *Model) EventsWanted() (EventsIntent, bool) {
	if m.tab != "events" || !m.loaded || m.loading || m.notFound || m.lastErr != "" {
		return EventsIntent{}, false
	}
	return EventsIntent{Ref: m.state.Summary.Ref}, true
}

// ApplyEvents keeps candidate events up to eventsCap, discarding deletions and the oldest excess.
func (m *Model) ApplyEvents(evs []core.Event) {
	if !m.loaded {
		return
	}
	if m.ev.byUID == nil {
		m.ev.byUID = map[string]core.Event{}
	}
	name := m.state.Summary.Ref.Name
	changed := false
	for _, e := range evs {
		if !candidateEvent(e, name) {
			continue
		}
		changed = true
		key := e.UID
		if key == "" {
			key = e.ObjectKind + "/" + e.ObjectName + "/" + e.Reason + "/" + e.FirstSeen.String()
		}
		if e.Deleted {
			delete(m.ev.byUID, key)
			continue
		}
		m.ev.byUID[key] = e
	}
	if over := len(m.ev.byUID) - eventsCap; over > 0 {
		keys := make([]string, 0, len(m.ev.byUID))
		for k := range m.ev.byUID {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return m.ev.byUID[keys[i]].LastSeen.Before(m.ev.byUID[keys[j]].LastSeen)
		})
		for _, k := range keys[:over] {
			delete(m.ev.byUID, k)
		}
	}
	if changed {
		m.ev.stale = true
	}
}

// SetEventsStatus records how the stream is doing. problem marks a status
// the reader must act on or wait for: a stopped or reconnecting stream.
func (m *Model) SetEventsStatus(status string, problem bool) {
	m.ev.status, m.ev.problem = status, problem
}

// eventRows is the table, built again when the events, the node map, the
// filter or the order changed.
func (m *Model) eventRows() []eventRow {
	if m.ev.stale || m.ev.rows == nil {
		m.ev.rows, m.ev.unmatched = buildEventRows(m.ev.byUID, m.state.Summary.Ref.Name, m.nodeMap, m.ev.filter, m.ev.order)
		if m.ev.rows == nil {
			m.ev.rows = []eventRow{}
		}
		m.ev.reasonW, m.ev.objectW = eventTextWidths(m.ev.rows)
		m.ev.stale = false
	}
	return m.ev.rows
}

// handleEventsKey handles the keys only the Events section binds: s for the
// order and / for the filter.
func (m *Model) handleEventsKey(key string) {
	switch key {
	case "s":
		if m.ev.order == eventsNewest {
			m.ev.order = eventsWarningsFirst
		} else {
			m.ev.order = eventsNewest
		}
		m.ev.stale = true
		m.ev.top = 0
	case "/":
		m.ev.editing = true
	}
}

// handleEventsFilterKey edits the filter. Printable keys type and narrow
// the table as they go, backspace deletes, enter keeps the filter and esc
// drops it.
func (m *Model) handleEventsFilterKey(key string) {
	switch key {
	case "esc":
		m.ev.editing, m.ev.filter = false, ""
	case "enter":
		m.ev.editing = false
		m.ev.filter = strings.TrimSpace(m.ev.filter)
	case "backspace":
		if r := []rune(m.ev.filter); len(r) > 0 {
			m.ev.filter = string(r[:len(r)-1])
		}
	default:
		ch, ok := printableKey(key)
		if !ok {
			return
		}
		m.ev.filter += ch
	}
	m.ev.stale = true
	m.ev.top = 0
}

// eventsFilterActive reports whether esc belongs to the filter: it is open,
// or a kept filter narrows the table.
func (m *Model) eventsFilterActive() bool {
	return m.tab == "events" && (m.ev.editing || m.ev.filter != "")
}

// eventRenderer returns the renderer for the current rows and pane width.
func (m *Model) eventRenderer(width int, theme shared.Theme) eventRenderer {
	m.eventRows()
	return eventRenderer{theme: theme, cols: eventColumnsFor(m.ev.reasonW, m.ev.objectW, width), now: m.now}
}

// eventsLineCount returns the row count plus a header or empty-state line.
func (m *Model) eventsLineCount() int { return len(m.eventRows()) + 1 }

// eventsLines returns at most h rendered lines starting at top.
func (m *Model) eventsLines(top, h int) []string {
	rows := m.eventRows()
	if len(rows) == 0 {
		msg := m.eventsEmpty()
		if m.width > 0 {
			msg = truncCell(msg, m.width)
		}
		return sliceLines([]string{m.theme.Muted.Render(msg)}, top, h)
	}
	n := len(rows) + 1
	top = min(max(top, 0), n)
	end := min(max(top+h, top), n)
	er := m.eventRenderer(m.width, m.theme)
	out := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		if i == 0 {
			out = append(out, er.header())
			continue
		}
		out = append(out, er.render(rows[i-1]))
	}
	return out
}

// eventsEmpty says why the table is empty.
func (m *Model) eventsEmpty() string {
	switch {
	case m.ev.filter != "":
		return "(no event matches /" + oneLine(m.ev.filter) + ")"
	case len(m.ev.byUID) == 0 && m.ev.problem:
		return "(no events: " + oneLine(m.ev.status) + ")"
	case len(m.ev.byUID) == 0 && m.ev.status == "":
		return "(connecting to the event stream…)"
	default:
		return "(no events: Kubernetes keeps events for about an hour, so a workflow that ended earlier may have none)"
	}
}

// eventsRawLines is the table as plain text for the full-screen view and
// the clipboard: every column whole, no styling.
func (m *Model) eventsRawLines() []string {
	rows := m.eventRows()
	if len(rows) == 0 {
		return []string{m.eventsEmpty()}
	}
	er := m.eventRenderer(0, shared.NewTheme(true))
	out := []string{er.header()}
	for _, r := range rows {
		out = append(out, er.render(r))
	}
	return out
}

// eventsStatusLine says what the table shows: how many events and how many
// warnings, the order, the filter, the stream's status, the pod events it
// holds back and the scroll position. While the filter is open it is the
// filter input.
func (m *Model) eventsStatusLine() string {
	t := m.theme
	rows := m.eventRows()
	if m.ev.editing {
		var p pieces
		p.add("filter /", t.Accent)
		p.add(oneLine(m.ev.filter), t.Text)
		p.add("▏", t.Accent)
		p.add("  "+plural(len(rows), "event"), t.Muted)
		if m.width > 0 {
			p = p.truncate(m.width)
		}
		return p.render()
	}
	warnings := 0
	for _, r := range rows {
		if r.ev.Type == "Warning" {
			warnings++
		}
	}
	parts := []string{"events", plural(len(rows), "event")}
	if m.ev.filter != "" {
		parts = append(parts, "/"+oneLine(m.ev.filter))
	}
	if warnings > 0 {
		parts = append(parts, plural(warnings, "warning"))
	}
	parts = append(parts, m.ev.order.String())
	if m.ev.status != "" {
		parts = append(parts, oneLine(m.ev.status))
	}
	if !m.podNamesKnown() {
		parts = append(parts, "pod events hidden: the server did not name the pods")
	}
	if n := m.scrollLines(); n > m.viewRows() {
		parts = append(parts, strconv.Itoa(m.ev.top+1)+"/"+strconv.Itoa(n))
	}
	s := strings.Join(parts, " · ")
	if m.width > 0 {
		s = truncCell(s, m.width)
	}
	if m.ev.problem {
		return t.Warning.Render(s)
	}
	return t.Dim.Render(s)
}

// podNamesKnown reports whether the node map names any pod, or has no pod
// node to name. Without names no pod event can be matched to its node.
func (m *Model) podNamesKnown() bool {
	pods := false
	for _, n := range m.nodeMap {
		if n.PodName != "" {
			return true
		}
		pods = pods || nodeTypeHasPod(n.Type)
	}
	return !pods
}

// eventsHints is the footer for the Events section.
func (m *Model) eventsHints() string {
	if m.ev.editing {
		return "type to filter  enter keep  esc clear"
	}
	h := []string{"tab section", "1-9 jump", "j/k scroll", "s " + otherOrder(m.ev.order), "/ filter"}
	if m.ev.filter != "" {
		h = append(h, "esc clear filter")
	} else {
		h = append(h, "esc back")
	}
	h = append(h, "y copy", "f raw", "r refresh")
	return strings.Join(h, "  ")
}

// otherOrder names the order s switches to.
func otherOrder(o eventOrder) string {
	if o == eventsNewest {
		return "warnings first"
	}
	return "newest first"
}
