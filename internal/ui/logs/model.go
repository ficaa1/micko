// Package logs is the log viewer component: a bounded, keyboard-driven
// log surface over injected core values.
//
// The model never starts goroutines or touches the transport; the root
// delivers records through ApplyRecords. Retention is bounded by MaxLines and
// MaxBytes, and oversize lines are truncated or dropped with a visible
// marker. Pausing stops autoscroll only. Every untrusted string is sanitized
// before render.
package logs

import (
	"regexp"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Phase is the stream lifecycle surfaced by the status row (distinguishable
// states).
type Phase int

const (
	// PhaseStreaming: a stream is open or being opened. A pane starts here.
	PhaseStreaming Phase = iota
	// PhasePaused: collection continues upstream, viewport is frozen.
	PhasePaused
	// PhaseEnded: clean end-of-stream.
	PhaseEnded
	// PhaseError: stream failed; error text is shown (honest state).
	PhaseError
	// PhaseCanceled: the user navigated away or canceled the stream.
	PhaseCanceled
)

// String renders the phase as the status badge (text accompanies any
// styling — color is never the only carrier).
func (p Phase) String() string {
	switch p {
	case PhasePaused:
		return "PAUSED"
	case PhaseEnded:
		return "ENDED"
	case PhaseError:
		return "ERROR"
	case PhaseCanceled:
		return "CANCELED"
	default:
		return "FOLLOWING"
	}
}

// Model is the log viewer child model. The zero value is not usable; use
// NewModel.
type Model struct {
	ref       core.Ref // the workflow whose logs are shown
	podName   string   // pod scope ("" = workflow-wide/all pods)
	container string   // selected container (default "main", editable)

	buf *buffer // the retained bounded buffer

	follow  bool  // autoscroll while true
	paused  bool  // manual pause (Space); resume with f
	bottom  int   // scroll position (row offset from top); <0 means "pinned to tail"
	rows    []row // last snapshot (scroll geometry operates on it)
	rowsFor int   // number of buffer entries rows was built from (-1 = stale)

	phase   Phase  // stream lifecycle
	errText string // sanitized error/unavailable explanation
	// notice explains a stream that ended cleanly with nothing to show when
	// there is a known reason, so an empty pane never reads as a quiet pod.
	notice string

	search       searchState // committed search
	searchBuf    string      // keystroke buffer while focused
	searchOn     bool        // search input has focus (isolates keys)
	hits         []int       // match indices over retained log lines
	hitRows      []int       // hit → scrollable row index (-1 when off-buffer)
	curHit       int         // the hit n/N last moved to
	searchScopeN int         // lines the search ran over (scope honesty)

	pipeOn       bool   // pipe-command editor has focus
	pipeBuf      string // editor keystroke buffer (the command)
	pipeCmd      string // the command the editor prefills with
	contextOn    bool   // pod/container editor has focus
	podBuf       string // editor keystroke buffer (pod)
	containerBuf string // editor keystroke buffer (container)
	contextErr   string // editor validation message (blank container refused)

	width, height int // last known terminal size (SetSize)
	// theme styles the search highlight and mutes the pane's own
	// annotations. Log lines are never coloured as a whole, so the highlight
	// is the one channel that says "this is the word you searched for"
	// without editing the line.
	theme shared.Theme

	headerDone bool // stream-open marker emitted for this context

	// gPending is the armed half of vim's gg (see the list pane).
	gPending bool

	// wrap cuts long lines into screen lines instead of letting the pane
	// clip them (w). The scroll position stays on logical lines either way.
	wrap bool
	// left is how many cells of each log line are scrolled off the left
	// edge while wrapping is off.
	left int
	// labels puts each line's source label in front of it (L). It starts on
	// for workflow-wide logs, which mix pods, and off for one pod's log,
	// where every line would carry the same label.
	labels bool
	// sources maps a pod name to the display name of the node that ran it,
	// for the labels. A pod missing from it is labelled by its own name.
	sources map[string]string
	// filterOn shows only the lines matching the committed search (&).
	// It hides lines from the view only: the buffer, its counts and its
	// retention are the same with it on or off.
	filterOn bool
	// retainedLogs is the number of retained log lines in the last
	// snapshot, before the filter: the "of" in "showing 12 of 840".
	retainedLogs int
	// timestamps asks the server to stamp each line (ctrl+t). Changing it
	// reopens the stream, so the root reads it when it builds the request.
	timestamps bool
	// searchRe is the committed search term compiled for highlighting, and
	// searchReFor the term it was compiled from.
	searchRe    *regexp.Regexp
	searchReFor string
	// note is a one-key hint on the status line, such as why & did
	// nothing. The next key clears it.
	note string

	// mu guards Apply* and Clear for callers outside the update loop.
	mu sync.Mutex
}

// NewModel builds a log viewer for ref with explicit context. podName ""
// means workflow-wide logs (rendered "(all pods)"). container "" is
// normalized to the visible, editable default "main" — never silently
// guessed.
func NewModel(ref core.Ref, podName, container string) *Model {
	if container == "" {
		container = "main"
	}
	return &Model{
		ref:       ref,
		podName:   podName,
		container: container,
		theme:     shared.NewTheme(false),
		buf:       NewBuffer(MaxLines, MaxBytes),
		follow:    true,
		bottom:    -1, // pinned to tail
		rowsFor:   -1,
		phase:     PhaseStreaming,
		labels:    podName == "",
		// The stream's first line is annotated with its context header
		// (pod:container) so provenance is visible in the transcript.
	}
}

// ensureContextHeaderLocked emits the stream-open marker for the current
// context once. ApplyRecords calls it before the first records land, so no
// stream shows records without saying which pod and container they came
// from. The caller holds m.mu.
func (m *Model) ensureContextHeaderLocked() {
	if m.headerDone {
		return
	}
	m.headerDone = true
	m.buf.PushMarker(markOpen, m.podName, m.container)
	m.rowsFor = -1
}

// SetTheme injects the style set used for the highlight and annotations.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// Ref returns the workflow the viewer is attached to.
func (m *Model) Ref() core.Ref { return m.ref }

// Context returns the current pod/container scope. podName "" means
// workflow-wide.
func (m *Model) Context() (podName, container string) {
	return m.podName, m.container
}

// EscapeConsumed reports whether Esc currently belongs to a text editor.
func (m *Model) EscapeConsumed() bool { return m.searchOn || m.contextOn || m.pipeOn }

// EscapeClears reports whether Esc clears something in the pane rather
// than leaving it: the & filter. It is not text entry, so q and ? keep
// their global meaning; only Esc stops meaning "back".
func (m *Model) EscapeClears() bool { return m.filterOn }

// Timestamps reports whether the stream is asked for server timestamps.
func (m *Model) Timestamps() bool { return m.timestamps }

// SetSources records which node each pod belongs to, as pod name to node
// display name, for the source labels.
func (m *Model) SetSources(sources map[string]string) { m.sources = sources }

// KeepViewPrefs carries the reader's display choices over from the pane
// this one replaces, so switching container does not undo them. Wrapping
// and timestamps carry over always; the labels only between two
// workflow-wide panes, since a pod-scoped pane starts without them.
func (m *Model) KeepViewPrefs(prev *Model) {
	if prev == nil {
		return
	}
	m.wrap = prev.wrap
	m.timestamps = prev.timestamps
	if prev.podName == "" && m.podName == "" {
		m.labels = prev.labels
	}
}

// SetPipeCommand records the command the pipe editor prefills with. The
// profile configures it; an empty value falls back to lnav.
func (m *Model) SetPipeCommand(cmd string) {
	if cmd = strings.TrimSpace(cmd); cmd != "" {
		m.pipeCmd = cmd
	}
}

func (m *Model) pipeDefault() string {
	if m.pipeCmd != "" {
		return m.pipeCmd
	}
	return "lnav"
}

// SetPhase records the stream lifecycle (root calls per logRecordMsg:
// running/done/canceled/err — batched delivery).
func (m *Model) SetPhase(p Phase) { m.phase = p }

// SetError records a sanitized, honest unavailability explanation
// (missing pods/GC'd logs name the cause; errors never include credentials).
func (m *Model) SetError(text string) {
	m.errText = strings.TrimSpace(text)
	if m.errText != "" {
		m.phase = PhaseError
	}
}

// SetNotice records why a stream may show nothing. The pane shows it below
// the status line for as long as no line has been retained.
func (m *Model) SetNotice(text string) { m.notice = strings.TrimSpace(text) }

// ApplyRecords feeds one batch of records into the buffer (the root
// delivers logRecordMsg batches; the component owns no
// transport). Applying records while paused must never move the viewport: the
// scroll position is only re-pinned to the tail when
// following.
func (m *Model) ApplyRecords(recs []core.LogRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(recs) > 0 {
		m.ensureContextHeaderLocked()
	}
	for _, r := range recs {
		m.buf.Push(r)
	}
	m.invalidateLocked()
}

// invalidateLocked marks the view snapshot stale (recomputed at render).
func (m *Model) invalidateLocked() { m.rowsFor = -1 }

// Update implements the child Tea model (children
// return intents; the root alone converts them to network effects).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Refresh the scroll geometry before key dispatch: scroll keys
		// and the pause freeze need absolute positions against the
		// current buffer, not a stale snapshot.
		m.mu.Lock()
		m.ensureSnapshot()
		m.mu.Unlock()
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return nil
	default:
		return nil
	}
}

