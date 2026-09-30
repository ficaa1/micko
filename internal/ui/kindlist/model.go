// Package kindlist is the list pane of the resource kinds beside workflows:
// cron workflows and templates. One kind is one Spec: its columns, its sort
// orders, its info panel and where enter leads. The list behaviour itself —
// movement, the live filter, the sort cycle, the info panel, the raw
// manifest — lives here once, so every kind answers the same keys the same
// way the workflow list does.
//
// Like the workflow list it is a child model over injected values: it never
// fetches, never starts goroutines, and returns intents (DrillMsg,
// RefreshMsg) for the root to turn into requests. Every server string is
// sanitized before it is drawn, and parameter values follow the profile's
// redactValues setting, which v flips for the selected row.
package kindlist

import (
	"cmp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/ui/detail"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Status is the list-level state, mirrored from the root's collection state.
// Each one renders distinctly, so an empty list never passes for a refusal.
type Status int

const (
	// StatusIdle: the items are the last successful answer.
	StatusIdle Status = iota
	// StatusLoading: a collection is running and nothing has landed yet.
	StatusLoading
	// StatusStale: the last collection failed; the items are older.
	StatusStale
	// StatusForbidden: the server refused the list.
	StatusForbidden
	// StatusUnauthenticated: the server does not accept the credential.
	StatusUnauthenticated
	// StatusUnsupported: this server has no such list at all.
	StatusUnsupported
)

// Column is one table column. Width 0 on the last column means "the rest of
// the row"; that column is dropped when the rest is too narrow to read.
type Column struct {
	ID    string
	Title string
	Width int
	Right bool
}

// SortOrder is one entry of the `s` cycle. Compare returns a negative number
// when a sorts first; ties fall back to namespace, then name.
type SortOrder[T any] struct {
	Label   string
	Compare func(a, b T, now time.Time) int
}

// Field is one line of the info panel: a label and its value. A field with
// an empty label continues the value above it. Warn draws the value in the
// warning style, for facts the reader should not miss.
type Field struct {
	Label string
	Value string
	Warn  bool
}

// DrillMsg asks the root for the workflows a row owns: the workflow list,
// narrowed by a label selector on the server. Title names the owner for the
// pane title.
type DrillMsg struct {
	Namespace string
	Selector  string
	Title     string
}

// RefreshMsg asks the root to collect the kind's list again now (r).
type RefreshMsg struct{}

// Spec describes one kind to the list.
type Spec[T any] struct {
	// Noun is the plural the pane uses in its sentences ("cron workflows").
	Noun string
	// Title is the pane's border title ("Cron workflows").
	Title string
	// Namespaced is false for a cluster-scoped kind: no NAMESPACE column,
	// and the namespace keys do not apply.
	Namespaced bool
	// Key is the row's namespace and name. It is the row's identity too,
	// unless ID is set.
	Key func(T) (namespace, name string)
	// ID is the row's identity, for a kind where two rows can share a
	// namespace and name (archived runs of a reused name). Selection and
	// reveal follow it across refreshes.
	ID func(T) string
	// Columns lays the table out for a table width.
	Columns func(width int) []Column
	// Cell is the text of one cell. The list sanitizes it.
	Cell func(item T, column string, now time.Time) string
	// RowStyle colors an unselected row. Nil draws rows plain.
	RowStyle func(item T, th shared.Theme) lipgloss.Style
	// Sorts is the `s` cycle; the first is the default.
	Sorts []SortOrder[T]
	// Info is the info panel's content for the selected row. reveal is the
	// reader's v toggle: parameter values are shown only when it is on.
	Info func(item T, reveal bool, now time.Time) []Field
	// Manifest is the row's object as the server sent it, for the raw view.
	Manifest func(T) []byte
	// Drill is where enter leads for a kind that owns workflows: the
	// workflow list narrowed to them.
	Drill func(T) DrillMsg
	// Open is where enter leads for a kind whose rows are themselves
	// something to open, used when Drill is nil. With neither, enter does
	// nothing.
	Open func(T) tea.Msg
	// EmptyNote is added to the empty-namespace line, for a kind whose
	// empty answer can mean more than one thing.
	EmptyNote string
}

// Model is the list of one kind.
type Model[T any] struct {
	spec  Spec[T]
	theme shared.Theme

	items []T
	rows  []T
	// selKey is the selected row's identity, never its index, so a refresh
	// that reorders the rows keeps the cursor on the same object.
	selKey string

	query       string
	searchOn    bool
	searchBuf   string
	queryBefore string
	gPending    bool

	sortIdx int

	status Status
	errMsg string
	errAge time.Duration
	// note is a standing remark about the snapshot, shown in the toolbar:
	// that it holds only the newest rows, for example.
	note string

	allNS bool
	info  bool
	// redact is the profile's redactValues setting: when set, values start
	// hidden and v reveals them; otherwise they start shown and v hides them.
	redact bool
	// flipKey is the row on which the reader pressed v, flipping that row
	// from the default. The flip belongs to that one object: moving the
	// cursor returns to the default, so with redaction on, scrolling never
	// shows another row's secrets unasked.
	flipKey string

	// now is the clock reading of the last snapshot. Sort orders that
	// depend on the time (next run soonest) read it, so the order is the
	// one the reader's clock gives and does not move between refreshes.
	now time.Time

	width, height    int
	scrollTop        int
	winStart, winEnd int
}

// New builds an empty list for spec.
func New[T any](spec Spec[T], theme shared.Theme) *Model[T] {
	return &Model[T]{spec: spec, theme: theme, status: StatusLoading}
}

// SetTheme restyles the list.
func (m *Model[T]) SetTheme(th shared.Theme) { m.theme = th }

// SetItems replaces the snapshot, read at now. The selection stays on the
// same object when it is still there.
func (m *Model[T]) SetItems(items []T, now time.Time) {
	m.items = items
	m.now = now
	m.applyView()
}

// Reset drops the snapshot and the cursor, for a change of scope: rows from
// the old scope under the new header would be wrong until the first answer.
func (m *Model[T]) Reset() {
	m.items, m.rows = nil, nil
	m.selKey, m.flipKey = "", ""
	m.scrollTop = 0
	m.status = StatusLoading
	m.errMsg, m.errAge = "", 0
	m.note = ""
}

// SetStatus records the collection state and its sanitized reason.
func (m *Model[T]) SetStatus(s Status, msg string, age time.Duration) {
	m.status = s
	m.errMsg = shared.Sanitize(msg)
	m.errAge = age
}

// SetNote sets the toolbar's standing remark about the snapshot; empty
// clears it.
func (m *Model[T]) SetNote(note string) { m.note = note }

// SetAllNamespaces says whether the snapshot spans namespaces.
func (m *Model[T]) SetAllNamespaces(on bool) {
	m.allNS = on && m.spec.Namespaced
	m.applyView()
}

// Namespaced reports whether the kind lives in namespaces.
func (m *Model[T]) Namespaced() bool { return m.spec.Namespaced }

// SetSize records the pane size.
func (m *Model[T]) SetSize(w, h int) { m.width, m.height = w, h }

// Searching reports whether the filter input owns printable keys.
func (m *Model[T]) Searching() bool { return m.searchOn }

// revealed reports whether the selected row's values are shown.
func (m *Model[T]) revealed() bool {
	if m.selKey == "" {
		return false
	}
	return (m.flipKey == m.selKey) == m.redact
}

// SetRedact applies the profile's redactValues setting and drops any flip,
// so a profile that redacts never inherits a row the previous one showed.
func (m *Model[T]) SetRedact(redact bool) {
	m.redact = redact
	m.flipKey = ""
}

// sortLabel names the active sort.
func (m *Model[T]) sortLabel() string {
	if len(m.spec.Sorts) == 0 {
		return "name"
	}
	return m.spec.Sorts[m.sortIdx].Label
}

// Len is the size of the whole snapshot, filtered or not.
func (m *Model[T]) Len() int { return len(m.items) }

// Selected is the row under the cursor.
func (m *Model[T]) Selected() (T, bool) {
	for _, r := range m.rows {
		if m.key(r) == m.selKey {
			return r, true
		}
	}
	var zero T
	return zero, false
}

// SelectedName is the cursor row's namespace and name, empty when none.
func (m *Model[T]) SelectedName() (string, string) {
	if r, ok := m.Selected(); ok {
		return m.spec.Key(r)
	}
	return "", ""
}

func (m *Model[T]) key(item T) string {
	if m.spec.ID != nil {
		return m.spec.ID(item)
	}
	ns, name := m.spec.Key(item)
	return ns + "/" + name
}

// filterText is what `/` matches: the name, or namespace/name across
// namespaces, where two rows can share a name.
func (m *Model[T]) filterText(item T) string {
	ns, name := m.spec.Key(item)
	if m.allNS {
		return strings.ToLower(ns + "/" + name)
	}
	return strings.ToLower(name)
}

// applyView recomputes the rows: filter, then sort, then re-anchor the
// cursor on its object or the first row.
func (m *Model[T]) applyView() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	rows := make([]T, 0, len(m.items))
	for _, it := range m.items {
		if q != "" && !strings.Contains(m.filterText(it), q) {
			continue
		}
		rows = append(rows, it)
	}
	now := m.now
	var order func(a, b T, now time.Time) int
	if len(m.spec.Sorts) > 0 {
		order = m.spec.Sorts[m.sortIdx].Compare
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if order != nil {
			if c := order(rows[i], rows[j], now); c != 0 {
				return c < 0
			}
		}
		ai, an := m.spec.Key(rows[i])
		bi, bn := m.spec.Key(rows[j])
		if c := cmp.Compare(strings.ToLower(an), strings.ToLower(bn)); c != 0 {
			return c < 0
		}
		return ai < bi
	})
	m.rows = rows
	found := false
	for _, r := range m.rows {
		if m.key(r) == m.selKey {
			found = true
			break
		}
	}
	if !found {
		next := ""
		if len(m.rows) > 0 {
			next = m.key(m.rows[0])
		}
		m.setSel(next)
	}
}

