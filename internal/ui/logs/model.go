// Package logs is the D1 log viewer component: a bounded, keyboard-driven
// log surface over injected core values (plan §8 D1).
//
// Contract rules pinned here:
//   - It is a child Tea model (New/Update/View/SetSize) accepting core
//     values, never HTTP clients; it never starts goroutines and never
//     owns transport (plan §4: "no transport goroutines in component" —
//     the root batches deliveries via ApplyRecords).
//   - Retention is bounded two ways: MaxLines entries and MaxBytes of
//     text, whichever hits first; oversize is truncated or dropped with
//     visible markers, never silently clipped (LOG-04/06).
//   - Pause (Space) freezes autoscroll only; incoming records keep
//     flowing into the bounded buffer and can never grow memory without
//     bound (LOG-07); while paused, arriving lines never move the
//     viewport (no autoscroll jump while paused — plan gate).
//   - `f` returns to follow (LOG-08). Search is over the retained buffer
//     only, with the scope visible (LOG-09). Container default "main" is
//     visible/editable and switching is an intent for the root (LOG-10).
//   - Every untrusted string passes the shared sanitizer before render
//     (SEC-01/02); duplicates are legitimate output and never deduped.
package logs

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/core"
)

// Phase is the stream lifecycle surfaced by the status row (distinguishable
// states, plan §2; UI-07).
type Phase int

const (
	// PhaseIdle: attached, no stream yet (or after a context reset).
	PhaseIdle Phase = iota
	// PhaseStreaming: a stream is delivering records.
	PhaseStreaming
	// PhasePaused: collection continues upstream, viewport is frozen.
	PhasePaused
	// PhaseEnded: clean end-of-stream.
	PhaseEnded
	// PhaseError: stream failed; error text is shown (honest state).
	PhaseError
	// PhaseCanceled: the user navigated away / canceled; reconnects are
	// labeled, never silent.
	PhaseCanceled
)

// String renders the phase as the status badge (text accompanies any
// styling — color is never the only carrier, plan §2).
func (p Phase) String() string {
	switch p {
	case PhaseStreaming:
		return "FOLLOWING"
	case PhasePaused:
		return "PAUSED"
	case PhaseEnded:
		return "ENDED"
	case PhaseError:
		return "ERROR"
	case PhaseCanceled:
		return "CANCELED"
	default:
		return "IDLE"
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
	errText string // sanitized error/unavailable explanation (LOG-12)

	search       searchState // committed search
	searchBuf    string      // keystroke buffer while focused
	searchOn     bool        // search input has focus (UI-04 isolation)
	hits         []int       // match indices over retained log lines
	searchScopeN int         // lines the search ran over (scope honesty)

	contextOn    bool   // pod/container editor has focus
	podBuf       string // editor keystroke buffer (pod)
	containerBuf string // editor keystroke buffer (container)
	contextErr   string // editor validation message (blank container refused)

	width, height int // last known terminal size (SetSize)
	noColor       bool

	headerDone bool // stream-open marker emitted for this context

	mu sync.Mutex // guards Apply*/Clear for test/bench callers; the Tea
	// update loop itself is serial.
}

// NewModel builds a log viewer for ref with explicit context. podName ""
// means workflow-wide logs (rendered "(all pods)"). container "" is
// normalized to the visible, editable default "main" — never silently
// guessed (plan §2; LOG-10).
func NewModel(ref core.Ref, podName, container string) *Model {
	if container == "" {
		container = "main"
	}
	return &Model{
		ref:       ref,
		podName:   podName,
		container: container,
		buf:       NewBuffer(MaxLines, MaxBytes),
		follow:    true,
		bottom:    -1, // pinned to tail
		rowsFor:   -1,
		phase:     PhaseStreaming,
		// The stream's first line is annotated with its context header
		// (pod:container) so provenance is visible in the transcript.
	}
}

// init emits the stream-open marker for the current context (called by
// the root when attaching the component to a stream; ApplyRecords on a
// fresh model with no header yet emits it lazily so tests and the demo
// path cannot skip it).
func (m *Model) ensureContextHeader() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContextHeaderLocked()
}