// SetSize records the terminal size. A resize invalidates the snapshot and
// the search geometry (hits re-derive from a window that no longer
// matches the terminal).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.rowsFor = -1
}

// handleKey routes keys by focus mode (key isolation: text-entry
// modes consume printable input and never interpret command keys).
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	m.note = ""
	switch {
	case m.searchOn:
		return m.handleSearchKey(key)
	case m.contextOn:
		return m.handleContextKey(key)
	case m.pipeOn:
		return m.handlePipeKey(key)
	}
	// vim's gg: the first g arms, the second jumps to the oldest retained
	// line. Any other key disarms and is then handled normally.
	if m.gPending {
		m.gPending = false
		if key == "g" {
			m.jumpToTop()
			return nil
		}
	} else if key == "g" {
		m.gPending = true
		return nil
	}
	return m.handleBrowseKey(key)
}

// handleBrowseKey implements the log-viewer key matrix:
// Space pauses, t follows, / searches, & filters to the search, w wraps,
// L toggles the source labels, ctrl+t asks for server timestamps, c edits
// context, pgup/pgdown and j/k/arrows scroll. No transport is ever touched:
// ctrl+t returns an intent the root turns into a new stream.
func (m *Model) handleBrowseKey(key string) tea.Cmd {
	if key == "space" {
		key = " " // uv renders Space's keystroke as "space"
	}
	switch key {
	case " ":
		// Space pauses autoscroll. Pure state change: the
		// viewport freezes; collection upstream keeps flowing into the
		// bounded buffer (pause never grows memory unboundedly).
		m.paused = true
		m.follow = false
		// Freeze the window at the currently visible rows: while paused,
		// the same rows stay visible when new lines arrive.
		m.bottom = m.bottomAt()
		m.phase = PhasePaused
		return nil
	case "t":
		// `t` (tail) returns to follow. `f` is the global
		// full-screen raw key on every route, so it cannot mean follow
		// here as well.
		m.paused = false
		m.follow = true
		m.bottom = -1 // re-pin to tail on next render
		m.phase = PhaseStreaming
		return nil
	case "/":
		// `/` focuses retained-buffer search.
		m.searchOn = true
		m.searchBuf = m.search.term // prefill with the active term
		return nil
	case "|":
		// `|` hands the retained lines to another program. The command is
		// editable every time: the reader knows what they want to look at
		// with far better than micko does.
		m.pipeOn = true
		m.pipeBuf = m.pipeDefault()
		return nil
	case "c":
		// `c` opens the pod/container context editor. The
		// current values prefill: default "main" is visible, editable.
		m.contextOn = true
		m.podBuf = m.podName
		m.containerBuf = m.container
		m.contextErr = ""
		return nil
	case "pgup":
		m.scrollBy(-m.pageRows())
		return nil
	case "pgdown":
		m.scrollBy(m.pageRows())
		return nil
	case "ctrl+u":
		m.scrollBy(-m.pageRows())
		return nil
	case "ctrl+d":
		m.scrollBy(m.pageRows())
		return nil
	case "up", "k":
		m.scrollBy(-1)
		return nil
	case "down", "j":
		m.scrollBy(1)
		return nil
	case "G", "end":
		// Jump to the newest line and start following again: reaching the
		// bottom of a live log is a request to keep watching it.
		m.paused = false
		m.follow = true
		m.bottom = -1
		m.phase = PhaseStreaming
		return nil
	case "home":
		m.jumpToTop()
		return nil
	case "n":
		// vim's n/N step through the search hits.
		m.jumpToHit(1)
		return nil
	case "N":
		m.jumpToHit(-1)
		return nil
	case "w":
		// Wrapping changes how many screen lines each row takes but not
		// which row is at the bottom, so the reader stays on their line.
		m.wrap = !m.wrap
		m.left = 0
		return nil
	case "h", "left":
		m.pan(-m.panStep())
		return nil
	case "l", "right":
		m.pan(m.panStep())
		return nil
	case "0":
		m.left = 0
		return nil
	case "$":
		m.pan(m.maxLeft())
		return nil
	case "L":
		m.labels = !m.labels
		return nil
	case "&":
		// less's &: show only the lines matching the / search. It needs a
		// search to filter by; without one it says so rather than showing
		// nothing.
		if m.filterOn {
			m.setFilter(false)
			return nil
		}
		if m.search.term == "" {
			m.note = "& shows the lines matching a / search; search first"
			return nil
		}
		m.setFilter(true)
		return nil
	case "esc":
		// The root routes esc here only while the filter is on (see
		// EscapeClears); otherwise esc leaves the pane.
		if m.filterOn {
			m.setFilter(false)
		}
		return nil
	case "ctrl+t":
		// Server timestamps are a property of the request, so switching
		// them reopens the stream. The lines already retained stay, and a
		// marker shows where the new stream starts: it replays the log
		// from the beginning with the new setting.
		m.timestamps = !m.timestamps
		m.mu.Lock()
		kind := markTimestampsOff
		if m.timestamps {
			kind = markTimestampsOn
		}
		m.buf.PushMarker(kind, m.podName, m.container)
		m.invalidateLocked()
		m.mu.Unlock()
		m.phase = PhaseStreaming
		m.errText = ""
		intent := TimestampsIntent{Ref: m.ref, PodName: m.podName, Container: m.container, On: m.timestamps}
		return func() tea.Msg { return intent }
	default:
		// Everything else (q, ?, ...) belongs to the root:
		// the child must not shadow global keys.
		return nil
	}
}

