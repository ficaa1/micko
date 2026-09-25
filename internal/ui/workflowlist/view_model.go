package workflowlist

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// View renders the full list pane. Layout (plan §2):
//
//	header:  <title>  ns:<ns>  READ ONLY
//	toolbar: Search: <q> (snapshot scope)  Phase: <bucket>  Sort: <key>  Updated/stale
//	table:   NAME  PHASE  AGE  DURATION  (+ message column when wide)
//	footer:  states + key hints
//
// All server-derived text passes shared.Sanitize before render (SEC-01/02).
// Statuses are distinguishable by text, never by color alone (UI-03/07).
func (m *Model) View() string {
	return m.ViewAt(time.Time{})
}

// ViewAt renders with an explicit clock (tests inject FixtureEpoch; the
// root passes its injected clock time in production code paths).
func (m *Model) ViewAt(now time.Time) string {
	if m.width > 0 && m.width < minUsableWidth {
		return m.resizeNotice()
	}
	// Standalone composition: the pane title, the body, a blank separator and
	// the pane's own footer. Under the shell (BodyLines) the title rides the
	// border and the footer is a shell band, so neither is repeated here.
	lines := append([]string{m.headerView()}, m.frameLines(now, 3)...)
	lines = append(lines, "", m.Hints()+"  ? help"+m.footerPosition())
	if m.height > 0 {
		lines = shared.ClampLines(lines, m.height)
	}
	return strings.Join(lines, "\n")
}

// PaneTitle is the shell border title. It names what the pane holds; the
// namespace and the connection state belong to the shell's own bands.
func (m *Model) PaneTitle() string { return "Workflows" }

// BodyLines renders the pane content for the shell: toolbar, column heads
// and the visible row window, with no title and no footer.
//
// Below the minimum usable width the whole pane becomes the resize notice:
// a 40-column table is not a degraded table, it is unreadable.
func (m *Model) BodyLines(now time.Time) []string {
	if m.width > 0 && m.width < minUsableWidth {
		return strings.Split(strings.TrimRight(m.resizeNotice(), "\n"), "\n")
	}
	lines := m.frameLines(now, 0)
	if m.height > 0 {
		lines = shared.ClampLines(lines, m.height)
	}
	return lines
}

// WindowStatus reports the visible row range for the shell footer, so a
// windowed list is distinguishable from one that simply has few workflows.
// It reflects the most recent BodyLines/ViewAt call, which is the render
// pass that decided the window.
func (m *Model) WindowStatus() string {
	if m.winEnd-m.winStart <= 0 || m.winEnd-m.winStart >= len(m.rows) {
		return itoa(len(m.rows)) + " shown"
	}
	return itoa(m.winStart+1) + "-" + itoa(m.winEnd) + "/" + itoa(len(m.rows))
}

// Hints are the list key contract, mirrored by the `?` overlay.
func (m *Model) Hints() string {
	return "enter open  l logs  T timeline  X explain  E events  / search  s sort  p phase  space mark  a actions  n namespace  0 all ns  r refresh  w wide"
}

// footerPosition is the window indicator appended to the standalone footer.
func (m *Model) footerPosition() string {
	if m.winEnd-m.winStart <= 0 || m.winEnd-m.winStart >= len(m.rows) {
		return ""
	}
	return "  " + itoa(m.winStart+1) + "-" + itoa(m.winEnd) + "/" + itoa(len(m.rows))
}

// frameLines assembles the pane body as discrete terminal lines so the height
// budget can be enforced on whole lines. reserve is the number of lines the
// caller will add below the body (the standalone footer and its separator);
// the shell adds none.
func (m *Model) frameLines(now time.Time, reserve int) []string {
	lines := strings.Split(m.toolbarView(), "\n")

	cols := m.columns()
	var head strings.Builder
	head.WriteString(strings.Repeat(" ", markGutter))
	for i, c := range cols {
		if i > 0 {
			head.WriteString(strings.Repeat(" ", colGap))
		}
		title := truncateRight(c.key.title(), c.width)
		switch {
		case i == len(cols)-1:
			// The last column is not padded out. A row does not pad its
			// message either, so padding only the head would put a run of
			// trailing spaces on one line of every screen, which a mouse
			// selection then copies.
			head.WriteString(title)
		case c.key == colAge:
			head.WriteString(padLeft(title, c.width))
		default:
			head.WriteString(padRight(title, c.width))
		}
	}
	lines = append(lines, m.theme.TableHeader.Render(head.String()))

	if len(m.rows) == 0 {
		m.winStart, m.winEnd = 0, 0
		return append(lines, m.emptyStateView())
	}

	start, end := m.window(len(lines) + reserve)
	m.winStart, m.winEnd = start, end
	for i := start; i < end; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.selIndex(), now))
	}
	return lines
}

