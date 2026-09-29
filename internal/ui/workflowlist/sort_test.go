package workflowlist

import (
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// testTheme is the shared plain theme for tests in this package.
func testTheme() shared.Theme { return shared.NewTheme(true) }

func TestSortKeyCycle(t *testing.T) {
	k := SortPhaseName
	want := []SortKey{SortName, SortTime, SortPhaseName}
	for i, w := range want {
		k = k.Next()
		if k != w {
			t.Fatalf("step %d: got %v, want %v", i, k, w)
		}
	}
}

func TestSortUnknownPhasesLastDeterministic(t *testing.T) {
	// Unknown phases must be displayable AND sort after the
	// canonical buckets, deterministically by name.
	items := []core.Summary{
		{Ref: core.Ref{Name: "zeta"}, Phase: "MysteryPhase"},
		{Ref: core.Ref{Name: "alpha"}, Phase: "MysteryPhase"},
		{Ref: core.Ref{Name: "mid"}, Phase: "Running"},
	}
	Sort(items, SortPhaseName)
	if items[0].Ref.Name != "mid" {
		t.Fatalf("canonical phase not first: %v", items[0])
	}
	if items[1].Ref.Name != "alpha" || items[2].Ref.Name != "zeta" {
		t.Fatalf("unknown phase group unordered: %v, %v", items[1], items[2])
	}
}

func TestSortTreatsErrorLikeFailedBucket(t *testing.T) {
	// "Error" is canonical upstream and themes as failure; phase rank
	// groups it with Failed deterministically.
	items := []core.Summary{
		{Ref: core.Ref{Name: "err"}, Phase: "Error"},
		{Ref: core.Ref{Name: "fail"}, Phase: "Failed"},
	}
	Sort(items, SortPhaseName)
	if items[0].Ref.Name != "err" || items[1].Ref.Name != "fail" {
		t.Fatalf("Error/Failed order: %v, %v", items[0], items[1])
	}
}

func TestPhaseStyleFallbackText(t *testing.T) {
	// Unknown phases stay displayable: the view text carries the phase
	// even without color (color is never the only carrier).
	m := New(testTheme(), true)
	if got := m.rowPhaseText("WeirdFuturePhase"); got != "WeirdFuturePhase" {
		t.Fatalf("rowPhaseText = %q", got)
	}
	if got := m.rowPhaseText(""); got != "(no phase)" {
		t.Fatalf("empty phase text = %q", got)
	}
}

func TestPhaseStyleUsesTheme(t *testing.T) {
	// The phase style is consulted so colors accompany text where the
	// theme has them; the plain theme renders identical text.
	m := New(testTheme(), true)
	if m.theme.PhaseStyle("Running").Value() != "" {
		t.Fatal("plain theme should carry no color value")
	}
}