// panStep is one horizontal pan: half the text width, so a cut word stays in
// view.
func (m *Model) panStep() int {
	return max(1, m.textWidth(row{kind: rowLog})/2)
}

// pan moves the left edge by delta cells, no further than the widest
// visible line needs. Wrapped lines ignore it.
func (m *Model) pan(delta int) {
	m.left = min(max(m.left+delta, 0), m.maxLeft())
}

// maxLeft is the left edge that brings the end of the widest visible log
// line to the pane's right edge.
func (m *Model) maxLeft() int {
	most := 0
	for _, r := range m.window() {
		if r.kind == rowLog {
			most = max(most, ansi.StringWidth(r.text)-m.textWidth(r))
		}
	}
	return most
}

// pageRows is the scroll step for pgup/pgdown (viewport minus one line of
// overlap). With wrapping on, the step is the rows on screen less one,
// since a page of screen lines holds fewer rows than lines.
func (m *Model) pageRows() int {
	if m.wrap {
		if n := len(m.window()); n > 1 {
			return n - 1
		}
		return 1
	}
	h := m.viewRows()
	if h < 1 {
		return 1
	}
	return h - 1
}

// setFilter turns the & filter on or off and keeps the reader on the same
// line: the row at the bottom of the pane stays at the bottom when it is
// still shown, and otherwise the nearest line above it takes its place. A
// following pane stays on the tail.
func (m *Model) setFilter(on bool) {
	m.ensureSnapshot()
	anchor := -1
	if m.bottom >= 0 && len(m.rows) > 0 {
		for i := m.bottomAt(); i >= 0; i-- {
			if m.rows[i].logIdx >= 0 {
				anchor = m.rows[i].logIdx
				break
			}
		}
	}
	m.filterOn = on
	m.invalidateLocked()
	m.ensureSnapshot()
	if m.bottom < 0 {
		return
	}
	m.bottom = 0
	for i, r := range m.rows {
		if r.logIdx >= 0 && r.logIdx <= anchor {
			m.bottom = i
		}
	}
	m.bottom = m.clampBottomRaw(m.bottom)
}

