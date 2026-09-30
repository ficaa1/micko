package kindlist

import (
	"slices"
	"strconv"
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
				return strconv.Itoa(t.size)
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

// things sort bravo, charlie, alpha by size and alpha, bravo, charlie by name.
func things() []thing {
	return []thing{
		{"a", "alpha", 1, "s-alpha"},
		{"a", "bravo", 30, "s-bravo"},
		{"b", "charlie", 10, "s-charlie"},
	}
}

// newList is spec's list at w×h holding items, collected and idle.
func newList(spec Spec[thing], w, h int, items []thing) *Model[thing] {
	m := New(spec, shared.NewTheme(true))
	m.SetSize(w, h)
	m.SetItems(items, epoch)
	m.SetStatus(StatusIdle, "", 0)
	return m
}

func newThings() *Model[thing] { return newList(thingSpec(true), 100, 20, things()) }

var namedKeys = map[string]tea.KeyPressMsg{
	"enter":     {Code: tea.KeyEnter},
	"esc":       {Code: tea.KeyEscape},
	"backspace": {Code: tea.KeyBackspace},
	"pgdown":    {Code: tea.KeyPgDown},
	"pgup":      {Code: tea.KeyPgUp},
	"home":      {Code: tea.KeyHome},
	"end":       {Code: tea.KeyEnd},
	"ctrl+u":    {Code: 'u', Mod: tea.ModCtrl},
}

// keys sends each space-separated key, drawing the pane before each as the
// shell does, and returns the last command.
func keys(m *Model[thing], seq string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range strings.Fields(seq) {
		msg, ok := namedKeys[k]
		if !ok {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		m.BodyLines(epoch)
		cmd = m.Update(msg)
	}
	return cmd
}

func body(m *Model[thing]) string {
	return ansi.Strip(strings.Join(m.BodyLines(epoch), "\n"))
}

func toolbar(m *Model[thing]) string {
	return strings.SplitN(body(m), "\n", 2)[0]
}

func selected(m *Model[thing]) string {
	_, name := m.SelectedName()
	return name
}

// shown is the NAME column of the rendered rows, top to bottom.
func shown(m *Model[thing]) []string {
	col := -1
	var out []string
	for _, l := range strings.Split(body(m), "\n") {
		f := strings.Fields(l)
		if col < 0 {
			col = slices.Index(f, "NAME")
			continue
		}
		if strings.Contains(l, "note for ") {
			out = append(out, f[col])
		}
	}
	return out
}

// The first sort is the default and s cycles through the rest and wraps,
// putting the cursor back on the first row.
func TestSortCycle(t *testing.T) {
	cases := []struct {
		keys     string
		wantRows []string
		wantSort string
		wantSel  string
	}{
		{"", []string{"bravo", "charlie", "alpha"}, "Sort: biggest", "bravo"},
		{"j s", []string{"alpha", "bravo", "charlie"}, "Sort: name", "alpha"},
		{"s s", []string{"bravo", "charlie", "alpha"}, "Sort: biggest", "bravo"},
	}
	for _, c := range cases {
		t.Run(c.keys, func(t *testing.T) {
			m := newThings()
			keys(m, c.keys)
			if got := shown(m); !slices.Equal(got, c.wantRows) {
				t.Errorf("rows = %v, want %v", got, c.wantRows)
			}
			if !strings.Contains(toolbar(m), c.wantSort) {
				t.Errorf("toolbar = %q, want %q", toolbar(m), c.wantSort)
			}
			if got := selected(m); got != c.wantSel {
				t.Errorf("cursor on %q, want %q", got, c.wantSel)
			}
		})
	}
}

// The cursor moves with j/k, the page keys, G/end, gg/home, and stops at
// either end.
func TestMovement(t *testing.T) {
	cases := []struct {
		keys string
		want string
	}{
		{"j", "charlie"},
		{"j j j", "alpha"},
		{"k", "bravo"},
		{"G", "alpha"},
		{"end", "alpha"},
		{"G g g", "bravo"},
		{"G home", "bravo"},
		{"pgdown", "alpha"},
		{"G pgup", "bravo"},
	}
	for _, c := range cases {
		t.Run(c.keys, func(t *testing.T) {
			m := newThings()
			keys(m, c.keys)
			if got := selected(m); got != c.want {
				t.Errorf("cursor on %q, want %q", got, c.want)
			}
		})
	}
}

// The cursor follows its object across a refresh that reorders the rows, and
// a kind with ID tells two rows of one name apart.
func TestSelectionFollowsIdentity(t *testing.T) {
	m := newThings()
	keys(m, "j")
	items := things()
	items[2].size = 0
	m.SetItems(items, epoch)
	if got := shown(m); !slices.Equal(got, []string{"bravo", "alpha", "charlie"}) {
		t.Fatalf("rows after the refresh = %v", got)
	}
	if got := selected(m); got != "charlie" {
		t.Errorf("after the reorder the cursor is on %q, want charlie", got)
	}

	spec := thingSpec(true)
	spec.ID = func(t thing) string { return t.secret }
	m = newList(spec, 100, 20, []thing{{"a", "same", 2, "id-2"}, {"a", "same", 1, "id-1"}})
	keys(m, "j")
	m.SetItems([]thing{{"a", "same", 1, "id-1"}, {"a", "same", 2, "id-2"}}, epoch)
	if sel, _ := m.Selected(); sel.secret != "id-1" {
		t.Errorf("after the refresh the cursor is on %q, want id-1", sel.secret)
	}
}

// / filters as the reader types, enter keeps the filter, esc on the list
// clears it, and esc inside the input restores the one there was.
func TestFilter(t *testing.T) {
	cases := []struct {
		name        string
		keys        string
		wantRows    []string
		wantToolbar string
	}{
		{"typing", "/ c h", []string{"charlie"}, "Search: ch[_]  (enter keep, esc cancel) [within 3 collected]"},
		{"kept", "/ c h enter", []string{"charlie"}, "Search: ch [within 3 collected]"},
		{"cleared on the list", "/ c h enter esc", []string{"bravo", "charlie", "alpha"}, "Search: (none)"},
		{"restored in the input", "/ c h enter / backspace esc", []string{"charlie"}, "Search: ch [within 3 collected]"},
		{"input cleared", "/ c h ctrl+u", []string{"bravo", "charlie", "alpha"}, "Search: [_]"},
		{"q is a letter", "/ q", nil, "Search: q[_]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newThings()
			keys(m, c.keys)
			if got := shown(m); !slices.Equal(got, c.wantRows) {
				t.Errorf("rows = %v, want %v", got, c.wantRows)
			}
			if !strings.HasPrefix(toolbar(m), c.wantToolbar) {
				t.Errorf("toolbar = %q, want it to start %q", toolbar(m), c.wantToolbar)
			}
		})
	}
}

// Across namespaces a namespaced kind gains a NAMESPACE column and filters on
// namespace/name; a cluster-scoped kind shows neither nor the namespace keys.
func TestAllNamespaces(t *testing.T) {
	cases := []struct {
		name       string
		namespaced bool
		wantColumn bool
		wantRows   []string
		wantHint   bool
	}{
		{"namespaced", true, true, []string{"charlie"}, true},
		{"cluster-scoped", false, false, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newList(thingSpec(c.namespaced), 100, 20, things())
			m.SetAllNamespaces(true)
			if got := strings.Contains(body(m), "NAMESPACE"); got != c.wantColumn {
				t.Errorf("NAMESPACE column shown: %v, want %v", got, c.wantColumn)
			}
			if got := strings.Contains(m.Hints(), "n namespace  0 all ns"); got != c.wantHint {
				t.Errorf("hints = %q, want the namespace keys: %v", m.Hints(), c.wantHint)
			}
			keys(m, "/ b /")
			if got := shown(m); !slices.Equal(got, c.wantRows) {
				t.Errorf("b/ matched %v, want %v", got, c.wantRows)
			}
		})
	}
}