func (m *Model) ensureContextHeaderLocked() {
	if m.headerDone {
		return
	}
	m.headerDone = true
	m.buf.PushMarker(markOpen, m.podName, m.container)
	m.rowsFor = -1
}

// SetNoColor forces the plain theme (golden determinism; NO_COLOR).
func (m *Model) SetNoColor(v bool) { m.noColor = v }

// Ref returns the workflow the viewer is attached to.
func (m *Model) Ref() core.Ref { return m.ref }

// Context returns the current pod/container scope. podName "" means
// workflow-wide.
func (m *Model) Context() (podName, container string) {
	return m.podName, m.container
}

// Phase reports the stream lifecycle.
func (m *Model) Phase() Phase { return m.phase }

// Paused reports the manual pause state.
func (m *Model) Paused() bool { return m.paused }

// Following reports autoscroll state.
func (m *Model) Following() bool { return m.follow }

// SetPhase records the stream lifecycle (root calls per logRecordMsg:
// running/done/canceled/err — plan §4 batched delivery).
func (m *Model) SetPhase(p Phase) { m.phase = p }

// SetError records a sanitized, honest unavailability explanation (LOG-12:
// missing pods/GC'd logs name the cause; errors never include credentials).
func (m *Model) SetError(text string) {
	m.errText = strings.TrimSpace(text)
	if m.errText != "" {
		m.phase = PhaseError
	}
}

// ApplyRecords feeds one batch of records into the buffer (the root
// delivers logRecordMsg batches — plan §5; the component owns no
// transport). Applying records while paused must never move the viewport
// (plan gate): the scroll position is only re-pinned to the tail when
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

// ApplyMarker appends one stream-context annotation (the root converts
// stream events to these — disconnect/reconnect/gap; LOG-11).
func (m *Model) ApplyMarker(kind markerKind, podName, container string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind == markOpen {
		// An explicit open marker replaces the lazy one for this context.
		m.headerDone = true
	}
	m.buf.PushMarker(kind, podName, container)
	m.invalidateLocked()
}

// NewStream annotates a fresh stream attempt on the same buffer: the next
// pushed line is preceded by the reconnect gap marker (LOG-11: "new
// stream; overlap/gap possible").
func (m *Model) NewStream() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureContextHeaderLocked()
	m.buf.SetReconnect()
	m.invalidateLocked()
}

// Clear drops all retained entries (route change to a different
// workflow/context starts empty; stale streams never bleed across).
func (m *Model) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = NewBuffer(MaxLines, MaxBytes)
	m.hits = nil
	m.search = searchState{}
	m.searchBuf = ""
	m.searchOn = false
	m.headerDone = false
	m.invalidateLocked()
}

// invalidateLocked marks the view snapshot stale (recomputed at render).
func (m *Model) invalidateLocked() { m.rowsFor = -1 }

// Update implements the child Tea model (plan §4 frozen surface: children
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

// handleKey routes keys by focus mode (UI-04 key isolation: text-entry
// modes consume printable input and never interpret command keys).
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	switch {
	case m.searchOn:
		return m.handleSearchKey(key)
	case m.contextOn:
		return m.handleContextKey(key)
	default:
		return m.handleBrowseKey(key)
	}
}

