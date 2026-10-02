// Package workflowlist is the keyboard-driven workflow pane over injected snapshots.
package workflowlist

import (
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Status is the list-level state surfaced distinctly by the view.
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
	// StatusIncomplete: collection capped (visible incomplete state
	// and search scope honesty — the snapshot is not the whole namespace).
	StatusIncomplete
	// StatusForbidden: the server denied the list (authoritative).
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
	query  string         // applied filter text (snapshot-scoped); always parses
	sort   SortKey        // current sort
	// match is query parsed. It is rebuilt only when the query changes, so
	// a snapshot refresh re-filters without re-parsing.
	match Matcher
	// queryErr is why the text being typed does not parse, empty when it
	// does. The previous filter stays applied meanwhile, and the toolbar
	// shows this, so the reader can tell why the rows stopped changing.
	queryErr string
	// now is the snapshot time: the clock reading that came with the last
	// SetItems. The age and duration predicates measure from it, so a
	// filter answers for the snapshot on screen.
	now time.Time
	// wide adds the metadata columns (the w key).
	wide   bool
	status Status
	errMsg string        // sanitized error/stale-reason text when StatusStale/...
	errAge time.Duration // stale age when StatusStale (root supplies)

	// searchBuf holds the raw search text while the search input is
	// focused (navigation keys must not be reinterpreted while
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
	// history holds the filters applied with enter this session, oldest
	// first. histPos is the entry on screen while recalling; len(history)
	// means the reader's own draft, which draft keeps.
	history []string
	histPos int
	draft   string
	// tabs is the completion tab is cycling through, nil once any other key
	// is pressed.
	tabs *tabCycle
	// picking lets the completion picker open. Typing sets it; opening the
	// input, recalling, accepting a value and esc clear it, so the picker
	// never covers the list before the reader types. pickSel is the
	// highlighted value.
	picking bool
	pickSel int

	// gPending is the armed half of vim's gg. It is cleared by the next key
	// press whatever that key is, so it can never leak into a later command.
	gPending bool

	// width/height are the last-known size (SetSize).
	width, height int

	// scrollTop is the first visible row index of the viewport window. It is
	// an anchor, not an identity: the window only moves far enough to keep
	// the UID-selected row visible, so scrolling stays stable while the
	// selection itself is never index-based.
	scrollTop int

	// winStart/winEnd are the row range the last render actually showed.
	// WindowStatus reports them to the shell footer; they are render output,
	// never input to selection (selection stays UID-based).
	winStart, winEnd int

	// counts for honest scope display: visible vs total snapshot size.
	visible int
	total   int

	// marks is the set of marked workflows, keyed by UID. It is keyed by
	// identity for the same reason the selection is: sorting, filtering and
	// a refresh reorder rows, and a mark must stay on its workflow. A mark
	// hidden by the filter is still a mark, because the bulk action it feeds
	// acts on every marked workflow, not only the visible ones.
	marks map[string]bool

	// theme is injected (shared.Theme) so goldens can force no-color.
	theme shared.Theme

	// allNS marks a snapshot collected across namespaces. The table gains a
	// NAMESPACE column and the filter matches it, because two rows of the
	// same name in two namespaces are otherwise indistinguishable.
	allNS bool
}

// New builds an empty list model. Items/status arrive via SetItems and
// SetStatus; the model never fetches by itself.
func New(theme shared.Theme) Model {
	return Model{
		phase: PhaseAll,
		sort:  SortPhaseName,
		theme: theme,
	}
}

// SetItems replaces the snapshot (root calls after listLoadedMsg). The
// selected workflow is preserved by UID identity across the replacement
// (reorder/deletion-safe). Sorting/filtering is reapplied.
func (m *Model) SetItems(items []core.Summary, now time.Time) {
	m.items = items
	m.now = now
	m.total = len(items)
	m.reselect(now)
	m.pruneMarks()
	m.applyView()
}

// ToggleMark marks the selected workflow, or unmarks it when it is already
// marked. With no selection it does nothing.
func (m *Model) ToggleMark() {
	uid := m.SelectedRef().UID
	if uid == "" {
		return
	}
	if m.marks[uid] {
		delete(m.marks, uid)
		return
	}
	if m.marks == nil {
		m.marks = map[string]bool{}
	}
	m.marks[uid] = true
}

