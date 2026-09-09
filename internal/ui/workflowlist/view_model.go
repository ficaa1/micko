package workflowlist

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/shared"
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
	lines := m.frameLines(now)
	if m.height > 0 {
		lines = shared.ClampLines(lines, m.height)
	}
	return strings.Join(lines, "\n")
}

// frameLines assembles the pane as discrete terminal lines so the height
// budget can be enforced on whole lines. The line sequence is identical to
// the pre-viewport concatenation, so unsized renders stay byte-stable.
func (m *Model) frameLines(now time.Time) []string {
	lines := []string{m.headerView()}
	lines = append(lines, strings.Split(m.toolbarView(), "\n")...)

	wName, wPhase, wAge, wDur := m.colWidths()
	head := padRight("NAME", wName) + "  " +
		padRight("PHASE", wPhase) + "  " +
		padLeft("AGE", wAge) + "  " +
		padRight("DURATION", wDur)
	lines = append(lines, m.theme.Header.Render(head))

	if len(m.rows) == 0 {
		return append(lines, m.emptyStateView(), m.footerView(0, 0))
	}

	start, end := m.window(len(lines))
	for i := start; i < end; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.selIndex(), now))
	}
	// The blank separator above the footer is part of the original layout.
	return append(lines, "", m.footerView(start, end))
}