// handleBrowseKey implements the log-viewer key matrix (plan §2 Logs row):
// Space pauses, f follows, / searches, c edits context, pgup/pgdown and
// j/k/arrows scroll. No transport is ever touched.
func (m *Model) handleBrowseKey(key string) tea.Cmd {
	if key == "space" {
		key = " " // uv renders Space's keystroke as "space"
	}
	switch key {
	case " ":
		// Space pauses autoscroll (LOG-07). Pure state change: the
		// viewport freezes; collection upstream keeps flowing into the
		// bounded buffer (pause never grows memory unboundedly).
		m.paused = true
		m.follow = false
		// Freeze the window at the currently visible rows: while paused,
		// the same rows stay visible when new lines arrive (plan gate).
		m.bottom = m.bottomAt()
		m.phase = PhasePaused
		return nil
	case "f":
		// `f` returns to follow (LOG-08).
		m.paused = false
		m.follow = true
		m.bottom = -1 // re-pin to tail on next render
		m.phase = PhaseStreaming
		return nil
	case "/":
		// `/` focuses retained-buffer search (LOG-09).
		m.searchOn = true
		m.searchBuf = m.search.term // prefill with the active term
		return nil
	case "c":
		// `c` opens the pod/container context editor (LOG-10). The
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
	case "up", "k":
		m.scrollBy(-1)
		return nil
	case "down", "j":
		m.scrollBy(1)
		return nil
	default:
		// Everything else (q, n, p, ?, ...) belongs to the root (UI-04):
		// the child must not shadow global keys.
		return nil
	}
}

// pageRows is the scroll step for pgup/pgdown (viewport minus one line of
// overlap).
func (m *Model) pageRows() int {
	h := m.viewRows()
	if h < 1 {
		return 1
	}
	return h - 1
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

// handleSearchKey routes keys while search has focus (UI-04: printable
// insertion only; enter applies over the retained buffer; esc cancels).
func (m *Model) handleSearchKey(key string) tea.Cmd {
	switch key {
	case "esc":
		// Cancel: drop the uncommitted buffer, restore the previous
		// committed search state (Esc cancels, plan §2).
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
		if runes := []rune(key); len(runes) == 1 && runes[0] >= 0x20 {
			m.searchBuf += key
		}
		return nil
	}
}

// applySearch commits a search term and resolves hits over the retained
// buffer (LOG-09: scope is the retained lines only; the view states it).
func (m *Model) applySearch(term string) {
	m.search = searchState{term: term}
	if term == "" {
		m.hits = nil
		m.searchScopeN = 0
	} else {
		lines := m.buf.Lines()
		m.searchScopeN = len(lines)
		m.hits = matchIndices(lines, term, m.search.caseSensitive)
	}
	// Rebuild the snapshot so the [match N] overlay renders (the row set
	// did not change, but its annotations did).
	m.invalidateLocked()
}

// ClearSearch drops the active search (root/test entry point).
func (m *Model) ClearSearch() {
	m.search = searchState{}
	m.searchBuf = ""
	m.hits = nil
	m.searchScopeN = 0
}

// handleContextKey routes keys while the pod/container editor has focus.
// Enter emits the SwitchContextIntent for the root; blank container is
// refused with a visible message (the default "main" is prefilled and
// editable — never silently guessed, plan §2/LOG-10). Esc cancels without
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
			// Refuse: no silent default, editor stays open (LOG-10).
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
		if runes := []rune(key); len(runes) == 1 && runes[0] >= 0x20 {
			// Backspace (visual) already handled for container; pod
			// editing uses left/right + insert semantics below.
			m.containerBuf += key
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

// SwitchContextIntent is the pod/container context-switch intent (LOG-10).
// The root alone converts it to a stream effect; this component never
// dials anything (plan §4).
type SwitchContextIntent struct {
	Ref       core.Ref
	PodName   string
	Container string
}

// OpenIntent returns the frozen app intent to (re)open logs for the
// current ref/context — the fresh-open path uses the shared contract
// (docs/contracts.md §4). Nil when the context is degenerate.
func (m *Model) OpenIntent() tea.Msg {
	if m.ref.Name == "" {
		return nil
	}
	return app.OpenLogsMsg{
		Ref:       m.ref,
		PodName:   m.podName,
		Container: m.container,
	}
}