// jumpToTop pins the viewport to the oldest retained line and detaches
// follow, because the reader asked to look at history.
func (m *Model) jumpToTop() {
	m.ensureSnapshot()
	m.bottom = m.clampBottomRaw(m.viewRows() - 1)
	m.follow = false
	m.paused = true
}

// scrollBy moves the viewport by delta rows. Scrolling while paused does
// not re-pin the tail; scrolling while following detaches follow (the
// user asked to read history).
func (m *Model) scrollBy(delta int) {
	m.bottom = m.clampBottom(m.bottomAt(), delta)
	if m.follow {
		m.follow = false
		m.paused = true
	}
}

// printable turns a key name into the character it inserts into a text
// editor. Space arrives as the name "space", so without this no search term
// and no pipe command could ever contain one.
func printable(key string) (string, bool) {
	if key == "space" {
		return " ", true
	}
	if runes := []rune(key); len(runes) == 1 && runes[0] >= 0x20 {
		return key, true
	}
	return "", false
}

// handleSearchKey routes keys while search has focus (printable
// insertion only; enter applies over the retained buffer; esc cancels).
func (m *Model) handleSearchKey(key string) tea.Cmd {
	switch key {
	case "esc":
		// Cancel: drop the uncommitted buffer, restore the previous
		// committed search state (Esc cancels).
		m.searchOn = false
		m.searchBuf = ""
		return nil
	case "enter":
		term := strings.TrimSpace(m.searchBuf)
		m.searchOn = false
		m.searchBuf = ""
		m.applySearch(term)
		return nil
	case "backspace":
		r := []rune(m.searchBuf)
		if len(r) > 0 {
			m.searchBuf = string(r[:len(r)-1])
		}
		return nil
	default:
		if ch, ok := printable(key); ok {
			m.searchBuf += ch
		}
		return nil
	}
}

