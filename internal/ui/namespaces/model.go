// Package namespaces is the namespace picker: the dialog behind the `n` key.
//
// The picker is a dialog: while it is open it owns every
// key except Ctrl-C. It holds no client and starts nothing. The root loads the
// names into it and converts its one intent into a reconnection.
package namespaces

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// SwitchMsg is the picker's only output: the namespace the reader chose.
type SwitchMsg struct{ Namespace string }

// Model is the picker state.
type Model struct {
	open bool
	// current is the namespace in use, marked in the list so the reader can
	// see where they are before they move.
	current string
	// names is the candidate list, already sorted and de-duplicated.
	names []string
	// rows is names narrowed by the typed filter; the cursor indexes it.
	rows   []string
	cursor int
	// filter is what the reader has typed. It doubles as the namespace to
	// use when it matches nothing: a namespace with no workflows cannot be
	// derived from a list of workflows, and it must still be reachable.
	filter string
	// loading marks a running fetch; note and lastErr explain where the
	// names came from, or why there are none.
	loading bool
	note    string
	lastErr string

	theme         shared.Theme
	width, height int
}

// New builds a closed picker.
func New(theme shared.Theme) *Model { return &Model{theme: theme} }

// IsOpen reports whether the dialog owns the keyboard.
func (m *Model) IsOpen() bool { return m.open }

// Open shows the picker for the namespace currently in use and clears the
// previous filter, so it never reopens narrowed by a search the reader has
// forgotten about.
func (m *Model) Open(current string) {
	m.open = true
	m.current = current
	m.filter = ""
	m.lastErr = ""
	m.applyFilter()
	m.selectCurrent()
}

// Close hides the picker.
func (m *Model) Close() { m.open = false }

// SetLoading marks a running fetch.
func (m *Model) SetLoading() { m.loading = true; m.lastErr = "" }

// SetNames records the fetched candidates and the note that says where they
// came from. seed names (from the profile) are merged in: a configured
// namespace must be offered even when the server's answer does not mention it.
func (m *Model) SetNames(names []string, note string, seed []string) {
	merged := map[string]bool{}
	for _, n := range append(append([]string(nil), names...), seed...) {
		if n = strings.TrimSpace(n); n != "" {
			merged[n] = true
		}
	}
	if m.current != "" {
		merged[m.current] = true
	}
	out := make([]string, 0, len(merged))
	for n := range merged {
		out = append(out, n)
	}
	sort.Strings(out)
	m.names = out
	m.note = note
	m.loading = false
	m.applyFilter()
	m.selectCurrent()
}

// SetError records why the list could not be fetched. It is not fatal: the
// picker still accepts a typed namespace, which is the whole reason the typed
// entry exists.
func (m *Model) SetError(msg string) {
	m.loading = false
	m.lastErr = shared.Sanitize(msg)
}

// SetSize records the dialog box size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetTheme replaces the style set the dialog is drawn in.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// Selected returns the namespace the picker would switch to right now: the
// row under the cursor, or the typed text when it matches no row.
func (m *Model) Selected() string {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor]
	}
	return strings.TrimSpace(m.filter)
}

// Update implements the dialog key matrix. Every printable character narrows
// the list, so there is no separate "search mode" to enter and leave.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.open {
		return nil
	}
	switch s := key.String(); s {
	case "esc":
		m.Close()
		return nil
	case "enter":
		ns := m.Selected()
		if ns == "" || ns == m.current {
			m.Close()
			return nil
		}
		m.Close()
		return func() tea.Msg { return SwitchMsg{Namespace: ns} }
	case "up", "ctrl+p":
		m.move(-1)
		return nil
	case "down", "ctrl+n":
		m.move(1)
		return nil
	case "backspace":
		r := []rune(m.filter)
		if len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.applyFilter()
		}
		return nil
	default:
		if runes := []rune(s); len(runes) == 1 && runes[0] >= 0x20 {
			m.filter += s
			m.applyFilter()
		}
		return nil
	}
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= len(m.rows) {
		m.cursor = 0
	}
}

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	rows := make([]string, 0, len(m.names))
	for _, n := range m.names {
		if q == "" || strings.Contains(strings.ToLower(n), q) {
			rows = append(rows, n)
		}
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = 0
	}
}

func (m *Model) selectCurrent() {
	for i, n := range m.rows {
		if n == m.current {
			m.cursor = i
			return
		}
	}
	m.cursor = 0
}

// Hints is the dialog footer.
func (m *Model) Hints() string {
	return "type to filter  ↑↓ move  enter switch  esc cancel"
}

// BodyLines renders the dialog. Every state says what it is: an empty list
// after a denied request must not look like a cluster with no namespaces.
func (m *Model) BodyLines() []string {
	lines := []string{m.theme.Title.Render("switch namespace") + m.theme.Muted.Render(" — current: ") + shared.Sanitize(m.current)}
	typed := strings.TrimSpace(m.filter)
	lines = append(lines, "filter: "+shared.Sanitize(m.filter)+"_")
	switch {
	case m.loading:
		lines = append(lines, "", "asking the server…")
	case m.lastErr != "":
		lines = append(lines, "", m.theme.Warning.Render("the server would not list namespaces: "+m.lastErr),
			"type the namespace and press enter")
	case len(m.names) == 0:
		lines = append(lines, "", "no namespace names available — type one and press enter")
	}
	if len(m.rows) == 0 && typed != "" {
		lines = append(lines, "", "no match — enter uses "+shared.Sanitize(typed)+" as typed")
	}
	for i, n := range m.rows {
		mark := "  "
		if n == m.current {
			mark = "* "
		}
		row := mark + shared.Sanitize(n)
		switch {
		case i == m.cursor:
			row = m.theme.SelectRow(row, m.width)
		case n == m.current:
			row = m.theme.Accent.Render(row)
		}
		lines = append(lines, row)
	}
	if m.note != "" && !m.loading {
		lines = append(lines, "", m.theme.Dim.Render(shared.Sanitize(m.note)))
	}
	if m.height > 0 {
		lines = shared.ClampLines(lines, m.height)
	}
	return lines
}
