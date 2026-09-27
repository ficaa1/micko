package kindlist

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// thing is a toy kind: enough fields to exercise every Spec hook.
type thing struct {
	ns, name string
	size     int
	secret   string
}

var epoch = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func thingSpec(namespaced bool) Spec[thing] {
	return Spec[thing]{
		Noun:       "things",
		Title:      "Things",
		Namespaced: namespaced,
		Key:        func(t thing) (string, string) { return t.ns, t.name },
		Columns: func(w int) []Column {
			return []Column{
				{ID: "name", Title: "NAME", Width: 20},
				{ID: "size", Title: "SIZE", Width: 6, Right: true},
				{ID: "note", Title: "NOTE", Width: 0},
			}
		},
		Cell: func(t thing, col string, _ time.Time) string {
			switch col {
			case "name":
				return t.name
			case "size":
				return itoa(t.size)
			}
			return "note for " + t.name
		},
		Sorts: []SortOrder[thing]{
			{Label: "biggest", Compare: func(a, b thing, _ time.Time) int { return b.size - a.size }},
			{Label: "name", Compare: func(a, b thing, _ time.Time) int { return strings.Compare(a.name, b.name) }},
		},
		Info: func(t thing, reveal bool, _ time.Time) []Field {
			v := "[REDACTED]"
			if reveal {
				v = t.secret
			}
			return []Field{{Label: "Name", Value: t.name}, {Label: "Secret", Value: v}}
		},
		Manifest: func(t thing) []byte {
			return []byte(`{"metadata":{"name":"` + t.name + `"},"spec":{"arguments":{"parameters":[{"name":"p","value":"` + t.secret + `"}]}}}`)
		},
		Drill: func(t thing) DrillMsg {
			return DrillMsg{Namespace: t.ns, Selector: "owner=" + t.name, Title: "thing " + t.name}
		},
	}
}

func things() []thing {
	return []thing{
		{"a", "small", 1, "s1"},
		{"a", "large", 30, "s2"},
		{"b", "medium", 10, "s3"},
	}
}

func newThings(t *testing.T) *Model[thing] {
	t.Helper()
	m := New(thingSpec(true), shared.NewTheme(true))
	m.SetSize(100, 20)
	m.SetItems(things(), epoch)
	m.SetStatus(StatusIdle, "", 0)
	return m
}

func key(m *Model[thing], k string) tea.Cmd {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		r := []rune(k)[0]
		msg = tea.KeyPressMsg{Code: r, Text: k}
	}
	return m.Update(msg)
}

func body(m *Model[thing]) string {
	return ansi.Strip(strings.Join(m.BodyLines(epoch), "\n"))
}

func selected(m *Model[thing]) string {
	_, name := m.SelectedName()
	return name
}

// The first sort is the default, s cycles, and the cursor follows its
// object across a refresh that reorders the rows.
func TestSortCycleAndSelectionByIdentity(t *testing.T) {
	m := newThings(t)
	if got := selected(m); got != "large" {
		t.Fatalf("default sort put %q first, want large", got)
	}
	key(m, "j")
	if got := selected(m); got != "medium" {
		t.Fatalf("j moved to %q", got)
	}
	// A refresh with medium grown to the top keeps the cursor on medium.
	items := things()
	items[2].size = 99
	m.SetItems(items, epoch)
	if got := selected(m); got != "medium" {
		t.Fatalf("after reorder the cursor is on %q, want medium", got)
	}
	key(m, "s")
	if m.SortLabel() != "name" || selected(m) != "large" {
		t.Fatalf("s: sort %q, cursor %q; want name order with the cursor at the top", m.SortLabel(), selected(m))
	}
	key(m, "s")
	if m.SortLabel() != "biggest" {
		t.Fatalf("s did not wrap: %q", m.SortLabel())
	}
}