// applySearch commits a search term and resolves hits over the retained
// buffer (scope is the retained lines only; the view states it).
func (m *Model) applySearch(term string) {
	m.search = searchState{term: term}
	m.curHit = 0
	if term == "" {
		m.hits = nil
		m.hitRows = nil
		m.searchScopeN = 0
		// The filter shows the search's lines; with no search it has
		// nothing to show.
		m.filterOn = false
	} else {
		lines := m.buf.Lines()
		m.searchScopeN = len(lines)
		m.hits = matchIndices(lines, term, m.search.caseSensitive)
	}
	// The row set did not change, but the hit map did.
	m.invalidateLocked()
	if len(m.hits) > 0 {
		// Land on the first hit: a search that reports 12 matches and shows
		// none of them is the complaint this answers.
		m.curHit = len(m.hits) - 1
		m.jumpToHit(1)
	}
}

// jumpToHit moves the viewport to the next (delta 1) or previous (delta -1)
// search hit and makes it the current one. The list wraps, because a reader
// pressing n at the last match wants the first one, not a dead key.
//
// Jumping detaches follow: the reader asked to look at a specific line, and a
// live tail would drag them off it within a second.
func (m *Model) jumpToHit(delta int) {
	m.ensureSnapshot()
	if len(m.hits) == 0 {
		return
	}
	n := len(m.hits)
	next := m.curHit
	// Walk at most one full turn, so hits whose row fell out of the retained
	// buffer are stepped over instead of freezing the key.
	for i := 0; i < n; i++ {
		next = ((next+delta)%n + n) % n
		if row := m.hitRowAt(next); row >= 0 {
			m.curHit = next
			m.follow = false
			m.paused = true
			m.phase = PhasePaused
			// Park the hit one line above the bottom edge where there is room,
			// so the lines after it are visible too.
			m.bottom = m.clampBottomRaw(row + 1)
			return
		}
	}
}