// Marked returns the marked workflows in the current sort order, hidden
// ones included. The order is the order a bulk action runs in, so it
// follows what the reader sees rather than the order they marked in.
func (m *Model) Marked() []core.Summary {
	if len(m.marks) == 0 {
		return nil
	}
	out := make([]core.Summary, 0, len(m.marks))
	for _, it := range m.items {
		if m.marks[it.Ref.UID] {
			out = append(out, it)
		}
	}
	return Sort(out, m.sort)
}

// MarkCount is the number of marked workflows, hidden ones included.
func (m *Model) MarkCount() int { return len(m.marks) }

// HiddenMarkCount is the number of marked workflows the current filter
// hides. A bulk action reaches them too, so the toolbar has to say so.
func (m *Model) HiddenMarkCount() int {
	if len(m.marks) == 0 {
		return 0
	}
	shown := 0
	for _, r := range m.rows {
		if m.marks[r.Ref.UID] {
			shown++
		}
	}
	return len(m.marks) - shown
}

// ClearMarks drops every mark.
func (m *Model) ClearMarks() { m.marks = nil }

// pruneMarks drops the marks whose workflow left the snapshot. A mark on a
// workflow that no longer exists would be counted in the toolbar and sent
// to a bulk action with no row to show it on.
func (m *Model) pruneMarks() {
	if len(m.marks) == 0 {
		return
	}
	present := make(map[string]bool, len(m.items))
	for _, it := range m.items {
		present[it.Ref.UID] = true
	}
	for uid := range m.marks {
		if !present[uid] {
			delete(m.marks, uid)
		}
	}
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

// SetStatus updates the list-level status with sanitized message text and
// (for stale) the age of the last good data.
func (m *Model) SetStatus(s Status, msg string, staleAge time.Duration) {
	m.status = s
	m.errMsg = sanitizeOne(msg)
	m.errAge = staleAge
}

// SetAllNamespaces says whether the snapshot spans namespaces. The root sets
// it with every change of scope, before the new snapshot arrives.
func (m *Model) SetAllNamespaces(on bool) {
	m.allNS = on
	m.applyView()
}

// CyclePhase advances the phase bucket and selects its first row.
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

// SetQuery parses and applies the local filter (snapshot-scoped).
// A query that does not parse changes nothing: the previous
// filter stays applied, and the error is returned and kept for the toolbar.
func (m *Model) SetQuery(q string) error {
	match, err := ParseQuery(q)
	if err != nil {
		m.queryErr = err.Error()
		return err
	}
	m.query, m.match, m.queryErr = q, match, ""
	m.applyView()
	return nil
}

// Query returns the applied filter text.
func (m *Model) Query() string { return m.query }

// ToggleWide switches the metadata columns on or off.
func (m *Model) ToggleWide() { m.wide = !m.wide }

// applyView recomputes the display rows from items: filter by phase bucket
// and the parsed query, then sort. Selection survives by UID (never
// reindexed by position).
//
// Every filter goes through here. HiddenMarkCount compares the marks with
// m.rows, so a filter applied anywhere else would miscount hidden marks.
func (m *Model) applyView() {
	rows := make([]core.Summary, 0, len(m.items))
	match := m.match.AcrossNamespaces(m.allNS)
	for _, it := range m.items {
		if !m.phase.Matches(it) {
			continue
		}
		if !match.Match(it, m.now) {
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
// clears it if the workflow disappeared (deletion between polls).
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
// The ROOT converts it to a network effect.
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
// Container defaults to "main" visibly (the UI for editing the
// container lives in the logs view).
func (m *Model) LogsIntent() tea.Msg {
	sel := m.Selected()
	if sel.Ref.UID == "" {
		return nil
	}
	return shared.OpenLogsMsg{Ref: sel.Ref, Container: "main"}
}

// RefreshListMsg asks the root to refresh the list.
type RefreshListMsg struct{}

func (m *Model) RefreshIntent() tea.Msg { return RefreshListMsg{} }

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
	m.histPos = len(m.history)
	m.picking, m.pickSel = true, 0
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
	m.histPos = len(m.history)
	m.picking, m.pickSel = true, 0
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

// SetSize records the terminal size (child model contract).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
}

// SetTheme replaces the style set the list is drawn in.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// sanitizeOne is the single render-path sanitization choke point for
// server-derived strings in this component.
func sanitizeOne(s string) string { return shared.Sanitize(s) }
