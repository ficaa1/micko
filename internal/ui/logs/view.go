package logs

import (
	"strings"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
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
	// paneChromeSaved is the header and footer the shell draws instead, and
	// which the pane therefore gets back as content rows.
	paneChromeSaved = 2
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

// hitRowIndex maps each hit, in order, to the scrollable row that carries it.
// hitLogIdx maps scrollable-row position → log-line index; hits are indices
// into the retained log lines.
//
// This replaces the old "[match N]" text prefix. That prefix edited the line
// itself, which pushed the content right, broke a copy of the buffer and made
// the reader count captions instead of seeing the word they searched for.
func hitRowIndex(hits []int, hitLogIdx []int) []int {
	if len(hits) == 0 {
		return nil
	}
	pos := make(map[int]int, len(hitLogIdx)) // log-line index → row index
	for r, l := range hitLogIdx {
		if l >= 0 {
			pos[l] = r
		}
	}
	out := make([]int, 0, len(hits))
	for _, h := range hits {
		r, ok := pos[h]
		if !ok {
			r = -1
		}
		out = append(out, r)
	}
	return out
}

// highlight marks every occurrence of the search term inside one line.
//
// The current hit is reversed and the others are coloured, so the eye lands on
// the line n or N just moved to without losing the others. Nothing is added to
// the text: the styling is an overlay, and RawLines and the clipboard keep the
// line exactly as the server sent it.
func (m *Model) highlight(text string, current bool) string {
	term := m.search.term
	if term == "" || text == "" {
		return text
	}
	style := m.theme.Warning
	if current {
		style = m.theme.Selected
	}
	hay, needle := text, term
	if !m.search.caseSensitive {
		hay, needle = strings.ToLower(text), strings.ToLower(term)
	}
	var b strings.Builder
	for i := 0; ; {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			b.WriteString(text[i:])
			break
		}
		b.WriteString(text[i : i+j])
		b.WriteString(style.Render(text[i+j : i+j+len(needle)]))
		i += j + len(needle)
	}
	return b.String()
}

// viewRows is the scrollable window height (fixed chrome + footer + an
// editor line when one is focused).
//
// In pane mode the shell draws the title and the footer as its own bands, so
// the pane gets those two lines back for content. Scroll paging reads this
// number, so it must agree with what View/BodyLines actually emits.
func (m *Model) viewRows() int {
	h := m.height - chromeRows - chromeFooter
	if m.paneMode {
		h += paneChromeSaved
	}
	if m.searchOn || m.contextOn || m.pipeOn {
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

// minBottom is the lowest bottom index that still fills the pane. Scrolling
// cannot go below it: rows[0] is already the first line on screen.
func (m *Model) minBottom() int {
	min := m.viewRows() - 1
	if max := m.maxBottom(); min > max {
		return max
	}
	if min < 0 {
		return 0
	}
	return min
}

// clampBottomRaw clamps an absolute bottom index into [minBottom, maxBottom].
// The floor matters: without it pgup drove bottom to 0 while the renderer
// still drew a full pane, so the next arrow-down moved the state but not the
// screen, and the pane looked frozen for a whole page of presses.
func (m *Model) clampBottomRaw(b int) int {
	if min := m.minBottom(); b < min {
		return min
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
	m.hitRows = hitRowIndex(m.hits, hitLogIdx)
	m.rowsFor = len(m.buf.buf)
}

// window returns the rows visible at the current scroll position.
func (m *Model) window() []row {
	rows, _ := m.windowAt()
	return rows
}

// windowAt returns the visible rows and the absolute index of the first one,
// so the renderer can tell which row carries the current search hit.
func (m *Model) windowAt() ([]row, int) {
	m.ensureSnapshot()
	h := m.viewRows()
	if len(m.rows) == 0 {
		return nil, 0
	}
	bottom := m.bottomAt()
	// Scrolling up past the first line must not shrink the pane. Without
	// this floor the window becomes rows[0:bottom+1], so every further press
	// removed one more line from the BOTTOM of the screen while the top
	// stayed put — text appeared to be eaten from below.
	if min := h - 1; bottom < min {
		bottom = min
	}
	if bottom >= len(m.rows) {
		bottom = len(m.rows) - 1
	}
	top := bottom - h + 1
	if top < 0 {
		top = 0
	}
	return m.rows[top : bottom+1], top
}

// View renders the log pane (header, status, editors, count, window,
// footer). The terminal receives only sanitized text from here.
func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(strings.Join(m.bodyLines(), "\n"))
	b.WriteString("\n")
	b.WriteString(m.footerView())
	return b.String()
}

// SetPaneMode tells the pane that a shell draws its title and footer. It is
// opt-in so any caller that renders logs standalone keeps a complete pane.
func (m *Model) SetPaneMode(v bool) { m.paneMode = v }

// PaneTitle is the shell border title: the sanitized workflow name (SEC-02).
func (m *Model) PaneTitle() string { return "Logs " + shared.Sanitize(m.ref.Name) }

// Hints is the log key contract, mirrored by the `?` overlay.
func (m *Model) Hints() string { return m.footerView() }

// PaneStatus is the right-aligned footer cell: the lifecycle state, which a
// reader must be able to find in one fixed place.
func (m *Model) PaneStatus() string {
	s := m.phase.String()
	if m.paused {
		return s + " · paused"
	}
	if m.follow {
		return s + " · follow"
	}
	return s
}

// BodyLines renders the pane content for the shell: no title, no footer.
func (m *Model) BodyLines() []string { return m.bodyLines() }

// RawLines is every retained line with no window: the borderless full-screen
// view pages over it, and the copy key yanks it.
func (m *Model) RawLines() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureSnapshot()
	out := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r.text)
	}
	return out
}

// bodyLines is the shared middle of the pane (status, editors, counts and
// the scroll window) that both compositions place between their own chrome.
func (m *Model) bodyLines() []string {
	var b strings.Builder
	b.WriteString(m.statusView())
	b.WriteString("\n")
	if m.searchOn {
		b.WriteString("search: " + m.searchBuf + "_  (enter apply, esc cancel)")
		b.WriteString("\n")
	} else if m.pipeOn {
		b.WriteString("pipe to: " + m.pipeBuf + "_  (enter runs it with the retained lines on stdin, esc cancels)")
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
	rows, top := m.windowAt()
	cur := m.currentHitRow()
	for i, r := range rows {
		text := r.text
		if r.kind == rowLog {
			text = m.highlight(text, top+i == cur)
		}
		b.WriteString(text)
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	// An empty buffer leaves the count line as the last written line, whose
	// trailing newline would otherwise become a phantom blank row.
	return strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
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
		s += " · search \"" + m.search.term + "\": "
		if len(m.hits) == 0 {
			s += "no match"
		} else {
			s += itoaView(m.curHit+1) + "/" + itoaView(len(m.hits)) + " (n next, N previous)"
		}
		s += " · search scope: retained buffer only (" + itoaView(m.searchScopeN) + " lines searched)"
	}
	return s
}

// footerView carries the key hints (q/n/p/? are root-owned).
func (m *Model) footerView() string {
	return "t follow  space pause  / search  n next  c container  | pipe  f raw  y copy  esc back"
}
