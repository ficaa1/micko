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

func TestMarksAcrossSnapshots(t *testing.T) {
	t.Run("toggle in place", func(t *testing.T) {
		m := newList(t)
		m.SetItems([]core.Summary{summary("a", "Running")}, testkit.FixtureEpoch)
		m.Update(spaceKey())
		if !slices.Equal(rowIDs(m.Marked()), []string{"a"}) || m.SelectedRef().UID != "a" {
			t.Fatal("toggle changed identity")
		}
		m.Update(spaceKey())
		if m.MarkCount() != 0 {
			t.Fatal("unmark failed")
		}
		m.SetItems(nil, testkit.FixtureEpoch)
		m.Update(spaceKey())
		if m.MarkCount() != 0 {
			t.Fatal("empty list marked")
		}
	})
	m, items := markedModel(t)
	for _, want := range [][]string{{"b", "a"},
		{"a", "b"},
		{"a", "b"},
		{"b", "a"}} {
		if got := rowIDs(m.Marked()); !slices.Equal(got, want) {
			t.Fatalf("sort %s marks %v want %v", m.sort, got, want)
		}
		m.Update(runeKey('s'))
	}
	m.Update(runeKey('s'))
	m.Update(runeKey('s'))
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
		t.Fatal("mark transferred by name")
	}
}

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
		t.Fatal("hidden target omitted")
	}
	m.Update(runeKey('/'))
	typeText(&m, " /bad(/")
	if m.HiddenMarkCount() != 1 {
		t.Fatal("invalid query changed hidden marks")
	}
	m.Update(escKey())
	m.SetQuery("")
	if m.HiddenMarkCount() != 0 || !slices.Equal(rowIDs(m.Marked()), []string{"b", "a"}) {
		t.Fatal("filter dropped marks")
	}
}

func TestEscapeClearsMarksBeforeFilter(t *testing.T) {
	m, _ := markedModel(t)
	m.SetQuery("a")
	sel := m.SelectedRef()
	for _, c := range []struct {
		marks int
		q     string
	}{{0, "a"},
		{0, ""},
		{0, ""}} {
		if cmd := m.Update(escKey()); cmd != nil {
			t.Fatal("Escape emitted command")
		}
		if m.MarkCount() != c.marks || m.Query() != c.q || m.SelectedRef() != sel {
			t.Fatalf("marks %d query %q selection %+v", m.MarkCount(), m.Query(), m.SelectedRef())
		}
	}
}