// window returns the half-open row range to render. chromeLines is the number
// of lines already emitted above the rows plus any the caller reserves below,
// all deducted before the rows get their budget.
//
// The window is anchored on scrollTop and only moves far enough to keep the
// selected row inside it, so paging feels stable rather than jumping.
func (m *Model) window(chromeLines int) (start, end int) {
	if m.height <= 0 {
		m.scrollTop = 0
		return 0, len(m.rows)
	}
	budget := m.height - chromeLines
	if budget >= len(m.rows) {
		m.scrollTop = 0
		return 0, len(m.rows)
	}
	if budget < 1 {
		budget = 1 // ClampLines trims the rest; never render zero rows
	}
	start = m.scrollTop
	if max := len(m.rows) - budget; start > max {
		start = max
	}
	if start < 0 {
		start = 0
	}
	if sel := m.selIndex(); sel >= 0 {
		if sel < start {
			start = sel
		}
		if sel >= start+budget {
			start = sel - budget + 1
		}
	}
	m.scrollTop = start
	return start, start + budget
}

// rowLine renders one table row. The phase cell carries the phase colour,
// the name and namespace keep the ordinary text colour so they read first,
// and the times, the metadata and the message are muted. A marked row is
// drawn in the Marked style instead, and the selected row is one highlight
// bar: it must win so the selection is never ambiguous, and its glyph and
// phase word still say what the colour would have.
func (m *Model) rowLine(r core.Summary, selected bool, now time.Time) string {
	// Symbol AND word AND color: a mono terminal, NO_COLOR and a color-blind
	// reader all keep two of the three channels (UI-03/07). A workflow parked
	// on a manual gate reads "Suspended" rather than "Running", because
	// "Running" would hide the one row that is waiting for the reader.
	shown := DisplayPhase(r)
	// The mark gutter carries the mark as a glyph, so a marked row is
	// distinguishable without colour; the Marked style is the second
	// channel on top of it.
	gutter := strings.Repeat(" ", markGutter)
	marked := m.marks[r.Ref.UID]
	if marked {
		gutter = markGlyph + " "
	}
	// plain is the row as text, for the two states that style it whole;
	// styled is the same text with each cell in its own style.
	var plain, styled strings.Builder
	none := lipgloss.NewStyle()
	add := func(text string, style lipgloss.Style) {
		plain.WriteString(text)
		styled.WriteString(style.Render(text))
	}
	add(gutter, none)
	gap := strings.Repeat(" ", colGap)
	for i, c := range m.columns() {
		if c.key == colMessage {
			// The message is the last column and is not padded out; an
			// empty one adds nothing, not even its gap.
			if msg := sanitizeOne(r.Message); msg != "" {
				add(gap, none)
				add(truncateRight(msg, c.width), m.theme.Muted)
			}
			continue
		}
		if i > 0 {
			add(gap, none)
		}
		switch c.key {
		case colNamespace:
			add(padRight(truncateRight(sanitizeOne(r.Ref.Namespace), c.width), c.width), none)
		case colName:
			add(padRight(truncateRight(sanitizeOne(r.Ref.Name), c.width), c.width), none)
		case colPhase:
			phase := shared.PhaseSymbol(shown) + " " + m.rowPhaseText(sanitizeOne(shown))
			add(padRight(truncateRight(phase, c.width), c.width), m.theme.PhaseStyle(shown))
		case colAge:
			add(padLeft(ageText(r, now), c.width), m.theme.Muted)
		case colDuration:
			add(padRight(durationText(r), c.width), m.theme.Muted)
		case colProgress:
			add(padRight(progressCell(r.Progress, c.width), c.width), m.theme.Muted)
		case colStarted:
			add(padRight(m.clockText(r.StartedAt), c.width), m.theme.Muted)
		case colFinished:
			add(padRight(m.clockText(r.FinishedAt), c.width), m.theme.Muted)
		case colTemplate:
			add(padRight(truncateRight(orDash(sanitizeOne(firstLabel(r, templateLabels...))), c.width), c.width), m.theme.Muted)
		case colCron:
			add(padRight(truncateRight(orDash(sanitizeOne(firstLabel(r, cronLabel))), c.width), c.width), m.theme.Muted)
		case colLabels:
			add(padRight(truncateRight(orDash(sanitizeOne(otherLabels(r))), c.width), c.width), m.theme.Muted)
		}
	}
	switch {
	case selected:
		return m.theme.SelectRow(plain.String(), m.width)
	case marked:
		return m.theme.Marked.Render(plain.String())
	}
	return styled.String()
}