func (m *Model[T]) selIndex() int {
	for i, r := range m.rows {
		if m.key(r) == m.selKey {
			return i
		}
	}
	return -1
}

func (m *Model[T]) moveTo(i int) {
	if len(m.rows) == 0 {
		m.setSel("")
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	m.setSel(m.key(m.rows[i]))
}

// setSel moves the cursor to key. A flip belongs to the row it was made
// on, so moving anywhere else ends it.
func (m *Model[T]) setSel(key string) {
	if key != m.selKey {
		m.flipKey = ""
	}
	m.selKey = key
}

func (m *Model[T]) move(delta int) {
	i := m.selIndex()
	if i < 0 {
		m.moveTo(0)
		return
	}
	m.moveTo(i + delta)
}

// Update handles one key. Keys the list does not own return nil so the root
// can act on them.
func (m *Model[T]) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	case tea.WindowSizeMsg:
		return nil
	}
	return nil
}

func (m *Model[T]) handleKey(key string) tea.Cmd {
	if m.searchOn {
		m.searchKey(key)
		return nil
	}
	if m.gPending {
		m.gPending = false
		if key == "g" {
			m.moveTo(0)
			return nil
		}
	} else if key == "g" {
		m.gPending = true
		return nil
	}
	switch key {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown", "ctrl+d":
		m.move(m.pageStep())
	case "pgup", "ctrl+u":
		m.move(-m.pageStep())
	case "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.rows) - 1)
	case "enter":
		sel, ok := m.Selected()
		switch {
		case !ok:
		case m.spec.Drill != nil:
			intent := m.spec.Drill(sel)
			return func() tea.Msg { return intent }
		case m.spec.Open != nil:
			intent := m.spec.Open(sel)
			return func() tea.Msg { return intent }
		}
	case "/":
		m.searchOn = true
		m.queryBefore = m.query
		m.searchBuf = m.query
	case "s":
		if len(m.spec.Sorts) > 1 {
			m.sortIdx = (m.sortIdx + 1) % len(m.spec.Sorts)
			m.selKey = ""
			m.scrollTop = 0
			m.applyView()
		}
	case "i":
		m.info = !m.info
	case "v":
		if m.flipKey == m.selKey {
			m.flipKey = ""
		} else {
			m.flipKey = m.selKey
		}
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "esc":
		// The panel closes first, then the filter clears. With neither
		// there is nothing to back out of on a list, so esc stays inert.
		switch {
		case m.info:
			m.info = false
		case m.query != "":
			m.query = ""
			m.applyView()
		}
	}
	return nil
}

