package workflowlist

import (
	"slices"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

func markedModel(t *testing.T) (Model, []core.Summary) {
	t.Helper()
	m := newList(t)
	items := []core.Summary{summary("a", "Running"), summary("b", "Failed"), summary("c", "Succeeded")}
	items[0].CreatedAt = testkit.FixtureEpoch
	m.SetItems(items, testkit.FixtureEpoch)
	m.Update(spaceKey())
	m.Update(runeKey('j'))
	m.Update(spaceKey())
	return m, items
}

// Marks stay on workflow UIDs through sorting and refreshes and disappear when their workflow leaves.
func TestMarksAcrossSnapshots(t *testing.T) {
	t.Run("toggle in place", func(t *testing.T) {
		m := newList(t)
		m.SetItems([]core.Summary{summary("a", "Running")}, testkit.FixtureEpoch)
		m.Update(spaceKey())
		if !slices.Equal(rowIDs(m.Marked()), []string{"a"}) || m.SelectedRef().UID != "a" {
			t.Fatalf("marked=%v selection=%q, want [a] and a", rowIDs(m.Marked()), m.SelectedRef().UID)
		}
		m.Update(spaceKey())
		if m.MarkCount() != 0 {
			t.Fatalf("marks after second Space = %v, want none", rowIDs(m.Marked()))
		}
		m.SetItems(nil, testkit.FixtureEpoch)
		m.Update(spaceKey())
		if m.MarkCount() != 0 {
			t.Fatalf("empty list marks = %v, want none", rowIDs(m.Marked()))
		}
	})
	m, items := markedModel(t)

	cases := []struct {
		sort SortKey
		want []string
	}{
		{SortPhaseName, []string{"b", "a"}},
		{SortName, []string{"a", "b"}},
		{SortTime, []string{"a", "b"}},
		{SortPhaseName, []string{"b", "a"}},
	}
	for i, c := range cases {
		if got := rowIDs(m.Marked()); m.sort != c.sort || !slices.Equal(got, c.want) {
			t.Fatalf("sort %q marks %v, want sort %q marks %v", m.sort, got, c.sort, c.want)
		}
		if i < len(cases)-1 {
			m.Update(runeKey('s'))
		}
	}
	items[1].Phase = "Succeeded"
	m.SetItems([]core.Summary{items[2], items[1], items[0]}, testkit.FixtureEpoch)
	if got := rowIDs(m.Marked()); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("refresh lost marks %v", got)
	}
	m.SetItems([]core.Summary{items[0], items[2]}, testkit.FixtureEpoch)
	if got := rowIDs(m.Marked()); !slices.Equal(got, []string{"a"}) || m.MarkCount() != 1 {
		t.Fatalf("deleted mark survived %v", got)
	}
	replacement := items[0]
	replacement.Ref.UID = "replacement"
	m.SetItems([]core.Summary{replacement}, testkit.FixtureEpoch)
	if m.MarkCount() != 0 {
		t.Fatalf("replacement snapshot marks = %v, want none", rowIDs(m.Marked()))
	}
}

// Marked rows carry a glyph and hidden marks remain counted and available to bulk actions.
func TestMarkVisibility(t *testing.T) {
	m, _ := markedModel(t)
	lines := m.BodyLines(testkit.FixtureEpoch)
	if len(lines) != 5 {
		t.Fatalf("rows absent: %v", lines)
	}
	for i, want := range []bool{true, true, false} {
		if got := strings.HasPrefix(lines[i+2], "◆ "); got != want {
			t.Fatalf("row %d glyph %v want %v", i, got, want)
		}
	}
	editQuery(&m, "a")
	m.Update(enterKey())
	if m.MarkCount() != 2 || m.HiddenMarkCount() != 1 || !slices.Equal(rowIDs(m.Marked()), []string{"b", "a"}) || !strings.Contains(body(&m), "2 marked (1 hidden by filter)") {
		t.Fatalf("filtered marks=%v count=%d hidden=%d body=%q, want [b a], count 2 and hidden 1", rowIDs(m.Marked()), m.MarkCount(), m.HiddenMarkCount(), body(&m))
	}
	m.Update(runeKey('/'))
	typeText(&m, " /bad(/")
	if m.HiddenMarkCount() != 1 {
		t.Fatalf("hidden marks after invalid query = %d, want 1", m.HiddenMarkCount())
	}
	m.Update(escKey())
	m.SetQuery("")
	if m.HiddenMarkCount() != 0 || !slices.Equal(rowIDs(m.Marked()), []string{"b", "a"}) {
		t.Fatalf("cleared query marks=%v hidden=%d, want [b a] and 0 hidden", rowIDs(m.Marked()), m.HiddenMarkCount())
	}
}

// Escape clears marks before the applied filter and is inert after both are cleared.
func TestEscapeClearsMarksBeforeFilter(t *testing.T) {
	m, _ := markedModel(t)
	m.SetQuery("a")
	sel := m.SelectedRef()
	for _, c := range []struct {
		marks int
		q     string
	}{
		{0, "a"},
		{0, ""},
		{0, ""},
	} {
		if cmd := m.Update(escKey()); cmd != nil {
			t.Fatalf("Escape command = %v, want nil", cmd)
		}
		if m.MarkCount() != c.marks || m.Query() != c.q || m.SelectedRef() != sel {
			t.Fatalf("Escape marks=%d query=%q selection=%+v, want marks=%d query=%q selection=%+v", m.MarkCount(), m.Query(), m.SelectedRef(), c.marks, c.q, sel)
		}
	}
}

// Watched rows carry a glyph unless marked, and the toolbar counts every
// watch, including ones outside the snapshot.
func TestWatchedRows(t *testing.T) {
	m, _ := markedModel(t)
	m.ClearMarks()
	m.Update(spaceKey())
	m.SetWatched(map[string]bool{"a": true, "b": true, "elsewhere": true})
	lines := m.BodyLines(testkit.FixtureEpoch)
	if !strings.Contains(lines[0], "◉ 3 watched") {
		t.Fatalf("toolbar %q lacks the watch count", lines[0])
	}
	var gutters []string
	for _, l := range lines[2:5] {
		gutters = append(gutters, string([]rune(l)[:1]))
	}
	if got := strings.Join(gutters, ""); got != "◉◆ " {
		t.Fatalf("gutters %q, want the watch on b, the mark over the watch on a and nothing on c", got)
	}
}