// markGutter is the width of the column left of NAME that holds the mark
// glyph. It is reserved on every row, marked or not, so marking the first
// workflow does not shift every column sideways.
const markGutter = 2

// markGlyph is the one-cell mark. It is not a phase glyph, so a marked row
// never reads as a phase.
const markGlyph = "◆" // black diamond

// minUsableWidth below which the plan requires a resize notice (<60 cols).
const minUsableWidth = 60

// resizeNotice renders the <60-col notice while preserving quit/help (UI-01).
func (m *Model) resizeNotice() string {
	var b strings.Builder
	b.WriteString("argo-tui: terminal too small (" + itoa(m.width) + "x" + itoa(m.height) + ")\n")
	b.WriteString("Resize to at least 60 columns to show the workflow list.\n")
	b.WriteString("q quit  ? help  ctrl+c quit\n")
	return b.String()
}

func (m *Model) headerView() string {
	title := "argo-tui"
	if m.total > 0 || m.status != StatusLoading {
		// namespace is owned by the root; the list title carries state only.
		title = "WORKFLOWS"
	}
	return m.theme.Header.Render(title)
}

// toolbarView is the Search/Phase/Sort/state line. It must surface the
// local search scope and the incomplete count visibly (plan gate; LIST-05/09).
func (m *Model) toolbarView() string {
	parts := []string{}
	// The mark count leads the toolbar. The marks are the one state here
	// that an action will act on, and a narrow pane clips the toolbar from
	// the right.
	if cell := m.markCell(); cell != "" {
		parts = append(parts, m.theme.Marked.Render(cell))
	}

	parts = append(parts, m.searchCell())

	phaseCell := m.theme.Muted.Render("Phase:") + " " + string(m.phase)
	if n := m.suspendedCount(); n > 0 && m.phase != PhaseSuspended {
		// The count is the whole point of the marker: an operator wants to
		// know a gate is open without reading every row.
		phaseCell += "  " + m.theme.Warning.Render(itoa(n)+" awaiting resume")
	}
	parts = append(parts, phaseCell)
	parts = append(parts, m.theme.Muted.Render("Sort:")+" "+sortLabel(m.sort))

	// Long, untrusted-derived error reasons (the stale / unauthenticated /
	// forbidden message) are word-wrapped to fit the remaining width instead
	// of being clipped at the right edge. Short
	// reasons keep exactly one styled line (byte-identical to goldens);
	// only genuinely long text gets wrapped.
	// A long reason is emitted as its own block after the toolbar cells, so
	// it is joined with a newline rather than the two-space cell separator.
	reason := ""
	switch m.status {
	case StatusLoading:
		parts = append(parts, m.theme.Warning.Render("loading…"))
	case StatusStale:
		reason = m.wrapStatusReason("stale "+humanDuration(m.errAge)+" — "+m.errMsg, m.theme.Warning.Render)
	case StatusForbidden:
		reason = m.wrapStatusReason("forbidden: "+m.errMsg, m.theme.ErrorText.Render)
	case StatusUnauthenticated:
		reason = m.wrapStatusReason("unauthenticated: "+m.errMsg, m.theme.ErrorText.Render)
	case StatusIncomplete:
		parts = append(parts, m.theme.Warning.Render("INCOMPLETE (snapshot cap)"))
	}
	line := strings.Join(parts, "  ")
	if reason == "" {
		return line
	}
	if strings.HasPrefix(reason, "\n") {
		return line + reason
	}
	return line + "  " + reason
}