// searchKey edits the filter. It filters as the reader types; enter keeps
// the filter, esc restores the one there was before.
func (m *Model[T]) searchKey(key string) {
	switch key {
	case "esc":
		m.searchOn = false
		m.searchBuf = ""
		m.query = m.queryBefore
		m.applyView()
		return
	case "enter":
		m.searchOn = false
		m.searchBuf = ""
		return
	case "backspace":
		if r := []rune(m.searchBuf); len(r) > 0 {
			m.searchBuf = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.searchBuf = ""
	default:
		r := []rune(key)
		if len(r) != 1 || r[0] < 0x20 || r[0] == 0x7f {
			return
		}
		m.searchBuf += key
	}
	if q := strings.TrimSpace(m.searchBuf); q != m.query {
		m.query = q
		m.selKey = ""
		m.scrollTop = 0
		m.applyView()
	}
}

func (m *Model[T]) pageStep() int {
	if n := m.winEnd - m.winStart; n > 1 {
		return n - 1
	}
	return 1
}

// RawLines is the selected row's manifest for the raw view, rendered and
// redacted the way the workflow resource tab renders it. v reveals it.
func (m *Model[T]) RawLines() []string {
	sel, ok := m.Selected()
	if !ok || m.spec.Manifest == nil {
		return []string{"(no " + m.spec.Noun + " selected)"}
	}
	out := detail.RenderManifest(m.spec.Manifest(sel), m.revealed())
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// PaneTitle is the border title.
func (m *Model[T]) PaneTitle() string { return m.spec.Title }

// Hints are the list's keys for the footer.
func (m *Model[T]) Hints() string {
	h := "i info  / search  s sort"
	switch {
	case m.spec.Drill != nil:
		h = "enter workflows  " + h
	case m.spec.Open != nil:
		h = "enter open  " + h
	}
	if m.info {
		if m.revealed() {
			h += "  v redact"
		} else {
			h += "  v reveal"
		}
	}
	if m.spec.Namespaced {
		h += "  n namespace  0 all ns"
	}
	return h + "  r refresh  f manifest"
}

// WindowStatus is the visible row range, for the footer.
func (m *Model[T]) WindowStatus() string {
	if m.winEnd-m.winStart <= 0 || m.winEnd-m.winStart >= len(m.rows) {
		return strconv.Itoa(len(m.rows)) + " shown"
	}
	return strconv.Itoa(m.winStart+1) + "-" + strconv.Itoa(m.winEnd) + "/" + strconv.Itoa(len(m.rows))
}
