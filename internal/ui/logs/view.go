package logs

import (
	"strconv"
	"strings"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// Rendering is the only path untrusted content takes to the terminal. Every
// string passes shared.Sanitize, and every state has text, so colour never
// carries meaning alone.

// rowKind distinguishes rendered row provenance.
type rowKind int

const (
	rowLog    rowKind = iota // retained log content
	rowMarker                // stream-context annotation
	rowEnd                   // explicit end-of-stream row
)

// row is one scrollable line: body text or an annotation. With wrapping on
// a row can take several screen lines; it is still one row, so scrolling,
// pausing and search jumps all anchor on the logical line.
type row struct {
	kind rowKind
	text string
	// logIdx is the row's index among the retained log lines, -1 for an
	// annotation. Search hits are indices of the same kind.
	logIdx int
	// pod is the pod the line came from, for the source label.
	pod string
}

// The body's fixed chrome is the status and count lines, plus an editor
// line while one is focused. The shell draws the title and footer bands.
const (
	chromeRows = 2 // status + count
	editorRows = 1 // search or context input line when focused
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
			rows = append(rows, row{kind: rowMarker, text: text, logIdx: -1})
			continue
		}
		rows = append(rows, row{kind: rowLog, text: e.line.Content, logIdx: logIdx, pod: e.line.PodName})
		logIdx++
	}
	return rows
}

// filterRows keeps the log rows that contain term, with the search's own
// case rule, and drops the annotations: the filter shows matching lines
// and nothing else, the way less's & does.
func filterRows(rows []row, term string, caseSensitive bool) []row {
	out := make([]row, 0, len(rows))
	needle := term
	if !caseSensitive {
		needle = strings.ToLower(term)
	}
	for _, r := range rows {
		if r.kind != rowLog {
			continue
		}
		hay := r.text
		if !caseSensitive {
			hay = strings.ToLower(hay)
		}
		if strings.Contains(hay, needle) {
			out = append(out, r)
		}
	}
	return out
}

// hitRowIndex maps each hit, in order, to the scrollable row that carries it.
// hitLogIdx maps scrollable-row position → log-line index; hits are indices
// into the retained log lines.
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

// viewRows is the scrollable window height: the body height less its fixed
// chrome and a focused editor line. Scroll paging reads this number, so it
// must agree with what BodyLines actually emits.
func (m *Model) viewRows() int {
	h := m.height - chromeRows
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
// cannot go below it: rows[0] is already the first line on screen. With
// wrapping on, a row can take several screen lines, so the floor is the
// first row at which the rows up to it fill the pane.
func (m *Model) minBottom() int {
	h := m.viewRows()
	if m.wrap {
		used := 0
		for i, r := range m.rows {
			used += m.rowHeight(r)
			if used >= h {
				return i
			}
		}
		return m.maxBottom()
	}
	min := h - 1
	if max := m.maxBottom(); min > max {
		return max
	}
	if min < 0 {
		return 0
	}
	return min
}

// textWidth is the number of cells a row's text gets on one screen line:
// the pane width less the label column when labels are shown.
func (m *Model) textWidth(r row) int {
	w := m.width
	if r.kind == rowLog && m.labelsShown() {
		w -= labelWidth + labelGap
	}
	if w < 1 {
		return 1
	}
	return w
}

// rowHeight is the number of screen lines a row takes: one, or with
// wrapping on, as many as its text needs at the pane width. Annotations are
// short and never wrap. An unknown width (0) wraps nothing.
func (m *Model) rowHeight(r row) int {
	if !m.wrap || r.kind != rowLog || m.width <= 0 {
		return 1
	}
	return len(wrapBreaks(r.text, m.textWidth(r)))
}

// clampBottomRaw clamps an absolute bottom index into [minBottom, maxBottom].
// The floor keeps scroll state in step with the screen: below it the pane is
// already at the top, and a key would move the state but not the view.
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
// same row that was visible stays visible when new lines arrive (no
// autoscroll jump while paused). While following, the tail is
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
	m.retainedLogs = 0
	for _, r := range m.rows {
		if r.kind == rowLog {
			m.retainedLogs++
		}
	}
	if m.filterOn && m.search.term != "" {
		m.rows = filterRows(m.rows, m.search.term, m.search.caseSensitive)
	}
	hitLogIdx := make([]int, 0, len(m.rows))
	for _, r := range m.rows {
		hitLogIdx = append(hitLogIdx, r.logIdx)
	}
	m.hitRows = hitRowIndex(m.hits, hitLogIdx)
	m.rowsFor = len(m.buf.buf)
}

// window returns the rows visible at the current scroll position, the top
// one included when wrapping shows only its tail.
func (m *Model) window() []row {
	rows, _, _ := m.windowAt()
	return rows
}