// wrapStatusReason word-wraps a long status reason into the remaining width
// of the toolbar line. Short reasons (which goldens pin byte-for-byte) are
// returned styled exactly as before; only text long enough to clip at the
// right edge is reflowed.
func (m *Model) wrapStatusReason(full string, style func(...string) string) string {
	const shortReasonThreshold = 60
	if m.width <= 0 || ansi.StringWidth(full) <= shortReasonThreshold {
		return style(full)
	}
	// A long reason gets its own full-width block below the toolbar cells
	// rather than the sliver left over beside them. Wrapping into a 13-cell
	// remainder turns one honest sentence into twenty rows and pushes the
	// workflow list off the pane; the reason is the thing a reader must be
	// able to read in full, so it gets the whole width.
	return "\n" + style(shared.Wrap(full, m.width))
}

// toolbarPrefixParts returns the non-status toolbar cells so wrapStatusReason
// can measure how much width remains for the (last) status reason. It mirrors
// the search/scope/phase/sort cells built in toolbarView.
func (m *Model) toolbarPrefixParts() []string {
	return []string{m.searchCell(), "Phase: " + string(m.phase), "Sort: " + sortLabel(m.sort)}
}

// searchCell is the toolbar's filter cell. While the input has focus it is
// the text being typed, followed by the parse error when there is one.
// Otherwise it is the applied filter in its parsed, canonical form, so the
// reader sees how the query was read.
//
// Scope honesty: the filter is LOCAL to the collected snapshot. The scope
// is shown whenever a query is active or the snapshot is known incomplete
// (LIST-05/09).
func (m *Model) searchCell() string {
	label := m.theme.Muted.Render("Search:")
	cell := label + " (none)  / to filter by name"
	if m.allNS {
		cell = label + " (none)  / to filter by namespace/name"
	}
	switch {
	case m.SearchOn:
		cell = label + " " + m.searchLineView()
	case m.query != "":
		cell = label + " " + sanitizeOne(m.match.String())
	}
	if m.total > m.visible || m.query != "" || m.status == StatusIncomplete {
		cell += " [within " + itoa(m.total) + " collected]"
	}
	return cell
}

// markCell is the toolbar's mark count, empty with no marks. It names the
// hidden marks separately: a bulk action reaches them too, and a reader who
// counts the diamonds on screen would otherwise come up short.
func (m *Model) markCell() string {
	n := m.MarkCount()
	if n == 0 {
		return ""
	}
	cell := markGlyph + " " + itoa(n) + " marked"
	if hidden := m.HiddenMarkCount(); hidden > 0 {
		cell += " (" + itoa(hidden) + " hidden by filter)"
	}
	return cell
}

// suspendedCount counts the workflows in the whole snapshot that hold an
// open manual gate, not only the ones the current filter shows.
func (m *Model) suspendedCount() int {
	n := 0
	for _, it := range m.items {
		if it.Suspended {
			n++
		}
	}
	return n
}

// sortLabel gives human names for the sort cycle.
func sortLabel(k SortKey) string {
	switch k {
	case SortName:
		return "name"
	case SortTime:
		return "newest"
	default:
		return "phase, newest first"
	}
}

