package detail

import (
	"strconv"
	"strings"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// timeline_tab.go is the Timeline section's state: its cursor, its keys, and
// how the axis, the chart, the status line and the info panel share the
// pane. Folds are the nodes tab's: both sections draw the same tree, so a
// group folded in one is folded in the other.

// timelineChromeRows is the tab strip and the status line.
const timelineChromeRows = 2

// timelineChanged is called whenever the tree or its folds change. The
// chart is laid out again at once while it is on screen, and otherwise when
// the reader next opens it, so a fold on a large nodes tab does not pay for
// a chart nobody is looking at.
func (m *Model) timelineChanged() {
	if m.tab == "timeline" {
		m.rebuildTimeline(m.tlSelectedID())
		return
	}
	m.tlStale = true
}

// showSection makes id the active section, laying the chart out first if
// it went stale while another section was on screen.
func (m *Model) showSection(id string) {
	m.tab = id
	if id == "timeline" && m.tlStale {
		m.rebuildTimeline(m.tlSelectedID())
	}
}

// rebuildTimeline lays the chart out again from the outline and the folds.
// keep is the node the cursor should stay on; when it is empty or gone the
// cursor keeps its index, clamped to the new rows.
func (m *Model) rebuildTimeline(keep string) {
	m.tlStale = false
	m.tl = buildTimeline(m.state.Outline, m.workflow(), m.folded, m.now)
	m.tlNameNeed = 0
	for _, r := range m.tl.rows {
		m.tlNameNeed = max(m.tlNameNeed, tlNameCellWidth(r))
	}
	if keep != "" {
		for i, r := range m.tl.rows {
			if r.Row.NodeID == keep {
				m.tlCursor = i
				break
			}
		}
	}
	if m.tlCursor >= len(m.tl.rows) {
		m.tlCursor = len(m.tl.rows) - 1
	}
	if m.tlCursor < 0 {
		m.tlCursor = 0
	}
}

// tlSelectedID is the node ID under the timeline's cursor, or "".
func (m *Model) tlSelectedID() string {
	if r, ok := m.tlCursorRow(); ok {
		return r.Row.NodeID
	}
	return ""
}

func (m *Model) tlCursorRow() (tlRow, bool) {
	if m.tlCursor < 0 || m.tlCursor >= len(m.tl.rows) {
		return tlRow{}, false
	}
	return m.tl.rows[m.tlCursor], true
}

// tlFlatRows is the chart's rows as the tree they draw, for the fold keys.
func (m *Model) tlFlatRows() []FlatRow {
	out := make([]FlatRow, len(m.tl.rows))
	for i, r := range m.tl.rows {
		out[i] = r.FlatRow
	}
	return out
}

// handleTimelineKey handles the keys only the timeline binds: the fold
// keys, as on the nodes tab, and the info panel.
func (m *Model) handleTimelineKey(key string) {
	switch key {
	case "space", "left", "right":
		if id, fold, ok := treeFoldKey(key, m.tlFlatRows(), &m.tlCursor); ok {
			m.setFold(id, fold)
		}
	case "i":
		m.showInfo = !m.showInfo
	}
}

// timelineLayout is how the timeline splits its pane.
func (m *Model) timelineLayout() nodesLayout {
	return m.treeLayout(timelineChromeRows, len(m.tl.rows), 3)
}

// tlRenderer lays the chart out for a width.
func (m *Model) tlRenderer(width int, theme shared.Theme) tlRenderer {
	return tlRenderer{
		theme: theme,
		cols:  tlColumnsFor(width, m.tlNameNeed, m.tl.running),
		span:  workflowSpan(m.workflow(), m.now),
		now:   m.now,
	}
}

// timelineBody is the section under the tab strip: the status line, then
// the axis and the chart, with the info panel beside or below them.
func (m *Model) timelineBody() []string {
	l := m.timelineLayout()
	return m.withInfoPanel([]string{m.timelineStatusLine()}, m.timelineLines(l), l)
}

// timelineLines renders the axis and exactly the rows that fit, scrolled to
// keep the cursor visible.
func (m *Model) timelineLines(l nodesLayout) []string {
	if msg, ok := m.timelineEmpty(); ok {
		if m.width > 0 {
			msg = truncCell(msg, m.width)
		}
		return []string{msg}
	}
	h := l.treeRows
	m.tlTop = windowTop(m.tlTop, m.tlCursor, h, len(m.tl.rows))
	end := min(m.tlTop+h, len(m.tl.rows))
	tr := m.tlRenderer(l.treeW, m.theme)
	out := make([]string, 0, end-m.tlTop+1)
	if l.colHead {
		out = append(out, tr.header())
	}
	for i := m.tlTop; i < end; i++ {
		out = append(out, tr.render(m.tl.rows[i], i == m.tlCursor))
	}
	return out
}

// timelineEmpty is the line the section shows when there is no chart to
// draw, and false when there is one.
func (m *Model) timelineEmpty() (string, bool) {
	switch {
	case !m.state.Outline.Available:
		reason := m.state.Outline.UnavailableReason
		if reason == "" {
			return "the server did not send node status for this workflow", true
		}
		return shared.Sanitize(reason), true
	case len(m.tl.rows) == 0 && m.tl.leftOut > 0:
		return "(every node was skipped — nothing ran to place on the timeline)", true
	case len(m.tl.rows) == 0 && phaseFinished(m.state.Summary.Phase):
		return "(no nodes — the workflow ended before any node ran)", true
	case len(m.tl.rows) == 0:
		return "(no nodes yet — workflow not started)", true
	}
	return "", false
}

// windowTop is the first visible row of a window of h rows over n rows that
// keeps the cursor in view, moving the previous top only as far as it must.
func windowTop(top, cursor, h, n int) int {
	if cursor < top {
		top = cursor
	}
	if cursor >= top+h {
		top = cursor - h + 1
	}
	if max := n - h; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	return top
}

// criticalNote describes the marked dependency chain, with its node count.
func (m *Model) criticalNote() string {
	n := len(m.tl.path)
	if n == 0 {
		return ""
	}
	mark := glyphCritical
	if barsASCII(m.theme) {
		mark = asciiCritical
	}
	nodes := plural(n, "node")
	if m.tl.running {
		return mark + " chain ending last so far: " + nodes
	}
	return mark + " chain ending last: " + nodes
}

// timelineStatusLine returns row counts, cursor position and the marked chain's meaning.
func (m *Model) timelineStatusLine() string {
	t := m.theme
	if !m.state.Outline.Available {
		return t.Dim.Render("node status unavailable")
	}
	parts := []string{plural(len(m.tl.rows), "row")}
	if m.tl.leftOut > 0 {
		parts = append(parts, strconv.Itoa(m.tl.leftOut)+" skipped not drawn")
	}
	if m.tl.folds > 0 {
		parts = append(parts, strconv.Itoa(m.tl.foldedNodes)+" in "+plural(m.tl.folds, "fold"))
	}
	if m.showInfo && infoPlacementFor(m.width) == infoHidden {
		parts = append(parts, "no room for info")
	}
	if len(m.tl.rows) > 0 {
		parts = append(parts, strconv.Itoa(m.tlCursor+1)+"/"+strconv.Itoa(len(m.tl.rows)))
	}
	if r, ok := m.tlCursorRow(); ok && r.Row.HasPod && r.Row.PodName != "" {
		parts = append(parts, "l logs")
	}
	if note := m.criticalNote(); note != "" {
		parts = append(parts, note)
	}
	s := strings.Join(parts, " · ")
	if m.width > 0 {
		s = truncCell(s, m.width)
	}
	return t.Dim.Render(s)
}

// timelineHints is the footer for the timeline.
func (m *Model) timelineHints() string {
	h := []string{"tab section", "1-9 jump", "space fold"}
	if m.showInfo {
		h = append(h, "i hide info", "v reveal")
	} else {
		h = append(h, "i info")
	}
	h = append(h, "l logs", "a actions", "r refresh", "f raw", "esc back")
	return strings.Join(h, "  ")
}

// timelineRawLines is the section as plain text for the full-screen view
// and the clipboard: what the marker means, the axis and every row, with
// ASCII bars.
func (m *Model) timelineRawLines() []string {
	if msg, ok := m.timelineEmpty(); ok {
		return []string{msg}
	}
	plain := shared.NewTheme(true)
	var out []string
	if note := m.criticalNote(); note != "" {
		out = append(out, strings.Replace(note, glyphCritical, asciiCritical, 1))
	}
	tr := m.tlRenderer(0, plain)
	out = append(out, tr.header())
	for _, r := range m.tl.rows {
		out = append(out, tr.render(r, false))
	}
	return out
}