// Movement covers j/k, G, gg and the bounds.
func TestMovement(t *testing.T) {
	m := newThings(t)
	key(m, "G")
	if selected(m) != "small" {
		t.Fatalf("G -> %q", selected(m))
	}
	key(m, "j")
	if selected(m) != "small" {
		t.Fatalf("j past the end -> %q", selected(m))
	}
	key(m, "g")
	key(m, "g")
	if selected(m) != "large" {
		t.Fatalf("gg -> %q", selected(m))
	}
	key(m, "k")
	if selected(m) != "large" {
		t.Fatalf("k past the top -> %q", selected(m))
	}
}

// / filters as the reader types, enter keeps the filter, esc on the list
// clears it, and esc inside the input restores the previous one.
func TestFilter(t *testing.T) {
	m := newThings(t)
	key(m, "/")
	if !m.Searching() {
		t.Fatal("/ did not open the input")
	}
	for _, r := range "med" {
		key(m, string(r))
	}
	if n := len(m.Rows()); n != 1 || selected(m) != "medium" {
		t.Fatalf("filter med: %d rows, cursor %q", n, selected(m))
	}
	key(m, "enter")
	if m.Searching() || m.Query() != "med" {
		t.Fatalf("enter: searching=%v query=%q", m.Searching(), m.Query())
	}
	if !strings.Contains(body(m), "[within 3 collected]") {
		t.Fatalf("a filtered list does not say it is a subset:\n%s", body(m))
	}
	key(m, "/")
	key(m, "backspace")
	key(m, "esc")
	if m.Query() != "med" {
		t.Fatalf("esc in the input lost the previous filter: %q", m.Query())
	}
	key(m, "esc")
	if m.Query() != "" || len(m.Rows()) != 3 {
		t.Fatalf("esc on the list left filter %q", m.Query())
	}
	// q typed into the input is a letter.
	key(m, "/")
	key(m, "q")
	if m.SearchValue() != "q" {
		t.Fatalf("input holds %q", m.SearchValue())
	}
}

// Across namespaces the filter matches namespace/name and the table gains
// a NAMESPACE column; a cluster-scoped kind never shows one.
func TestAllNamespaces(t *testing.T) {
	m := newThings(t)
	m.SetAllNamespaces(true)
	if !strings.Contains(body(m), "NAMESPACE") {
		t.Fatal("no NAMESPACE column across namespaces")
	}
	m.SetStatus(StatusIdle, "", 0)
	key(m, "/")
	for _, r := range "b/" {
		key(m, string(r))
	}
	if n := len(m.Rows()); n != 1 {
		t.Fatalf("b/ matched %d rows", n)
	}

	c := New(thingSpec(false), shared.NewTheme(true))
	c.SetSize(100, 20)
	c.SetItems(things(), epoch)
	c.SetAllNamespaces(true)
	if strings.Contains(body(c), "NAMESPACE") {
		t.Fatal("a cluster-scoped kind drew a NAMESPACE column")
	}
	if strings.Contains(c.Hints(), "0 all ns") || strings.Contains(c.Hints(), "n namespace") {
		t.Fatalf("cluster-scoped hints offer the namespace keys: %s", c.Hints())
	}
}

// enter emits the drill intent for the selected row; r the refresh intent.
func TestIntents(t *testing.T) {
	m := newThings(t)
	cmd := key(m, "enter")
	if cmd == nil {
		t.Fatal("enter emitted nothing")
	}
	d, ok := cmd().(DrillMsg)
	if !ok || d.Selector != "owner=large" || d.Namespace != "a" || d.Title != "thing large" {
		t.Fatalf("drill = %#v", d)
	}
	if _, ok := key(m, "r")().(RefreshMsg); !ok {
		t.Fatal("r did not ask for a refresh")
	}
	empty := New(thingSpec(true), shared.NewTheme(true))
	if cmd := key(empty, "enter"); cmd != nil {
		t.Fatal("enter on an empty list emitted an intent")
	}
}