// searchLineView renders the focused search entry: buffer with cursor and
// a usage hint (enter apply / esc cancel). Untrusted text is not involved
// (the buffer holds only typed characters) but the cursor indicator is a
// plain block so goldens stay deterministic.
func (m *Model) searchLineView() string {
	runes := []rune(m.searchBuf)
	at := m.searchCur
	if at < 0 || at > len(runes) {
		at = len(runes)
	}
	var b strings.Builder
	b.WriteString(string(runes[:at]))
	if at < len(runes) {
		b.WriteString("[")
		b.WriteString(string(runes[at]))
		b.WriteString("]")
		b.WriteString(string(runes[at+1:]))
	} else {
		b.WriteString("[_]") // cursor at end
	}
	// The error sits before the usage hint because a narrow pane clips the
	// toolbar from the right, and the error is what explains why the rows
	// stopped following the typing. The cross carries it without colour.
	if m.queryErr != "" {
		b.WriteString("  " + m.theme.ErrorText.Render("✗ "+sanitizeOne(m.queryErr)))
	}
	b.WriteString("  (enter apply, esc cancel)")
	return b.String()
}

// colKey names a table column.
type colKey int

const (
	colName colKey = iota
	colPhase
	colAge
	colDuration
	colProgress
	colStarted
	colFinished
	colTemplate
	colCron
	colLabels
	colMessage
	colNamespace
)

// title is the column head.
func (k colKey) title() string {
	return [...]string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED", "FINISHED",
		"TEMPLATE", "CRON", "LABELS", "MESSAGE", "NAMESPACE"}[k]
}

// column is one laid-out column: what it holds and how many cells it gets.
type column struct {
	key   colKey
	width int
}

// Widths of the optional columns. PROGRESS holds a count right-aligned in
// five cells ("12/40"), a space and a six-cell bar. STARTED and FINISHED
// hold "01-02 15:04". TEMPLATE and CRON fit most template names; longer
// ones are clipped with an ellipsis. LABELS is clipped; the detail view
// shows every label in full.
const (
	progressWidth = 12
	clockWidth    = 11
	originWidth   = 16
	labelsWidth   = 24
)

// wideDropOrder is the order the wide layout gives up its optional columns
// as the pane narrows, first to go first. LABELS and CRON repeat what the
// name or the template usually says already; FINISHED follows from STARTED
// and DURATION. MESSAGE outlasts them because it is the one cell that says
// why a workflow failed. PROGRESS is kept longest: it is the one live
// number the normal layout lacks below 120 columns.
var wideDropOrder = []colKey{colLabels, colCron, colFinished, colMessage, colTemplate, colStarted, colProgress}

// columns lays out the table for the current pane width.
//
// NAME, PHASE, AGE and DURATION are always shown. Each holds a value whose
// longest form is known, so widening the pane cannot make them more useful.
// MESSAGE is what is left after the others, and it is the only column that
// grows. A failure message is the one cell with no natural length, and the
// reader is usually reading the list to find out why something failed.
//
// The normal layout adds PROGRESS from 120 columns. The wide layout (w)
// offers PROGRESS, STARTED, FINISHED, TEMPLATE, CRON and LABELS as well,
// and drops them in wideDropOrder until the rest fit.
//
// A MESSAGE narrower than minMessageWidth is dropped. A message clipped to
// a few cells shows no more than the fact that a message exists, and costs
// the NAME column the width that does show something.
func (m *Model) columns() []column {
	// 28 plus the mark gutter leaves an 80-column pane a message column of
	// minMessageWidth and one cell to spare.
	name := 28
	// The PHASE cell holds "symbol space word"; the longest phase word is
	// "Succeeded" (9), so 11 is the narrowest width that never truncates a
	// known phase, and 12 leaves one cell of slack in the wide layout.
	phase, age, dur := 12, 8, 9
	switch {
	case m.width < 80:
		name, phase, age, dur = 20, 11, 6, 8
	case m.wide && m.width < 200:
		// The wide layout spends the width on metadata. 28 cells hold every
		// generated name a CronWorkflow or template gives its runs.
	case m.width >= 100:
		name = 44
	}
	// The mark gutter comes first and a gap separates each column from the
	// one before it.
	room := m.width - markGutter - (name + phase + age + dur + colGap*3)
	// The all-namespaces view leads with NAMESPACE, since two workflows in
	// different namespaces may share a name. On a pane too narrow for it
	// beside the fixed columns, NAME gives up the difference: it is the one
	// fixed column whose cells are still readable when clipped.
	var lead []column
	if ns := m.nsWidth(); ns > 0 {
		lead = []column{{colNamespace, ns}}
		room -= ns + colGap
		if m.width > 0 && room < 0 {
			cut := -room
			if name-cut < minNameWidth {
				cut = name - minNameWidth
			}
			name -= cut
			room += cut
		}
	}
	base := append(lead, column{colName, name}, column{colPhase, phase}, column{colAge, age}, column{colDuration, dur})

	var optional []column
	switch {
	case m.wide:
		optional = []column{{colProgress, progressWidth}, {colStarted, clockWidth}, {colFinished, clockWidth},
			{colTemplate, originWidth}, {colCron, originWidth}, {colLabels, labelsWidth}, {colMessage, minMessageWidth}}
	case m.width >= 120:
		optional = []column{{colProgress, progressWidth}, {colMessage, minMessageWidth}}
	case m.width >= 80:
		// Under 80 columns the fixed columns are already clipping names
		// and durations. Spending what is left on a fifth column would clip
		// them further to show a fragment of a sentence.
		optional = []column{{colMessage, minMessageWidth}}
	}
	need := func() int {
		n := 0
		for _, c := range optional {
			n += colGap + c.width
		}
		return n
	}
	for _, drop := range wideDropOrder {
		if need() <= room {
			break
		}
		for i, c := range optional {
			if c.key == drop {
				optional = append(optional[:i:i], optional[i+1:]...)
				break
			}
		}
	}
	if need() > room {
		optional = nil
	}
	// Whatever the fixed columns leave goes to the message.
	if spare := room - need(); spare > 0 {
		for i := range optional {
			if optional[i].key == colMessage {
				optional[i].width += spare
			}
		}
	}
	return append(base, optional...)
}

