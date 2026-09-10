package logs

import (
	"strings"
	"testing"
)

// The shell owns the footer band, so the pane body must not repeat the
// key hints.
func TestLogBodyLinesExcludeTheFooter(t *testing.T) {
	m := testModel(t)
	m.SetPaneMode(true)
	for i, l := range m.BodyLines() {
		if strings.Contains(l, "space pause") {
			t.Fatalf("body line %d carries footer hints: %q", i, l)
		}
	}
}

// The workflow name rides the shell border, so the body must not repeat it
// as a heading row.
func TestLogPaneTitleCarriesTheWorkflowName(t *testing.T) {
	m := testModel(t)
	if !strings.Contains(m.PaneTitle(), "wf-1") {
		t.Fatalf("pane title lost the workflow name: %q", m.PaneTitle())
	}
	m.SetPaneMode(true)
	if strings.HasPrefix(m.BodyLines()[0], "logs: ") {
		t.Fatal("body repeats the pane title")
	}
}

// In pane mode the body must fit the budget SetSize gave it, or the shell
// clips rows the viewer believes are on screen.
func TestLogBodyLinesFitTheHeightBudget(t *testing.T) {
	for _, h := range []int{4, 8, 20} {
		m := testModel(t)
		m.SetPaneMode(true)
		m.SetSize(80, h)
		m.ApplyRecords(recs(100))
		if got := len(m.BodyLines()); got > h {
			t.Fatalf("height %d: body is %d lines", h, got)
		}
	}
}

// Scroll paging and the visible window must agree. If viewRows and the
// rendered body disagree, pgup skips or repeats lines.
func TestLogPaneScrollMatchesTheVisibleRows(t *testing.T) {
	m := testModel(t)
	m.SetPaneMode(true)
	m.SetSize(80, 12)
	m.ApplyRecords(recs(60))
	if got, want := len(m.window()), m.viewRows(); got != want {
		t.Fatalf("window shows %d rows but viewRows says %d", got, want)
	}
}

// Hints must name the keys the help overlay documents for logs.
func TestLogHintsAdvertiseTheDocumentedKeys(t *testing.T) {
	m := testModel(t)
	h := m.Hints()
	// Movement keys were deliberately dropped from the footer: they belong
	// to the `?` overlay, and the footer has to name the keys unique to logs.
	for _, key := range []string{"space", "t follow", "/", "c", "esc"} {
		if !strings.Contains(h, key) {
			t.Fatalf("hints %q omit key %q", h, key)
		}
	}
}

// Pane mode is opt-in. The standalone View must keep its own title and
// footer so nothing that renders logs directly loses them.
func TestStandaloneViewKeepsTitleAndFooter(t *testing.T) {
	m := testModel(t)
	v := m.View()
	if !strings.HasPrefix(v, "logs: wf-1") {
		t.Fatalf("standalone view lost its title: %q", v)
	}
	if !strings.Contains(v, "space pause") {
		t.Fatal("standalone view lost its footer")
	}
}
