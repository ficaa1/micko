package logs

import (
	"strings"

	"argo-tui/internal/ui/shared"
)

// view.go — renders the retained buffer into terminal-safe text (the only
// path any untrusted content takes before reaching the terminal).
//
// Rules (plan §8 D1; acceptance LOG-04..LOG-12):
//   - every untrusted string passes shared.Sanitize before it is placed
//     in a row (log content is sanitized at Push time; marker context at
//     PushMarker time; the header re-sanitizes defensively)
//   - markers render as full-width dash annotations with pinned wording
//   - truncated lines keep the visible markerTruncated suffix
//   - search scope is stated: "search scope: retained buffer only"
//   - the viewport never scrolls on its own while paused; following pins
//     to the tail (LOG-07/08)
//   - no ANSI of its own reaches the terminal; text accompanies every
//     state so color is never the only carrier (plan §2)

// rowKind distinguishes rendered row provenance.
type rowKind int

const (
	rowLog    rowKind = iota // retained log content
	rowMarker                // stream-context annotation
	rowEnd                   // explicit end-of-stream row
)

// row is one scrollable line: body text or an annotation.
type row struct {
	kind rowKind
	text string
}

// layout fixed chrome: header, status, [editor line], count line, footer.
const (
	chromeRows   = 3 // header + status + count
	chromeFooter = 1
	editorRows   = 1 // search or context input line when focused
)

// snapshot builds the full scrollable row list from the buffer (match
// annotations overlay onto log rows). Scroll geometry operates on this.
// The stream-open header is annotated with the number of log lines
// recorded in the whole transcript ("N line(s) recorded") — a single
// pass first counts the lines, then renders with the total.
func (b *buffer) snapshot() []row {
	entries := b.Entries()
	lineCount := 0
	for i := range entries {
		if entries[i].kind == logEntryKind {
			lineCount++
		}
	}
	rows := make([]row, 0, len(entries)+1)
	logIdx := 0
	for _, e := range entries {
		if e.isMarker() {
			text := markerText(e.kind, e.ctxPod, e.ctxCont)
			if e.kind == markOpen {
				text = markerLineWithCount(e.ctxPod, e.ctxCont, lineCount)
			}
			rows = append(rows, row{kind: rowMarker, text: text})
			continue
		}
		rows = append(rows, row{kind: rowLog, text: e.line.Content})
		logIdx++
	}
	return rows
}

// overlayMatches prefixes hit rows with a visible "[match N]" annotation
// so the search scope is auditable (LOG-09). hitLogIdx maps scrollable-row
// position → log-line index; hits are indices into the retained log lines.
func overlayMatches(rows []row, hits []int, hitLogIdx []int) {
	if len(hits) == 0 {
		return
	}
	pos := make(map[int]int, len(hitLogIdx)) // log-line index → row index
	for r, l := range hitLogIdx {
		pos[l] = r
	}
	for n, h := range hits {
		r, ok := pos[h]
		if !ok || r < 0 || r >= len(rows) {
			continue
		}
		rows[r].text = "[match " + itoaView(n+1) + "] " + rows[r].text
	}
}

// viewRows is the scrollable window height (fixed chrome + footer + an
// editor line when one is focused).
func (m *Model) viewRows() int {
	h := m.height - chromeRows - chromeFooter
	if m.searchOn || m.contextOn {
		h -= editorRows
	}
	if h < 1 {
		return 1
	}
	return h
}

// maxBottom is the last valid bottom row index for the current snapshot.
func (m *Model) maxBottom() int {
	if n := len(m.rows); n > 0 {
		return n - 1
	}
	return 0
}

// bottomAt resolves the current bottom edge to an absolute row index
// (bottom < 0 = pinned to tail).
func (m *Model) bottomAt() int {
	if m.bottom < 0 {
		return m.maxBottom()
	}
	return m.clampBottomRaw(m.bottom)
}

// clampBottomRaw clamps an absolute bottom index into [0, maxBottom].
func (m *Model) clampBottomRaw(b int) int {
	if b < 0 {
		return 0
	}
	if max := m.maxBottom(); b > max {
		return max
	}
	return b
}

// clampBottom resolves bottom+delta to an absolute clamped index.
func (m *Model) clampBottom(base, delta int) int {
	return m.clampBottomRaw(base + delta)
}