// enter emits the kind's intent for the selected row and the hints name it;
// r asks for a refresh.
func TestIntents(t *testing.T) {
	open := thingSpec(true)
	open.Drill, open.Open = nil, func(t thing) tea.Msg { return "open " + t.name }
	inert := thingSpec(true)
	inert.Drill = nil
	cases := []struct {
		name      string
		spec      Spec[thing]
		items     []thing
		want      tea.Msg
		wantHints string
	}{
		{"drill", thingSpec(true), things(), DrillMsg{Namespace: "a", Selector: "owner=bravo", Title: "thing bravo"}, "enter workflows  i info"},
		{"open", open, things(), "open bravo", "enter open  i info"},
		{"neither", inert, things(), nil, "i info"},
		{"empty list", thingSpec(true), nil, nil, "enter workflows  i info"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newList(c.spec, 100, 20, c.items)
			var got tea.Msg
			if cmd := keys(m, "enter"); cmd != nil {
				got = cmd()
			}
			if got != c.want {
				t.Errorf("enter = %#v, want %#v", got, c.want)
			}
			if !strings.HasPrefix(m.Hints(), c.wantHints) {
				t.Errorf("hints = %q, want them to start %q", m.Hints(), c.wantHints)
			}
		})
	}
	if _, ok := keys(newThings(), "r")().(RefreshMsg); !ok {
		t.Error("r did not ask for a refresh")
	}
}