// i opens the panel below the table under 140 cells and beside it from 140;
// esc closes the panel before it clears a filter.
func TestInfoPanelPlacement(t *testing.T) {
	m := newThings(t)
	key(m, "i")
	if !m.InfoOpen() {
		t.Fatal("i did not open the panel")
	}
	lines := strings.Split(body(m), "\n")
	below := false
	for _, l := range lines {
		if strings.HasPrefix(l, "Name") && strings.Contains(l, "large") {
			below = true
		}
	}
	if !below {
		t.Fatalf("at 100 cells the panel is not below the table:\n%s", body(m))
	}

	m.SetSize(150, 20)
	side := false
	for _, l := range strings.Split(body(m), "\n") {
		if strings.Contains(l, "│ Name") && strings.Contains(l, "SIZE") {
			side = true
		}
	}
	if !side {
		t.Fatalf("at 150 cells the panel is not beside the table:\n%s", body(m))
	}
	for _, l := range strings.Split(body(m), "\n") {
		if w := ansi.StringWidth(l); w > 150 {
			t.Fatalf("line of %d cells in a 150-cell pane: %q", w, l)
		}
	}

	m.SetQuery("large")
	key(m, "esc")
	if m.InfoOpen() || m.Query() != "large" {
		t.Fatalf("esc: panel open=%v query=%q; want the panel closed first", m.InfoOpen(), m.Query())
	}
}

// With redaction on, v reveals the selected row's values in the panel and the
// manifest, and only that row's: moving the cursor redacts again.
func TestRevealBelongsToOneRow(t *testing.T) {
	m := newThings(t)
	m.SetRedact(true)
	key(m, "i")
	if strings.Contains(body(m), "s2") {
		t.Fatal("a value shown before v")
	}
	if raw := strings.Join(m.RawLines(), "\n"); strings.Contains(raw, "s2") {
		t.Fatalf("the manifest shows a value before v:\n%s", raw)
	}
	key(m, "v")
	if !strings.Contains(body(m), "s2") || !strings.Contains(body(m), "values shown") {
		t.Fatalf("v did not reveal:\n%s", body(m))
	}
	if raw := strings.Join(m.RawLines(), "\n"); !strings.Contains(raw, "s2") {
		t.Fatalf("v did not reveal the manifest:\n%s", raw)
	}
	key(m, "j")
	if m.Revealed() || strings.Contains(body(m), "s3") {
		t.Fatal("moving to another row kept values revealed")
	}
	key(m, "k")
	if m.Revealed() {
		t.Fatal("returning to the row re-revealed it without v")
	}
}