// windowAt returns the visible rows, the absolute index of the first one,
// so the renderer can tell which row carries the current search hit, and
// how many of the first row's screen lines are scrolled off the top.
//
// The window is anchored at its bottom row, a logical line. With wrapping
// on, rows are added above it until the pane is full, and the top one may
// show only its last screen lines. Turning wrapping on or off therefore
// keeps the same line at the bottom of the pane.
func (m *Model) windowAt() ([]row, int, int) {
	m.ensureSnapshot()
	h := m.viewRows()
	if len(m.rows) == 0 {
		return nil, 0, 0
	}
	if m.wrap {
		bottom := m.bottomAt()
		if min := m.minBottom(); bottom <= min {
			// At the top of the log the first row starts the pane. The
			// floor row may not fit whole below it; windowLines cuts its
			// tail at the pane's last line.
			return m.rows[:min+1], 0, 0
		}
		top, used := bottom, m.rowHeight(m.rows[bottom])
		for top > 0 && used < h {
			top--
			used += m.rowHeight(m.rows[top])
		}
		skip := 0
		if used > h && top < bottom {
			skip = used - h
		}
		return m.rows[top : bottom+1], top, skip
	}
	bottom := m.bottomAt()
	// Scrolling up past the first line must not shrink the pane.
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
	return m.rows[top : bottom+1], top, 0
}

// PaneTitle is the shell border title: the sanitized workflow name.
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
// The terminal receives only sanitized text from here.
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

// bodyLines is the pane body: status, editors, counts and the scroll window.
func (m *Model) bodyLines() []string {
	var b strings.Builder
	b.WriteString(m.statusView())
	b.WriteString("\n")
	// A notice explains an empty stream, so it is shown only while nothing
	// was retained. It gets lines of its own, wrapped, because it is the one
	// thing on an empty pane worth reading in full.
	if m.notice != "" && m.phase != PhaseError && len(m.buf.Lines()) == 0 {
		b.WriteString(shared.Wrap(shared.Sanitize(m.notice), m.width))
		b.WriteString("\n")
	}
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
	b.WriteString(m.theme.Muted.Render(m.countView()))
	b.WriteString("\n")
	b.WriteString(strings.Join(m.windowLines(), "\n"))
	// An empty buffer leaves the count line as the last written line, whose
	// trailing newline would otherwise become a phantom blank row.
	return strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
}

// windowLines renders the visible window as screen lines: each row with its
// source label and its styled spans, cut into screen lines when wrapping is
// on. A wrapped row's continuation lines are indented past the label
// column, so the labels stay a clean column.
func (m *Model) windowLines() []string {
	rows, top, skip := m.windowAt()
	cur := m.currentHitRow()
	h := m.viewRows()
	out := make([]string, 0, h)
	for i, r := range rows {
		if r.kind != rowLog {
			// A marker is the pane's own annotation, not server output.
			out = append(out, m.theme.Muted.Render(r.text))
			continue
		}
		prefix, indent := "", ""
		if m.labelsShown() {
			prefix = m.labelCell(r.pod)
			indent = strings.Repeat(" ", labelWidth+labelGap)
		}
		spans := m.lineSpans(r.text, top+i == cur)
		if !m.wrap || m.width <= 0 {
			out = append(out, prefix+renderRange(r.text, cellOffset(r.text, m.left), len(r.text), spans))
			continue
		}
		breaks := wrapBreaks(r.text, m.textWidth(r))
		for j, from := range breaks {
			if i == 0 && j < skip {
				continue
			}
			to := len(r.text)
			if j+1 < len(breaks) {
				to = breaks[j+1]
			}
			lead := indent
			if j == 0 {
				lead = prefix
			}
			out = append(out, lead+renderRange(r.text, from, to, spans))
		}
	}
	// A bottom row taller than the pane shows its first screen lines: the
	// anchor is the line's start, which is where it has to be read from.
	if len(out) > h && m.wrap {
		out = out[:h]
	}
	return out
}

// statusView carries scope + lifecycle. States are distinguishable by text;
// errors surface the honest cause.
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
	// The filter hides lines, so the count of what it hides sits on the
	// line that is always shown, beside the stream state.
	if m.filterOn && m.search.term != "" {
		m.ensureSnapshot()
		s += " · & \"" + m.search.term + "\": showing " + strconv.Itoa(len(m.rows)) + " of " + strconv.Itoa(m.retainedLogs)
	}
	if m.note != "" {
		s += " · " + m.note
	}
	return s
}

// countView reports retention counts (bounded, visible) and, when a
// search is committed, its honest scope (retained buffer only).
// Lines counts log lines only (markers are UI authoring, not data).
func (m *Model) countView() string {
	_, _, _, evicted, _ := m.buf.Counts()
	lines := len(m.buf.Lines())
	s := "retained: " + strconv.Itoa(lines) + "/" + strconv.Itoa(MaxLines) +
		" lines (" + strconv.FormatInt(evicted, 10) + " evicted)"
	if m.search.term != "" {
		s += " · search \"" + m.search.term + "\": "
		if len(m.hits) == 0 {
			s += "no match"
		} else {
			s += strconv.Itoa(m.curHit+1) + "/" + strconv.Itoa(len(m.hits)) + " (n next, N previous)"
		}
		s += " · search scope: retained buffer only (" + strconv.Itoa(m.searchScopeN) + " lines searched)"
	}
	return s
}

// footerView carries the key hints (q/n/p/? are root-owned). They are in
// the order a reader reaches for them, because a narrow pane clips the
// footer from the right; `?` lists the rest.
func (m *Model) footerView() string {
	return "t follow  space pause  / search  n next  & only matches  w wrap  L labels  c container  | pipe  esc back"
}
