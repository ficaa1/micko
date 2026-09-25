// Package workflowlist is the B1 list component: a keyboard-driven list of
// workflow summaries over injected core values (plan §8 B1).
//
// Contract rules pinned here:
//   - It is a child Tea model (New/Update/View/SetSize) accepting core
//     values, never HTTP clients, and never starts goroutines (plan §4).
//   - Selection identity is UID (plus namespace+name), never a row index
//     (plan §2; LIST-12): reordering or deletion keeps the selection on the
//     same workflow.
//   - Open intent is emitted as app.OpenWorkflowMsg / app.OpenLogsMsg; the
//     root alone converts intents to network effects (plan §4).
//   - Search is local over the injected snapshot and the view says so
//     (plan §5; LIST-05/09): no claim of searching un-fetched workflows.
//   - Untrusted text is sanitized with the shared sanitizer before render
//     (plan §5; SEC-01/02); styling is applied separately from content.
//   - All state transitions (loading/stale/incomplete/forbidden/empty) are
//     distinguishable in the view (UI-07).
package workflowlist

import (
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// Status is the list-level state surfaced distinctly by the view (UI-07).
// It mirrors the root's listState facts the parent passes down; the
// component never guesses network state.
type Status int

const (
	// StatusIdle: snapshot loaded, no exceptional condition.
	StatusIdle Status = iota
	// StatusLoading: a snapshot collection is running (root's listState.loading).
	StatusLoading
	// StatusStale: last poll errored; last good data shown with age.
	StatusStale
	// StatusIncomplete: collection capped (plan §5: visible incomplete state
	// and search scope honesty — the snapshot is not the whole namespace).
	StatusIncomplete
	// StatusForbidden: the server denied the list (authoritative; CONN-13).
	StatusForbidden
	// StatusUnauthenticated: 401 with guidance; do not retry silently.
	StatusUnauthenticated
)

// Model is the workflow list child model.
type Model struct {
	items  []core.Summary // the current snapshot as injected (unsorted master)
	rows   []core.Summary // sorted+filtered view over items (display order)
	selUID string         // selected workflow identity: UID + namespace + name
	phase  PhaseFilter    // local phase filter bucket
	query  string         // local search text (snapshot-scoped)
	sort   SortKey        // current sort
	status Status
	errMsg string        // sanitized error/stale-reason text when StatusStale/...
	errAge time.Duration // stale age when StatusStale (root supplies)

	// searchBuf holds the raw search text while the search input is
	// focused (UI-04: navigation keys must not be reinterpreted while
	// typing). The component owns this minimal buffer directly — no
	// bubbles/textinput (its clipboard dependency is outside the pinned
	// module set); rendering of the entry line is plain text.
	searchBuf string
	searchCur int // rune-index cursor into searchBuf (grapheme-free is fine for names)
	SearchOn  bool
	// queryBefore is the applied query at the moment the search input took
	// focus. Typing filters live, so Esc has to restore something; without
	// this, cancelling a search would silently discard the filter the user
	// already had.
	queryBefore string

	// gPending is the armed half of vim's gg. It is cleared by the next key
	// press whatever that key is, so it can never leak into a later command.
	gPending bool

	// width/height are the last-known size (SetSize).
	width, height int

	// scrollTop is the first visible row index of the viewport window. It is
	// an anchor, not an identity: the window only moves far enough to keep
	// the UID-selected row visible, so scrolling stays stable while the
	// selection itself is never index-based (LIST-12).
	scrollTop int

	// winStart/winEnd are the row range the last render actually showed.
	// WindowStatus reports them to the shell footer; they are render output,
	// never input to selection (LIST-12 keeps selection UID-based).
	winStart, winEnd int

	// counts for honest scope display: visible vs total snapshot size.
	visible int
	total   int

	// theme is injected (shared.Theme) so goldens can force no-color.
	theme shared.Theme

	// noColor mirrors the theme's plainness for ASCII snapshot tests.
	noColor bool
}

// New builds an empty list model. Items/status arrive via SetItems and
// SetStatus; the model never fetches by itself (plan §4).
func New(theme shared.Theme, noColor bool) Model {
	return Model{
		phase: PhaseAll,
		sort:  SortPhaseName,
		theme: theme,
	}
}

// SetItems replaces the snapshot (root calls after listLoadedMsg). The
// selected workflow is preserved by UID identity across the replacement
// (reorder/deletion-safe; LIST-12). Sorting/filtering is reapplied.
func (m *Model) SetItems(items []core.Summary, now time.Time) {
	m.items = items
	m.total = len(items)
	m.reselect(now)
	m.applyView()
}

// Rows returns the currently displayed rows in display order. It is a read
// of what the pane shows, so callers that export the view (raw text, copy)
// export exactly what the reader sees.
func (m *Model) Rows() []core.Summary {
	out := make([]core.Summary, len(m.rows))
	copy(out, m.rows)
	return out
}

// SelectedRef returns the currently selected workflow's Ref, or zero.
func (m *Model) SelectedRef() core.Ref {
	for _, it := range m.rows {
		if it.Ref.UID == m.selUID {
			return it.Ref
		}
	}
	return core.Ref{}
}

// Selected returns the full selected summary (zero value when none).
func (m *Model) Selected() core.Summary {
	for _, it := range m.rows {
		if it.Ref.UID == m.selUID {
			return it
		}
	}
	return core.Summary{}
}

// HasSelection reports whether the selection still points at a visible row.
func (m *Model) HasSelection() bool {
	return m.SelectedRef().UID != ""
}

// SetStatus updates the list-level status with sanitized message text and
// (for stale) the age of the last good data.
func (m *Model) SetStatus(s Status, msg string, staleAge time.Duration) {
	m.status = s
	m.errMsg = sanitizeOne(msg)
	m.errAge = staleAge
}

// SetPhase rotates/sets the phase filter.
func (m *Model) SetPhase(p PhaseFilter) { m.phase = p; m.applyView() }

// CyclePhase advances the phase filter bucket (p key handling stays with
// the root; the component exposes the intent).
func (m *Model) CyclePhase() {
	m.phase = m.phase.Next()
	m.selUID = ""
	m.scrollTop = 0
	m.applyView()
}

// CycleSort advances the sort key and parks the cursor back at the top.
//
// Selection is by UID, so without this the cursor would ride its workflow
// down into the new order: after sorting to newest-first the reader would be
// left somewhere in the middle, scrolling up to reach the rows they asked
// for. Sorting is a request to look at the top of a new order.
func (m *Model) CycleSort() {
	m.sort = m.sort.Next()
	m.selUID = ""
	m.scrollTop = 0
	m.applyView()
}

// SetQuery applies the local search text (snapshot-scoped; LIST-05/09).
func (m *Model) SetQuery(q string) { m.query = q; m.applyView() }

// Query returns the active search text.
func (m *Model) Query() string { return m.query }

// SortKey returns the active sort.
func (m *Model) SortKey() SortKey { return m.sort }

// PhaseFilter returns the active phase bucket.
func (m *Model) PhaseFilter() PhaseFilter { return m.phase }

// VisibleCount / TotalCount expose honest scope numbers (view shows
// "visible/total" and marks the search as snapshot-scoped).
func (m *Model) VisibleCount() int { return m.visible }
func (m *Model) TotalCount() int   { return m.total }

// applyView recomputes the display rows from items: filter by phase bucket
// + name substring, then sort. Selection survives by UID (never reindexed
// by position — plan gate "no row-index identity").
func (m *Model) applyView() {
	rows := make([]core.Summary, 0, len(m.items))
	for _, it := range m.items {
		if !m.phase.Matches(it) {
			continue
		}
		if !nameMatches(it.Ref.Name, m.query) {
			continue
		}
		rows = append(rows, it)
	}
	m.rows = Sort(rows, m.sort)
	m.visible = len(m.rows)

	// Re-anchor selection to the same UID if it survived filtering; else
	// drop it (deletion case) — the view shows "no selection".
	if m.selUID != "" {
		found := false
		for _, r := range m.rows {
			if r.Ref.UID == m.selUID {
				found = true
				break
			}
		}
		if !found {
			m.selUID = ""
		}
	}
	// Default to the first row when nothing is selected (initial state or
	// selection filtered out).
	if m.selUID == "" && len(m.rows) > 0 {
		m.selUID = m.rows[0].Ref.UID
	}
}

// reselect preserves the UID selection across a snapshot replacement and
// clears it if the workflow disappeared (deletion between polls; LIST-12/13).
func (m *Model) reselect(_ time.Time) {
	if m.selUID == "" {
		return
	}
	for _, it := range m.items {
		if it.Ref.UID == m.selUID {
			return // still present: keep
		}
	}
	m.selUID = ""
}

// selIndex returns the row position of the selection, or -1.
func (m *Model) selIndex() int {
	for i, r := range m.rows {
		if r.Ref.UID == m.selUID {
			return i
		}
	}
	return -1
}

// move moves selection by delta (j/k/arrows), clamped to bounds.
func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		m.selUID = ""
		return
	}
	i := m.selIndex()
	if i < 0 {
		i = 0
	} else {
		i += delta
	}
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	m.selUID = m.rows[i].Ref.UID
}