// window returns the half-open row range to render. chromeLines is the number
// of lines already emitted above the rows; the footer, the blank separator
// above it, and that chrome are all reserved before rows get their budget.
//
// The window is anchored on scrollTop and only moves far enough to keep the
// selected row inside it, so paging feels stable rather than jumping.
func (m *Model) window(chromeLines int) (start, end int) {
	if m.height <= 0 {
		m.scrollTop = 0
		return 0, len(m.rows)
	}
	budget := m.height - chromeLines - 2 // blank separator + footer
	if budget >= len(m.rows) {
		m.scrollTop = 0
		return 0, len(m.rows)
	}
	if budget < 1 {
		budget = 1 // clampLines trims the rest; never render zero rows
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

// rowLine renders one table row with its phase styling (or the selection
// highlight, which must win so the selected row is never ambiguous).
func (m *Model) rowLine(r core.Summary, selected bool, now time.Time) string {
	wName, wPhase, wAge, wDur := m.colWidths()
	name := sanitizeOne(r.Ref.Name)
	phase := m.rowPhaseText(sanitizeOne(r.Phase))
	msg := sanitizeOne(r.Message)
	line := padRight(truncateRight(name, wName), wName) + "  " +
		padRight(truncateRight(phase, wPhase), wPhase) + "  " +
		padLeft(ageText(r, now), wAge) + "  " +
		padRight(durationText(r), wDur)
	if m.width >= 100 && msg != "" {
		line += "  " + truncateRight(msg, maxString(10, m.width-wName-wPhase-wAge-wDur-14))
	}
	if selected {
		return m.theme.Selected.Render(line)
	}
	return m.theme.PhaseStyle(r.Phase).Render(line)
}

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

	q := m.query
	if m.SearchOn {
		q = m.searchLineView()
	}
	searchCell := "Search: " + q
	if q == "" && !m.SearchOn {
		searchCell = "Search: (none)  / to filter by name"
	}
	// Scope honesty: the search/filter is LOCAL to the collected snapshot.
	// Show the scope whenever a query is active or the snapshot is known
	// incomplete (plan gate; LIST-05/09).
	scope := ""
	if m.total > m.visible {
		scope = " [within " + itoa(m.total) + " collected]"
	} else if m.query != "" || m.status == StatusIncomplete {
		scope = " [within " + itoa(m.total) + " collected]"
	}
	parts = append(parts, searchCell+scope)

	parts = append(parts, "Phase: "+string(m.phase))
	parts = append(parts, "Sort: "+sortLabel(m.sort))

	// Long, untrusted-derived error reasons (the stale / unauthenticated /
	// forbidden message) are word-wrapped to fit the remaining width instead
	// of being clipped at the right edge (live-smoke defect 5). Short
	// reasons keep exactly one styled line (byte-identical to goldens);
	// only genuinely long text gets wrapped.
	switch m.status {
	case StatusLoading:
		parts = append(parts, m.theme.Warning.Render("loading…"))
	case StatusStale:
		parts = append(parts, m.wrapStatusReason("stale "+humanDuration(m.errAge)+" — "+m.errMsg, m.theme.Warning.Render))
	case StatusForbidden:
		parts = append(parts, m.wrapStatusReason("forbidden: "+m.errMsg, m.theme.ErrorText.Render))
	case StatusUnauthenticated:
		parts = append(parts, m.wrapStatusReason("unauthenticated: "+m.errMsg, m.theme.ErrorText.Render))
	case StatusIncomplete:
		parts = append(parts, m.theme.Warning.Render("INCOMPLETE (snapshot cap)"))
	}
	return strings.Join(parts, "  ")
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
	// Remaining width after the non-status parts already joined (the status
	// part is appended last). Leave room for the "  " separator.
	prefixCells := 0
	prefixCells = ansi.StringWidth(strings.Join(m.toolbarPrefixParts(), "  "))
	budget := m.width - prefixCells - 3
	if budget < 8 {
		// Almost no room left after the toolbar prefix; wrapping would
		// produce unusable slivers. Keep the single line (better to clip a
		// tiny residual than to garble the layout).
		return style(full)
	}
	return style(shared.Wrap(full, budget))
}

// toolbarPrefixParts returns the non-status toolbar cells so wrapStatusReason
// can measure how much width remains for the (last) status reason. It mirrors
// the search/scope/phase/sort cells built in toolbarView.
func (m *Model) toolbarPrefixParts() []string {
	q := m.query
	if m.SearchOn {
		q = m.searchLineView()
	}
	searchCell := "Search: " + q
	if q == "" && !m.SearchOn {
		searchCell = "Search: (none)  / to filter by name"
	}
	scope := ""
	if m.total > m.visible {
		scope = " [within " + itoa(m.total) + " collected]"
	} else if m.query != "" || m.status == StatusIncomplete {
		scope = " [within " + itoa(m.total) + " collected]"
	}
	return []string{searchCell + scope, "Phase: " + string(m.phase), "Sort: " + sortLabel(m.sort)}
}

// sortLabel gives human names for the sort cycle.
func sortLabel(k SortKey) string {
	switch k {
	case SortName:
		return "name"
	case SortTime:
		return "newest"
	default:
		return "phase"
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
	b.WriteString("  (enter apply, esc cancel)")
	return b.String()
}

// colWidths computes column widths from the terminal size.
func (m *Model) colWidths() (name, phase, age, dur int) {
	name = 30
	phase = 10
	age = 8
	dur = 9
	if m.width >= 100 {
		name = 44
	}
	if m.width < 80 {
		name = 20
		phase = 9
		age = 6
		dur = 8
	}
	return
}

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
	case m.total > 0 && m.visible == 0:
		return m.theme.Dim.Render("no workflows match the current filter (search is local to the collected snapshot)")
	default:
		return m.theme.Dim.Render("no workflows in this namespace yet")
	}
}

// footerView carries key hints; q is root-owned (outside text entry). When
// the viewport hides rows it also carries the window position, so a windowed
// list is distinguishable from a list that simply has few workflows.
func (m *Model) footerView(start, end int) string {
	hints := "j/k move  enter open  l logs  / search  s sort  r refresh  ? help"
	if end-start > 0 && end-start < len(m.rows) {
		hints += "  " + itoa(start+1) + "-" + itoa(end) + "/" + itoa(len(m.rows))
	}
	return hints
}

var _ = core.Summary{}
var _ = shared.Sanitize