// i opens the panel below the table under 140 cells and beside it from 140;
// esc closes the panel before it clears a filter.
func TestInfoPanel(t *testing.T) {
	cases := []struct {
		width     int
		wantBelow bool
	}{
		{100, true},
		{150, false},
	}
	for _, c := range cases {
		t.Run(strconv.Itoa(c.width), func(t *testing.T) {
			m := newList(thingSpec(true), c.width, 20, things())
			keys(m, "i")
			below, beside := false, false
			for _, l := range strings.Split(body(m), "\n") {
				below = below || strings.HasPrefix(l, "Name") && strings.Contains(l, "bravo")
				beside = beside || strings.Contains(l, "│ Name") && strings.Contains(l, "SIZE")
				if w := ansi.StringWidth(l); w > c.width {
					t.Errorf("line of %d cells: %q", w, l)
				}
			}
			if below != c.wantBelow || beside == c.wantBelow {
				t.Errorf("panel below %v beside %v, want below: %v\n%s", below, beside, c.wantBelow, body(m))
			}
		})
	}

	m := newThings()
	keys(m, "/ c h enter i esc")
	if strings.Contains(body(m), "Secret") || !strings.HasPrefix(toolbar(m), "Search: ch") {
		t.Fatalf("esc did not close the panel alone:\n%s", body(m))
	}
	keys(m, "esc")
	if !strings.HasPrefix(toolbar(m), "Search: (none)") {
		t.Fatalf("the second esc kept the filter: %q", toolbar(m))
	}
}

// v flips the selected row's values against the profile's redaction, in the
// panel, the toolbar, the hints and the manifest, and only for that row.
func TestReveal(t *testing.T) {
	cases := []struct {
		name     string
		redact   bool
		keys     string
		wantShow bool
	}{
		{"redacted", true, "i", false},
		{"revealed with v", true, "i v", true},
		{"the next row stays redacted", true, "i v j", false},
		{"returning does not re-reveal", true, "i v j k", false},
		{"shown by default", false, "i", true},
		{"hidden with v", false, "i v", false},
		{"the next row stays shown", false, "i v j", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newThings()
			m.SetRedact(c.redact)
			keys(m, c.keys)
			sel, _ := m.Selected()
			wantToolbar, wantHint := "values redacted (v reveals)", "v reveal"
			if c.wantShow {
				wantToolbar, wantHint = "values shown (v redacts)", "v redact"
			}
			if got := strings.Contains(body(m), sel.secret); got != c.wantShow {
				t.Errorf("panel shows %s: %v, want %v\n%s", sel.secret, got, c.wantShow, body(m))
			}
			if got := strings.Contains(strings.Join(m.RawLines(), "\n"), sel.secret); got != c.wantShow {
				t.Errorf("manifest shows %s: %v, want %v", sel.secret, got, c.wantShow)
			}
			if !strings.Contains(toolbar(m), wantToolbar) || !strings.Contains(m.Hints(), wantHint) {
				t.Errorf("toolbar %q, hints %q; want %q and %q", toolbar(m), m.Hints(), wantToolbar, wantHint)
			}
		})
	}

	m := newThings()
	keys(m, "i v")
	m.SetRedact(true)
	if strings.Contains(body(m), "s-bravo") {
		t.Error("a profile that redacts inherited the previous profile's flip")
	}
}