// OpenIntent returns the open-workflow intent for the selected row, or nil.
// The ROOT converts it to a network effect (plan §4).
func (m *Model) OpenIntent() tea.Msg {
	sel := m.Selected()
	if sel.Ref.UID == "" {
		return nil
	}
	return shared.OpenWorkflowMsg{Ref: sel.Ref}
}

// OpenSectionIntent returns the intent that opens the selected workflow on
// one detail section, or nil when nothing is selected.
func (m *Model) OpenSectionIntent(section string) tea.Msg {
	sel := m.Selected()
	if sel.Ref.UID == "" {
		return nil
	}
	return shared.OpenWorkflowMsg{Ref: sel.Ref, Section: section}
}

// LogsIntent returns the open-logs intent for the selected row, or nil.
// Container defaults to "main" visibly (plan §2; the UI for editing the
// container lives in the logs view, D1).
func (m *Model) LogsIntent() tea.Msg {
	sel := m.Selected()
	if sel.Ref.UID == "" {
		return nil
	}
	return shared.OpenLogsMsg{Ref: sel.Ref, Container: "main"}
}

// RefreshIntent is the manual refresh request (r). The frozen app contract
// has no RefreshListMsg; the root already polls and coalesces (LIST-07), so
// the component emits a lightweight local signal the root can interpret.
// A dedicated frozen message would need an F-gated contract amendment.
type RefreshListMsg struct{}