// A panel taller than its room flows into two columns on a wide pane, and is
// cut with a pointer to the manifest on a narrow one.
func TestTallPanel(t *testing.T) {
	spec := thingSpec(true)
	spec.Info = func(t thing, _ bool, _ time.Time) []Field {
		var f []Field
		for i := 0; i < 20; i++ {
			f = append(f, Field{Label: "F" + itoa(i), Value: "v" + itoa(i)})
		}
		return f
	}
	m := New(spec, shared.NewTheme(true))
	m.SetSize(120, 30)
	m.SetItems(things(), epoch)
	m.SetStatus(StatusIdle, "", 0)
	key(m, "i")
	out := body(m)
	if !strings.Contains(out, "F0") || !strings.Contains(out, "F19") {
		t.Fatalf("two columns did not hold every field:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n > 30 {
		t.Fatalf("%d lines in a 30-line pane", n)
	}
	m.SetSize(80, 30)
	out = body(m)
	if strings.Contains(out, "F19") || !strings.Contains(out, "more in the manifest: f") {
		t.Fatalf("a narrow tall panel was not cut with a pointer:\n%s", out)
	}
}

// Every state reads as itself on an empty table.
func TestEmptyStates(t *testing.T) {
	cases := []struct {
		status Status
		msg    string
		want   string
	}{
		{StatusLoading, "", "loading things…"},
		{StatusForbidden, "cannot list", "no things visible: list forbidden"},
		{StatusUnauthenticated, "bad token", "not authenticated"},
		{StatusUnsupported, "no such list", "no things on this server"},
		{StatusStale, "boom", "the list failed"},
		{StatusIdle, "", "no things in this namespace"},
	}
	for _, c := range cases {
		m := New(thingSpec(true), shared.NewTheme(true))
		m.SetSize(100, 20)
		m.SetItems(nil, epoch)
		m.SetStatus(c.status, c.msg, time.Minute)
		out := body(m)
		if !strings.Contains(out, c.want) {
			t.Errorf("status %d: want %q in\n%s", c.status, c.want, out)
		}
		if c.msg != "" && !strings.Contains(out, c.msg) {
			t.Errorf("status %d: the reason %q is not shown:\n%s", c.status, c.msg, out)
		}
	}
	m := newThings(t)
	m.SetQuery("zzz")
	if !strings.Contains(body(m), "no things match the current filter") {
		t.Fatal("filtered-empty state missing")
	}
	m.SetQuery("")
	m.SetAllNamespaces(true)
	m.SetItems(nil, epoch)
	if !strings.Contains(body(m), "in any namespace this token can read") {
		t.Fatal("all-namespaces empty state missing")
	}
}

// Server text is sanitized before it reaches the screen.
func TestCellsAreSanitized(t *testing.T) {
	m := New(thingSpec(true), shared.NewTheme(true))
	m.SetSize(100, 10)
	m.SetItems([]thing{{"a", "evil\x1b[31mred", 1, ""}}, epoch)
	if strings.Contains(strings.Join(m.BodyLines(epoch), ""), "\x1b[31m") {
		t.Fatal("an escape sequence from a name reached the screen")
	}
}

// The table keeps to the pane: the open last column takes the rest, or is
// dropped when the rest is too narrow; below 60 cells the pane asks for room.
func TestWidths(t *testing.T) {
	m := newThings(t)
	for _, w := range []int{60, 76, 100, 136} {
		m.SetSize(w, 20)
		for _, l := range m.BodyLines(epoch) {
			if n := ansi.StringWidth(ansi.Strip(l)); n > w {
				t.Fatalf("width %d: line of %d cells: %q", w, n, l)
			}
		}
	}
	m.SetSize(50, 10)
	if !strings.Contains(body(m), "Resize to at least 60 columns") {
		t.Fatalf("no resize notice at 50 cells:\n%s", body(m))
	}
}

// The window follows the cursor and the footer says which rows are shown.
func TestWindow(t *testing.T) {
	var many []thing
	for i := 0; i < 30; i++ {
		many = append(many, thing{"a", "t" + itoa(100+i), i, ""})
	}
	m := New(thingSpec(true), shared.NewTheme(true))
	m.SetSize(100, 8)
	m.SetItems(many, epoch)
	m.SetStatus(StatusIdle, "", 0)
	m.BodyLines(epoch)
	if got := m.WindowStatus(); got != "1-6/30" {
		t.Fatalf("window = %q", got)
	}
	key(m, "G")
	m.BodyLines(epoch)
	if got := m.WindowStatus(); got != "25-30/30" {
		t.Fatalf("window after G = %q", got)
	}
}

// Values are shown by default. v hides them on the selected row only, and
// moving the cursor shows them again.
func TestValuesShownByDefault(t *testing.T) {
	m := newThings(t)
	key(m, "i")
	if !m.Revealed() || !strings.Contains(body(m), "s2") {
		t.Fatalf("values hidden by default:\n%s", body(m))
	}
	key(m, "v")
	if m.Revealed() || strings.Contains(body(m), "s2") {
		t.Fatalf("v did not hide the row's values:\n%s", body(m))
	}
	key(m, "j")
	if !m.Revealed() {
		t.Fatal("the next row must show its values")
	}
}