// A panel taller than its room flows into two columns on a wide pane, and is
// cut with a pointer to the manifest on a narrow one.
func TestTallPanel(t *testing.T) {
	spec := thingSpec(true)
	spec.Info = func(t thing, _ bool, _ time.Time) []Field {
		var f []Field
		for i := 0; i < 20; i++ {
			f = append(f, Field{Label: "F" + strconv.Itoa(i), Value: "v" + strconv.Itoa(i)})
		}
		return f
	}
	m := newList(spec, 120, 30, things())
	keys(m, "i")
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

// Every state reads as itself on an empty table, with the failure's reason.
func TestEmptyStates(t *testing.T) {
	note := thingSpec(true)
	note.EmptyNote = "(which can also mean the server keeps none of them at all)"
	cases := []struct {
		name   string
		spec   Spec[thing]
		items  []thing
		status Status
		msg    string
		setup  func(*Model[thing])
		want   []string
	}{
		{"loading", thingSpec(true), nil, StatusLoading, "", nil, []string{"loading things…"}},
		{"forbidden", thingSpec(true), nil, StatusForbidden, "cannot list", nil, []string{"no things visible: list forbidden", "cannot list"}},
		{"unauthenticated", thingSpec(true), nil, StatusUnauthenticated, "bad token", nil, []string{"not authenticated", "bad token"}},
		{"unsupported", thingSpec(true), nil, StatusUnsupported, "no such list", nil, []string{"no things on this server", "no such list"}},
		{"failed", thingSpec(true), nil, StatusStale, "boom", nil, []string{"no things visible: the list failed", "stale 1m — boom"}},
		{"empty namespace", thingSpec(true), nil, StatusIdle, "", nil, []string{"no things in this namespace"}},
		{"empty cluster", thingSpec(false), nil, StatusIdle, "", nil, []string{"no things on this cluster"}},
		{"every namespace", thingSpec(true), nil, StatusIdle, "", func(m *Model[thing]) { m.SetAllNamespaces(true) },
			[]string{"no things in any namespace this token can read"}},
		{"nothing matches", thingSpec(true), things(), StatusIdle, "", func(m *Model[thing]) { keys(m, "/ z z z enter") },
			[]string{"no things match the current filter"}},
		{"note wrapped, not clipped", note, nil, StatusIdle, "", func(m *Model[thing]) { m.SetSize(60, 20) },
			[]string{"no things in this namespace", "keeps none of them at all)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newList(c.spec, 100, 20, c.items)
			m.SetStatus(c.status, c.msg, time.Minute)
			if c.setup != nil {
				c.setup(m)
			}
			out := body(m)
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("want %q in\n%s", want, out)
				}
			}
		})
	}
}

// The toolbar note stays until Reset, which also drops the rows for a new
// scope and shows it loading.
func TestNoteAndReset(t *testing.T) {
	m := newThings()
	m.SetNote("newest 3 only")
	if !strings.Contains(toolbar(m), "newest 3 only") {
		t.Fatalf("note not shown: %q", toolbar(m))
	}
	m.Reset()
	out := body(m)
	if strings.Contains(out, "newest 3 only") || len(shown(m)) != 0 || !strings.Contains(out, "loading things…") {
		t.Fatalf("after reset:\n%s", out)
	}
}

// Server text is sanitized before it reaches the screen.
func TestCellsAreSanitized(t *testing.T) {
	m := newList(thingSpec(true), 100, 10, []thing{{"a", "evil\x1b[31mred", 1, ""}})
	if strings.Contains(strings.Join(m.BodyLines(epoch), ""), "\x1b[31m") {
		t.Fatal("an escape sequence from a name reached the screen")
	}
}

// The table keeps to the pane at every width; below 60 cells the pane asks
// for room.
func TestWidths(t *testing.T) {
	m := newThings()
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
		many = append(many, thing{"a", "t" + strconv.Itoa(100+i), i, ""})
	}
	cases := []struct {
		keys string
		want string
	}{
		{"", "1-6/30"},
		{"G", "25-30/30"},
		{"pgdown pgdown", "6-11/30"},
	}
	for _, c := range cases {
		t.Run(c.keys, func(t *testing.T) {
			m := newList(thingSpec(true), 100, 8, many)
			keys(m, c.keys)
			m.BodyLines(epoch)
			if got := m.WindowStatus(); got != c.want {
				t.Errorf("window = %q, want %q", got, c.want)
			}
		})
	}
	if got := newThings().WindowStatus(); got != "3 shown" {
		t.Errorf("window of a list that fits = %q, want 3 shown", got)
	}
}