func (m *Model) RefreshIntent() tea.Msg { return RefreshListMsg{} }

// SearchValue returns the in-progress search text while the input has
// focus (testing/inspection only; the applied query is Query()).
func (m *Model) SearchValue() string { return m.searchBuf }

// SearchSetValue replaces the buffer contents and parks the cursor at the
// end (used by tests and by commit/apply paths).
func (m *Model) SearchSetValue(s string) {
	m.searchBuf = s
	m.searchCur = len([]rune(s))
}

// searchInsert inserts a rune at the cursor (text-entry editing).
func (m *Model) searchInsert(r rune) {
	runes := []rune(m.searchBuf)
	at := m.searchCur
	if at < 0 || at > len(runes) {
		at = len(runes)
	}
	edited := make([]rune, 0, len(runes)+1)
	edited = append(edited, runes[:at]...)
	edited = append(edited, r)
	edited = append(edited, runes[at:]...)
	m.searchBuf = string(edited)
	m.searchCur = at + 1
}

// searchBackspace deletes the rune before the cursor, if any.
func (m *Model) searchBackspace() {
	runes := []rune(m.searchBuf)
	at := m.searchCur
	if at <= 0 || at > len(runes) {
		return
	}
	edited := make([]rune, 0, len(runes)-1)
	edited = append(edited, runes[:at-1]...)
	edited = append(edited, runes[at:]...)
	m.searchBuf = string(edited)
	m.searchCur = at - 1
}

// searchLeft / searchRight move the cursor within the buffer bounds.
func (m *Model) searchLeft() {
	if m.searchCur > 0 {
		m.searchCur--
	}
}

func (m *Model) searchRight() {
	if m.searchCur < len([]rune(m.searchBuf)) {
		m.searchCur++
	}
}

// SetSize records the terminal size (child model contract, plan §4).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
}

// SetTheme replaces the style set the list is drawn in.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// sanitizeOne is the single render-path sanitization choke point for
// server-derived strings in this component (SEC-02).
func sanitizeOne(s string) string { return shared.Sanitize(s) }