// ensureSnapshot rebuilds the scrollable rows when the buffer changed.
// While paused, the stored absolute bottom edge is honored exactly: the
// same row that was visible stays visible when new lines arrive (plan
// gate: no autoscroll jump while paused). While following, the tail is
// re-pinned every render.
//
// The rebuild must happen BEFORE any scroll decision that resolves
// bottom<0 ("pinned to tail"): key handling reads absolute positions from
// the current snapshot. Update therefore refreshes the snapshot ahead of
// key dispatch whenever the buffer changed.
func (m *Model) ensureSnapshot() {
	if m.rowsFor == len(m.buf.buf) && m.rows != nil {
		return
	}
	m.rows = m.buf.snapshot()
	hitLogIdx := make([]int, 0, len(m.rows))
	logIdx := 0
	for _, row := range m.rows {
		if row.kind == rowLog {
			hitLogIdx = append(hitLogIdx, logIdx)
			logIdx++
		} else {
			hitLogIdx = append(hitLogIdx, -1)
		}
	}
	overlayMatches(m.rows, m.hits, hitLogIdx)
	m.rowsFor = len(m.buf.buf)
}

// window returns the rows visible at the current scroll position.
func (m *Model) window() []row {
	m.ensureSnapshot()
	h := m.viewRows()
	if len(m.rows) == 0 {
		return nil
	}
	bottom := m.bottomAt()
	top := bottom - h + 1
	if top < 0 {
		top = 0
	}
	if bottom >= len(m.rows) {
		bottom = len(m.rows) - 1
	}
	return m.rows[top : bottom+1]
}

// View renders the log pane (header, status, editors, count, window,
// footer). The terminal receives only sanitized text from here.
func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(m.statusView())
	b.WriteString("\n")
	if m.searchOn {
		b.WriteString("search: " + m.searchBuf + "_  (enter apply, esc cancel)")
		b.WriteString("\n")
	} else if m.contextOn {
		b.WriteString("pod: [" + m.podBuf + "]  container: [" + m.containerBuf + "]  (enter apply, esc cancel)")
		if m.contextErr != "" {
			b.WriteString("  — " + m.contextErr)
		}
		b.WriteString("\n")
	}
	b.WriteString(m.countView())
	b.WriteString("\n")
	for _, r := range m.window() {
		switch r.kind {
		case rowMarker:
			b.WriteString(r.text)
		case rowEnd:
			b.WriteString(r.text)
		default:
			b.WriteString(r.text)
		}
		b.WriteString("\n")
	}
	b.WriteString(m.footerView())
	return b.String()
}

// headerView is the pane title: sanitized workflow name (SEC-02).
func (m *Model) headerView() string {
	return "logs: " + shared.Sanitize(m.ref.Name)
}

// statusView carries scope + lifecycle. States are distinguishable by text
// (UI-07); errors surface the honest cause (LOG-12).
func (m *Model) statusView() string {
	scope := "workflow-wide"
	if m.podName != "" {
		scope = "pod: " + shared.Sanitize(m.podName)
	}
	s := "Scope: " + scope + " · container: " + shared.Sanitize(m.container) +
		" · [" + m.phase.String() + "]"
	if m.phase == PhaseError && m.errText != "" {
		s += " — " + shared.Sanitize(m.errText)
	}
	return s
}

// countView reports retention counts (bounded, visible) and, when a
// search is committed, its honest scope (LOG-09: retained buffer only).
// Lines counts log lines only (markers are UI authoring, not data).
func (m *Model) countView() string {
	_, _, _, evicted, _ := m.buf.Counts()
	lines := len(m.buf.Lines())
	s := "retained: " + itoaView(lines) + "/" + itoaView(MaxLines) +
		" lines (" + itoa64(evicted) + " evicted)"
	if m.search.term != "" {
		s += " · search: " + itoaView(len(m.hits)) + " match(es) for " +
			"\"" + m.search.term + "\"" +
			" · search scope: retained buffer only (" + itoaView(m.searchScopeN) + " lines searched)"
	}
	return s
}

// footerView carries the key hints (q/n/p/? are root-owned).
func (m *Model) footerView() string {
	return "space pause  f follow  / search  c container  pgup/pgdn scroll"
}
