package workflowlist

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/testkit"
)

// lineCount counts rendered terminal lines for a view string.
func lineCount(s string) int {
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

// TestViewFitsHeightBudget pins the live-smoke defect that motivated this
// slice: with more rows than the terminal can show, the list rendered every
// row and pushed the footer (and its key hints) off the screen. The view
// must never emit more lines than the height it was given.
func TestViewFitsHeightBudget(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 40)), testkit.FixtureEpoch)
	for _, h := range []int{8, 12, 24} {
		m.SetSize(100, h)
		v := m.ViewAt(testkit.FixtureEpoch)
		if got := lineCount(v); got > h {
			t.Fatalf("height %d: rendered %d lines, must not exceed the budget:\n%s", h, got, v)
		}
		if !strings.Contains(v, "? help") {
			t.Fatalf("height %d: footer key hints must survive the clamp:\n%s", h, v)
		}
	}
}

// TestViewportKeepsSelectionVisible: moving the selection past the bottom of
// the window must scroll the window, not hide the selected row.
func TestViewportKeepsSelectionVisible(t *testing.T) {
	m := tl(t)
	items := summariesFrom(testkit.FixtureWorkflowList("ns", 40))
	m.SetItems(items, testkit.FixtureEpoch)
	m.SetSize(100, 12)

	for i := 0; i < 30; i++ {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	sel := m.Selected()
	if sel.Ref.Name == "" {
		t.Fatal("expected a selection after 30 moves")
	}
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, sel.Ref.Name) {
		t.Fatalf("selected row %q scrolled out of the window:\n%s", sel.Ref.Name, v)
	}
	if got := lineCount(v); got > 12 {
		t.Fatalf("rendered %d lines for height 12:\n%s", got, v)
	}

	// Moving back up must scroll the other way and keep the row visible.
	for i := 0; i < 30; i++ {
		m.Update(tea.KeyPressMsg{Code: 'k'})
	}
	sel = m.Selected()
	v = m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, sel.Ref.Name) {
		t.Fatalf("selected row %q scrolled out after moving up:\n%s", sel.Ref.Name, v)
	}
}

// TestViewportShowsPosition: when the window hides rows, the footer must say
// where the user is, so a windowed list is distinguishable from a short one.
func TestViewportShowsPosition(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 40)), testkit.FixtureEpoch)
	m.SetSize(100, 12)
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "/40") {
		t.Fatalf("windowed list must show its position in the footer:\n%s", v)
	}

	// A list that fits entirely needs no position indicator.
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 3)), testkit.FixtureEpoch)
	m.SetSize(100, 24)
	v = m.ViewAt(testkit.FixtureEpoch)
	if strings.Contains(v, "/3") {
		t.Fatalf("a fully visible list must not claim to be windowed:\n%s", v)
	}
}

// TestUnknownHeightRendersEveryRow: height 0 means "unknown" (tests and the
// pre-resize first frame). The view must not clamp to nothing.
func TestUnknownHeightRendersEveryRow(t *testing.T) {
	m := tl(t)
	items := summariesFrom(testkit.FixtureWorkflowList("ns", 12))
	m.SetItems(items, testkit.FixtureEpoch)
	m.SetSize(100, 0)
	v := m.ViewAt(testkit.FixtureEpoch)
	for _, it := range items {
		if !strings.Contains(v, it.Ref.Name) {
			t.Fatalf("row %q missing when height is unknown:\n%s", it.Ref.Name, v)
		}
	}
}

// TestEscapeClearsAppliedFilter: Esc on the list, with the search input NOT
// focused, clears an applied query. Without this a filter that matches
// nothing looks exactly like an empty namespace, with no obvious way back.
func TestEscapeClearsAppliedFilter(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 5)), testkit.FixtureEpoch)
	m.SetSize(100, 24)
	m.SetQuery("no-such-workflow")
	if m.VisibleCount() != 0 {
		t.Fatalf("precondition: filter should hide every row, visible=%d", m.VisibleCount())
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Query() != "" {
		t.Fatalf("Esc must clear the applied query, got %q", m.Query())
	}
	if m.VisibleCount() != 5 {
		t.Fatalf("clearing the filter must restore every row, visible=%d", m.VisibleCount())
	}
	if m.SearchOn {
		t.Fatal("Esc must not open the search input")
	}
}

// TestEscapeWithoutFilterIsInert: Esc on an unfiltered list must stay a no-op
// so it never becomes a surprise quit on the top route.
func TestEscapeWithoutFilterIsInert(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 5)), testkit.FixtureEpoch)
	before := m.SelectedRef()
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("Esc on an unfiltered list must emit no command")
	}
	if m.SelectedRef() != before {
		t.Fatal("Esc must not move the selection")
	}
}
