package workflowlist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

func spaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }
func escKey() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }

// markedModel is a six-row list with the first and third rows marked.
func markedModel(t *testing.T) (Model, []core.Summary) {
	t.Helper()
	m := tl(t)
	items := summariesFrom(testkit.FixtureWorkflowList("ns", 6))
	m.SetItems(items, testkit.FixtureEpoch)
	m.SetSize(120, 30)
	m.Update(spaceKey())
	m.Update(runeKey('j'))
	m.Update(runeKey('j'))
	m.Update(spaceKey())
	return m, items
}

func markedNames(m *Model) []string {
	var out []string
	for _, s := range m.Marked() {
		out = append(out, s.Ref.Name)
	}
	return out
}

// Space marks the selected row and a second space unmarks it; the cursor
// does not move either way.
func TestSpaceTogglesTheMarkOnTheSelectedRow(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 3)), testkit.FixtureEpoch)
	sel := m.SelectedRef()
	m.Update(spaceKey())
	if !m.IsMarked(sel.UID) || m.MarkCount() != 1 || m.SelectedRef() != sel {
		t.Fatalf("space did not mark the selected row in place: count=%d sel=%v", m.MarkCount(), m.SelectedRef())
	}
	m.Update(spaceKey())
	if m.IsMarked(sel.UID) || m.MarkCount() != 0 {
		t.Fatalf("a second space did not unmark: count=%d", m.MarkCount())
	}
}

// Marks are keyed by UID, so they stay on their workflows through a
// refresh that reorders the snapshot, and through every sort.
func TestMarksSurviveRefreshAndSort(t *testing.T) {
	m, items := markedModel(t)
	want := map[string]bool{}
	for _, s := range m.Marked() {
		want[s.Ref.UID] = true
	}
	if len(want) != 2 {
		t.Fatalf("marked %d, want 2", len(want))
	}
	reversed := make([]core.Summary, len(items))
	for i := range items {
		reversed[i] = items[len(items)-1-i]
	}
	m.SetItems(reversed, testkit.FixtureEpoch)
	for i := 0; i < 3; i++ {
		m.Update(runeKey('s'))
		got := m.Marked()
		if len(got) != 2 || !want[got[0].Ref.UID] || !want[got[1].Ref.UID] {
			t.Fatalf("sort %v moved the marks: %v", m.SortKey(), markedNames(&m))
		}
	}
}

// A mark hidden by the filter is still a mark, and the toolbar says how
// many are hidden.
func TestAHiddenMarkStillCountsAndIsNamed(t *testing.T) {
	m, _ := markedModel(t)
	names := markedNames(&m)
	m.SetQuery(names[0])
	if m.MarkCount() != 2 || m.HiddenMarkCount() != 1 {
		t.Fatalf("count=%d hidden=%d, want 2 and 1", m.MarkCount(), m.HiddenMarkCount())
	}
	if len(m.Marked()) != 2 {
		t.Fatalf("the hidden mark is missing from Marked(): %v", markedNames(&m))
	}
	view := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(view, "2 marked (1 hidden by filter)") {
		t.Fatalf("toolbar does not name the hidden mark:\n%s", view)
	}
}

// A mark whose workflow left the snapshot is dropped: it could never be
// shown again, and a bulk action must not reach it.
func TestAMarkOnARemovedWorkflowIsDropped(t *testing.T) {
	m, items := markedModel(t)
	gone := m.Marked()[0].Ref.UID
	var kept []core.Summary
	for _, s := range items {
		if s.Ref.UID != gone {
			kept = append(kept, s)
		}
	}
	m.SetItems(kept, testkit.FixtureEpoch)
	if m.MarkCount() != 1 || m.IsMarked(gone) {
		t.Fatalf("count=%d, removed still marked=%v", m.MarkCount(), m.IsMarked(gone))
	}
}

// Esc clears the marks first and the filter on the next press.
func TestEscClearsMarksBeforeTheFilter(t *testing.T) {
	m, _ := markedModel(t)
	m.SetQuery("fixture")
	m.Update(escKey())
	if m.MarkCount() != 0 || m.Query() != "fixture" {
		t.Fatalf("first esc: marks=%d query=%q, want marks cleared and the filter kept", m.MarkCount(), m.Query())
	}
	m.Update(escKey())
	if m.Query() != "" {
		t.Fatalf("second esc left the filter %q", m.Query())
	}
}

// A marked row carries the mark glyph, so the mark reads without colour.
func TestAMarkedRowCarriesTheGlyph(t *testing.T) {
	m, _ := markedModel(t)
	marked := map[string]bool{}
	for _, n := range markedNames(&m) {
		marked[n] = true
	}
	for _, line := range strings.Split(m.ViewAt(testkit.FixtureEpoch), "\n") {
		if !strings.Contains(line, "fixture-wf-") {
			continue
		}
		name := strings.Fields(strings.TrimPrefix(line, markGlyph))[0]
		if got := strings.HasPrefix(line, markGlyph+" "); got != marked[name] {
			t.Errorf("row %q: glyph=%v, marked=%v", name, got, marked[name])
		}
	}
}

// Marked() runs in the display order, not the order the marks were made.
func TestMarkedFollowsTheSortOrder(t *testing.T) {
	m, _ := markedModel(t)
	rows := m.Rows()
	pos := map[string]int{}
	for i, r := range rows {
		pos[r.Ref.UID] = i
	}
	got := m.Marked()
	if pos[got[0].Ref.UID] > pos[got[1].Ref.UID] {
		t.Fatalf("marked order %v does not follow the rows", markedNames(&m))
	}
}