// columnWidth is the width of column k in the current layout, or 0 when the
// layout leaves it out.
func (m *Model) columnWidth(k colKey) int {
	for _, c := range m.columns() {
		if c.key == k {
			return c.width
		}
	}
	return 0
}

// nsWidth is the NAMESPACE column's width: zero outside the all-namespaces
// view, otherwise the longest namespace in the snapshot, bounded so a long
// namespace cannot take the NAME column's room. It is measured over the whole
// snapshot rather than the visible window, so scrolling never moves the
// columns.
func (m *Model) nsWidth() int {
	if !m.allNS {
		return 0
	}
	limit := 16
	switch {
	case m.width >= 100:
		limit = 20
	case m.width > 0 && m.width < 80:
		limit = 12
	}
	w := len("NAMESPACE")
	for _, it := range m.items {
		if n := ansi.StringWidth(sanitizeOne(it.Ref.Namespace)); n > w {
			w = n
		}
	}
	if w > limit {
		w = limit
	}
	return w
}

// minNameWidth is the narrowest NAME column the namespace column may leave.
const minNameWidth = 12

// colGap is the run of spaces between two columns.
const colGap = 2

// minMessageWidth is the narrowest message column worth rendering.
const minMessageWidth = 12

func maxString(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// emptyStateView renders the no-rows line. Distinguishable states (UI-07):
// loading ≠ empty ≠ filtered-empty ≠ error.
func (m *Model) emptyStateView() string {
	switch {
	case m.status == StatusLoading:
		return m.theme.Dim.Render("loading workflows…")
	case m.status == StatusForbidden:
		return m.theme.ErrorText.Render("no workflows visible: list forbidden (" + m.errMsg + ")")
	case m.status == StatusUnauthenticated:
		return m.theme.ErrorText.Render("no workflows visible: not authenticated (" + m.errMsg + ")")
	case m.status == StatusStale && m.total == 0:
		// A failed first collection has nothing to be stale against; the
		// failure is all there is to show.
		return m.theme.ErrorText.Render("no workflows visible: " + m.errMsg)
	case m.total > 0 && m.visible == 0:
		return m.theme.Dim.Render("no workflows match the current filter (search is local to the collected snapshot)")
	case m.allNS:
		return m.theme.Dim.Render("no workflows in any namespace this token can read")
	default:
		return m.theme.Dim.Render("no workflows in this namespace yet")
	}
}

var _ = core.Summary{}
var _ = shared.Sanitize
