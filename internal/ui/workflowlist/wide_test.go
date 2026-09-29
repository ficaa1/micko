package workflowlist

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func enterKey() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func backspaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

// colorTheme is the coloured theme whatever NO_COLOR says.
func colorTheme() shared.Theme {
	os.Unsetenv("NO_COLOR")
	return shared.NewTheme(false)
}

// demoList is a list over the demo dataset, sized, with STARTED and
// FINISHED pinned to UTC so the render does not depend on the machine.
func demoList(t *testing.T, w, h int) Model {
	t.Helper()
	m := tl(t)
	m.loc = time.UTC
	items, now := demoSummaries(t)
	m.SetItems(items, now)
	m.SetSize(w, h)
	return m
}

func columnKeys(m *Model) []string {
	var out []string
	for _, c := range m.columns() {
		out = append(out, c.key.title())
	}
	return out
}

// The layout at each width, in both modes. Wide mode drops its optional
// columns in the stated order, LABELS first and PROGRESS last, and the
// normal layout gains PROGRESS from 120 cells.
func TestColumnsAtEachWidth(t *testing.T) {
	cases := []struct {
		width      int
		wide       bool
		want       []string
		messageMin int
	}{
		{200, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED", "FINISHED", "TEMPLATE", "CRON", "LABELS", "MESSAGE"}, minMessageWidth},
		{160, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED", "FINISHED", "TEMPLATE", "CRON", "MESSAGE"}, minMessageWidth},
		{140, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED", "FINISHED", "TEMPLATE", "MESSAGE"}, minMessageWidth},
		{120, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED", "TEMPLATE"}, 0},
		{100, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "STARTED"}, 0},
		{80, true, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS"}, 0},
		{60, true, []string{"NAME", "PHASE", "AGE", "DURATION"}, 0},
		{160, false, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "MESSAGE"}, minMessageWidth},
		{120, false, []string{"NAME", "PHASE", "AGE", "DURATION", "PROGRESS", "MESSAGE"}, minMessageWidth},
		{119, false, []string{"NAME", "PHASE", "AGE", "DURATION", "MESSAGE"}, minMessageWidth},
		{100, false, []string{"NAME", "PHASE", "AGE", "DURATION", "MESSAGE"}, minMessageWidth},
		{80, false, []string{"NAME", "PHASE", "AGE", "DURATION", "MESSAGE"}, minMessageWidth},
		{60, false, []string{"NAME", "PHASE", "AGE", "DURATION"}, 0},
	}
	for _, c := range cases {
		m := demoList(t, c.width, 30)
		if c.wide {
			m.ToggleWide()
		}
		if got := columnKeys(&m); !slices.Equal(got, c.want) {
			t.Errorf("width %d wide=%v: columns %v, want %v", c.width, c.wide, got, c.want)
		}
		if msg := m.columnWidth(colMessage); c.messageMin > 0 && msg < c.messageMin {
			t.Errorf("width %d wide=%v: message column %d, want at least %d", c.width, c.wide, msg, c.messageMin)
		}
		// The head names every column in the layout, and no table line is
		// wider than the pane.
		body := m.BodyLines(testkit.FixtureEpoch)
		head := body[1]
		for _, k := range c.want {
			if !strings.Contains(head, k) {
				t.Errorf("width %d wide=%v: head %q lacks %s", c.width, c.wide, head, k)
			}
		}
		for _, line := range body[1:] {
			if w := ansi.StringWidth(line); w > c.width {
				t.Errorf("width %d wide=%v: line is %d cells:\n%q", c.width, c.wide, w, line)
			}
		}
	}
}

// w toggles wide mode, and it is a list key only: while the filter input
// has focus it is a letter.
func TestWTogglesWideModeOutsideTheFilter(t *testing.T) {
	m := demoList(t, 140, 30)
	m.Update(runeKey('w'))
	if !m.Wide() {
		t.Fatal("w did not turn wide mode on")
	}
	m.Update(runeKey('w'))
	if m.Wide() {
		t.Fatal("a second w did not turn wide mode off")
	}
	m.Update(runeKey('/'))
	m.Update(runeKey('w'))
	if m.Wide() || m.SearchValue() != "w" {
		t.Fatalf("w in the filter input toggled wide mode (wide=%v, buffer %q)", m.Wide(), m.SearchValue())
	}
}

// The wide cells: progress as a count and a glyph bar, local start and
// finish times, the template and cron origin from the labels, and the
// labels no other column shows.
func TestWideCellsCarryTheMetadata(t *testing.T) {
	m := demoList(t, 220, 30)
	m.ToggleWide()
	body := strings.Join(m.BodyLines(testkit.FixtureEpoch), "\n")
	for _, want := range []string{
		"5/6 █████░",      // nightly-report: one retried pod short of done
		"3/8 ██░░░░",      // train-pipeline, mid fan-out
		"1/1 ██████",      // hello-world
		"nightly-report",  // TEMPLATE
		"demo-etl-hourly", // CRON
		"env=prod,team=platform",
		"team=data",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wide body lacks %q:\n%s", want, body)
		}
	}
	// The labels a column already shows are not repeated in LABELS.
	for _, dup := range []string{"workflows.argoproj.io/phase", "workflows.argoproj.io/completed", "workflows.argoproj.io/workflow-template"} {
		if strings.Contains(body, dup) {
			t.Errorf("LABELS repeats %s:\n%s", dup, body)
		}
	}
	start := testkit.FixtureEpoch.Add(-26 * time.Minute).UTC().Format("01-02 15:04")
	end := testkit.FixtureEpoch.Add(-22 * time.Minute).UTC().Format("01-02 15:04")
	if !strings.Contains(body, start+"  "+end) {
		t.Errorf("hello-world's STARTED/FINISHED %s / %s missing:\n%s", start, end, body)
	}
}

func TestProgressCell(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "    -"},
		{"0/4", "  0/4 ░░░░░░"},
		{"3/7", "  3/7 ██░░░░"},
		{"6/7", "  6/7 █████░"}, // never full until done
		{"7/7", "  7/7 ██████"},
		{"0/0", "  0/0 ░░░░░░"},
		{"120/1500", "120/1500 ░░░"},
		{"9/3", "9/3"}, // nonsense is shown as sent, without a bar
		{"lots", "lots"},
		{"123456/1234567", "123456/1234…"},
	} {
		if got := progressCell(c.in, progressWidth); got != c.want {
			t.Errorf("progressCell(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Wide mode on the demo at 160 cells, and at 100 where it has dropped to
// PROGRESS and STARTED.
func TestWideGoldens(t *testing.T) {
	for _, w := range []int{160, 100} {
		m := demoList(t, w, 20)
		m.ToggleWide()
		golden(t, filepath.Join("testdata", "wide-"+itoa(w)+".golden"), m.ViewAt(testkit.FixtureEpoch))
	}
}

// Marks under a query filter: the toolbar counts the marks the query
// hides, and the golden pins the leading mark cell and the parsed query.
func TestMarksUnderTheQueryFilter(t *testing.T) {
	m := demoList(t, 120, 20)
	// Mark the first three rows: the gate, the failed ETL run and the
	// nightly report.
	for i := 0; i < 3; i++ {
		m.Update(spaceKey())
		m.Update(runeKey('j'))
	}
	if m.MarkCount() != 3 {
		t.Fatalf("marked %d, want 3", m.MarkCount())
	}
	if err := m.SetQuery("phase=failed tmpl=nightly-report|cron=demo-etl-hourly"); err != nil {
		t.Fatal(err)
	}
	if got := m.HiddenMarkCount(); got != 1 {
		t.Fatalf("hidden marks = %d, want 1 (the gate)", got)
	}
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "◆ 3 marked (1 hidden by filter)") {
		t.Fatalf("toolbar does not count the hidden mark:\n%s", v)
	}
	golden(t, filepath.Join("testdata", "marks-query.golden"), v)

	// A query that does not parse leaves the rows, so the hidden count
	// stands.
	if err := m.SetQuery("phase="); err == nil {
		t.Fatal("phase= parsed")
	}
	if got := m.HiddenMarkCount(); got != 1 {
		t.Fatalf("a bad query changed the hidden count to %d", got)
	}
	// Marks survive the filter: clearing it shows all three again.
	m.SetQuery("")
	if got := m.HiddenMarkCount(); got != 0 {
		t.Fatalf("hidden marks after clearing = %d", got)
	}
	if got := len(m.Marked()); got != 3 {
		t.Fatalf("marked after clearing = %d", got)
	}
}

// Typing narrows live; a term that does not parse keeps the last filter
// that did, shows the error in the toolbar, and enter will not apply it.
// Esc restores the filter from before the input opened.
func TestTheToolbarShowsTheQueryError(t *testing.T) {
	m := demoList(t, 160, 30)
	if err := m.SetQuery("etl"); err != nil {
		t.Fatal(err)
	}
	m.Update(runeKey('/'))
	for _, r := range " phase=failed" {
		if r == ' ' {
			m.Update(spaceKey())
			continue
		}
		m.Update(runeKey(r))
	}
	if got := m.Query(); got != "etl phase=failed" {
		t.Fatalf("live query = %q", got)
	}
	if n := m.VisibleCount(); n != 1 {
		t.Fatalf("etl phase=failed shows %d rows, want 1", n)
	}
	// Every prefix of this term is an unterminated or broken regex, so
	// the filter must hold at "etl phase=failed" throughout.
	for _, r := range " /hourly(/" {
		if r == ' ' {
			m.Update(spaceKey())
			continue
		}
		m.Update(runeKey(r))
		if n := m.VisibleCount(); n != 1 || m.Query() != "etl phase=failed" {
			t.Fatalf("a bad term changed the filter: %q, %d rows", m.Query(), n)
		}
	}
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "✗ bad regex /hourly(/: missing closing )") {
		t.Fatalf("toolbar does not show the error:\n%s", v)
	}
	m.Update(enterKey())
	if !m.SearchOn {
		t.Fatal("enter applied a query that does not parse")
	}
	m.Update(backspaceKey())
	m.Update(backspaceKey())
	m.Update(runeKey('/'))
	if m.QueryError() != "" || m.Query() != "etl phase=failed /hourly/" || m.VisibleCount() != 1 {
		t.Fatalf("the corrected query: %q (%d rows), error %q", m.Query(), m.VisibleCount(), m.QueryError())
	}
	v = m.ViewAt(testkit.FixtureEpoch)
	if strings.Contains(v, "✗ bad") {
		t.Fatalf("the error outlived its fix:\n%s", v)
	}
	m.Update(escKey())
	if m.SearchOn || m.Query() != "etl" || m.QueryError() != "" {
		t.Fatalf("esc did not restore the filter: on=%v query=%q err=%q", m.SearchOn, m.Query(), m.QueryError())
	}
	// Applied, the toolbar shows the parsed form.
	m.Update(runeKey('/'))
	for _, r := range "|/gate$/" {
		m.Update(runeKey(r))
	}
	m.Update(enterKey())
	v = m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "Search: etl|/gate$/ [within 12 collected]") {
		t.Fatalf("toolbar does not show the parsed filter:\n%s", v)
	}
}

// The error is styled as an error. The plain theme would pass any text, so
// this renders with colour.
func TestTheQueryErrorUsesTheErrorStyle(t *testing.T) {
	m := New(colorTheme())
	m.SetItems([]core.Summary{{Ref: core.Ref{Name: "a", UID: "a"}}}, testkit.FixtureEpoch)
	m.SetSize(160, 10)
	m.Update(runeKey('/'))
	for _, r := range "age<" {
		m.Update(runeKey(r))
	}
	want := m.theme.ErrorText.Render("✗ age< needs a value")
	if !strings.Contains(want, "\x1b[") {
		t.Fatal("the coloured theme rendered the error plain; the check below would prove nothing")
	}
	if !strings.Contains(m.ViewAt(testkit.FixtureEpoch), want) {
		t.Fatalf("the error is not rendered in the error style:\n%q", m.ViewAt(testkit.FixtureEpoch))
	}
}