// hitRowAt resolves one hit to its scrollable row, or -1.
func (m *Model) hitRowAt(i int) int {
	if i < 0 || i >= len(m.hitRows) {
		return -1
	}
	return m.hitRows[i]
}

// currentHitRow is the row the current hit sits on, or -1.
func (m *Model) currentHitRow() int { return m.hitRawRow() }

func (m *Model) hitRawRow() int {
	if len(m.hits) == 0 {
		return -1
	}
	return m.hitRowAt(m.curHit)
}

// PipeIntent asks the root to run Command with the retained log lines on its
// standard input. The component never starts a process: the root owns every
// effect, and this one takes the whole terminal.
type PipeIntent struct {
	Ref     core.Ref
	Command string
}

// handlePipeKey routes keys while the pipe editor has focus. An empty command
// is refused rather than defaulted: running something the reader did not type
// with the whole screen is not a place for a guess.
func (m *Model) handlePipeKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.pipeOn = false
		m.pipeBuf = ""
		return nil
	case "enter":
		cmd := strings.TrimSpace(m.pipeBuf)
		if cmd == "" {
			return nil
		}
		m.pipeOn = false
		m.pipeBuf = ""
		ref := m.ref
		return func() tea.Msg { return PipeIntent{Ref: ref, Command: cmd} }
	case "backspace":
		r := []rune(m.pipeBuf)
		if len(r) > 0 {
			m.pipeBuf = string(r[:len(r)-1])
		}
		return nil
	default:
		if ch, ok := printable(key); ok {
			m.pipeBuf += ch
		}
		return nil
	}
}

// handleContextKey routes keys while the pod/container editor has focus.
// Enter emits the SwitchContextIntent for the root; blank container is
// refused with a visible message (the default "main" is prefilled and
// editable — never silently guessed). Esc cancels without
// emitting anything.
func (m *Model) handleContextKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.contextOn = false
		m.podBuf, m.containerBuf = "", ""
		m.contextErr = ""
		return nil
	case "enter":
		container := strings.TrimSpace(m.containerBuf)
		if container == "" {
			// Refuse: no silent default, editor stays open.
			m.contextErr = "container must not be empty (\"main\" is the editable default)"
			return nil
		}
		m.contextOn = false
		m.contextErr = ""
		m.podName = strings.TrimSpace(m.podBuf)
		m.container = container
		m.bottom = -1
		m.phase = PhaseStreaming
		return func() tea.Msg {
			return SwitchContextIntent{
				Ref:       m.ref,
				PodName:   m.podName,
				Container: m.container,
			}
		}
	case "backspace":
		r := []rune(m.containerBuf)
		if len(r) > 0 {
			m.containerBuf = string(r[:len(r)-1])
		}
		return nil
	case "left":
		return m.podLeft()
	case "right":
		return m.podRight()
	default:
		if ch, ok := printable(key); ok {
			// Backspace (visual) already handled for container; pod
			// editing uses left/right + insert semantics below.
			m.containerBuf += ch
		}
		return nil
	}
}

// podLeft/podRight implement minimal pod-field editing (cursor-less:
// backspace-on-pod reuses the container path; left/right trim characters
// from the pod buffer so a mistyped pod is correctable without Esc).
func (m *Model) podLeft() tea.Cmd {
	r := []rune(m.podBuf)
	if len(r) > 0 {
		m.podBuf = string(r[:len(r)-1])
	}
	return nil
}

func (m *Model) podRight() tea.Cmd {
	// right at the pod field appends a space separator (editing aid).
	m.podBuf += " "
	return nil
}

// TimestampsIntent asks the root to reopen the stream with server
// timestamps on or off. The component never dials anything; it
// has already marked the switch point in its buffer.
type TimestampsIntent struct {
	Ref       core.Ref
	PodName   string
	Container string
	On        bool
}

// SwitchContextIntent is the pod/container context-switch intent.
// The root alone converts it to a stream effect; this component never
// dials anything.
type SwitchContextIntent struct {
	Ref       core.Ref
	PodName   string
	Container string
}
