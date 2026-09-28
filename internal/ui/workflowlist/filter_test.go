package workflowlist

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

func TestPhaseFilterBuckets(t *testing.T) {
	cases := []struct {
		filter PhaseFilter
		phase  string
		want   bool
	}{
		{PhaseAll, "Running", true},
		{PhaseAll, "WeirdFuturePhase", true},
		{PhaseAll, "", true},
		{PhaseRunning, "Running", true},
		{PhaseRunning, "Pending", false},
		{PhaseFailed, "Error", false}, // Error is its own canonical bucket
		{PhaseOther, "WeirdFuturePhase", true},
		{PhaseOther, "Running", false},
		{PhaseOther, "Succeeded", false},
		{PhaseOther, "", true}, // missing phase = not canonical = Other
	}
	for _, tc := range cases {
		if got := tc.filter.Matches(core.Summary{Phase: tc.phase}); got != tc.want {
			t.Errorf("%q.Matches(%q) = %v, want %v", tc.filter, tc.phase, got, tc.want)
		}
	}
}

// A suspended workflow reports Running to the server, so the bucket must key
// on the Suspended flag rather than the phase word.
func TestPhaseFilterSuspendedBucket(t *testing.T) {
	gated := core.Summary{Phase: "Running", Suspended: true}
	plain := core.Summary{Phase: "Running"}
	if !PhaseSuspended.Matches(gated) {
		t.Error("suspended bucket rejected a gated workflow")
	}
	if PhaseSuspended.Matches(plain) {
		t.Error("suspended bucket accepted a plain running workflow")
	}
	if !PhaseRunning.Matches(gated) {
		t.Error("Running bucket must still contain a gated workflow: the server phase is Running")
	}
	if got := DisplayPhase(gated); got != "Suspended" {
		t.Errorf("DisplayPhase = %q, want Suspended", got)
	}
	if got := DisplayPhase(plain); got != "Running" {
		t.Errorf("DisplayPhase = %q, want Running", got)
	}
}

func TestPhaseFilterCycle(t *testing.T) {
	f := PhaseAll
	seen := map[PhaseFilter]bool{}
	for i := 0; i < len(phaseCycle); i++ {
		seen[f] = true
		f = f.Next()
	}
	if len(seen) != len(phaseCycle) {
		t.Fatalf("cycle repeats early: saw %v", seen)
	}
	if f != PhaseAll {
		t.Fatalf("cycle did not return to All: %v", f)
	}
}

func TestFilterAndViewIntegration(t *testing.T) {
	m := New(testTheme(), true)
	items := summariesFrom(testkit.FixtureWorkflowList("ns", 12)) // phases rotate incl. unknown
	m.SetItems(items, testkit.FixtureEpoch)
	if m.VisibleCount() != 12 {
		t.Fatalf("visible = %d, want 12", m.VisibleCount())
	}

	m.SetPhase(PhaseFailed)
	if m.VisibleCount() != 2 { // 12 items, 6-phase cycle → Failed appears twice
		t.Fatalf("Failed visible = %d, want 2", m.VisibleCount())
	}
	m.SetPhase(PhaseOther)
	if m.VisibleCount() != 2 { // WeirdFuturePhase appears twice
		t.Fatalf("Other visible = %d, want 2", m.VisibleCount())
	}

	m.SetPhase(PhaseAll)
	m.SetQuery("fixture-wf-0011") // exactly one name (11 is unambiguous)
	if m.VisibleCount() != 1 {
		t.Fatalf("query visible = %d, want 1", m.VisibleCount())
	}
	if got := m.SelectedRef().Name; got != "fixture-wf-0011" {
		t.Fatalf("selection after filter = %q, want the only row", got)
	}

	// Query matching nothing: selection must vanish (no phantom rows).
	m.SetQuery("no-such-workflow")
	if m.VisibleCount() != 0 || m.HasSelection() {
		t.Fatalf("empty result must clear selection: visible=%d sel=%v", m.VisibleCount(), m.SelectedRef())
	}
	m.SetQuery("")
	if m.VisibleCount() != 12 {
		t.Fatalf("clearing query should restore 12, got %d", m.VisibleCount())
	}
}

func TestSortTimeMissingTimestampsSensible(t *testing.T) {
	// Zero CreatedAt (never-created metadata) must not be treated as the
	// oldest real time — it goes last with a name tiebreak (plan gate).
	base := testkit.FixtureEpoch
	items := []core.Summary{
		{Ref: core.Ref{Name: "b-missing"}, CreatedAt: time.Time{}},
		{Ref: core.Ref{Name: "a-missing"}, CreatedAt: time.Time{}},
		{Ref: core.Ref{Name: "old"}, CreatedAt: base.Add(-time.Hour)},
		{Ref: core.Ref{Name: "new"}, CreatedAt: base.Add(-time.Minute)},
	}
	Sort(items, SortTime)
	got := []string{}
	for _, it := range items {
		got = append(got, it.Ref.Name)
	}
	want := []string{"new", "old", "a-missing", "b-missing"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("time sort = %v, want %v", got, want)
		}
	}
}

func TestAgeOrderedHelper(t *testing.T) {
	base := testkit.FixtureEpoch
	t1 := base.Add(-time.Hour)
	t2 := base.Add(-2 * time.Hour)
	items := []core.Summary{
		{Ref: core.Ref{Name: "created-only"}, CreatedAt: base.Add(-30 * time.Minute)},
		{Ref: core.Ref{Name: "started-recent"}, CreatedAt: base.Add(-9 * time.Hour), StartedAt: &t1},
		{Ref: core.Ref{Name: "started-old"}, CreatedAt: base.Add(-time.Minute), StartedAt: &t2},
		{Ref: core.Ref{Name: "nothing"}},
	}
	// StartedAt wins over CreatedAt when present: started-old (2h ago) is
	// newer activity than created-only? No — created-only was created
	// 30m before the epoch while started-old started 2h before: the AGE
	// column shows created-only (30m) as newest real activity, then
	// started-recent (1h), then started-old (2h), missing last.
	AgeOrdered(items)
	got := []string{}
	for _, it := range items {
		got = append(got, it.Ref.Name)
	}
	want := []string{"created-only", "started-recent", "started-old", "nothing"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AgeOrdered = %v, want %v", got, want)
		}
	}
}

func TestNameMatchesCaseInsensitive(t *testing.T) {
	if !nameMatches("My-Workflow", "workflow") {
		t.Fatal("case-insensitive substring failed")
	}
	if nameMatches("other", "workflow") {
		t.Fatal("substring matched wrong row")
	}
	if !nameMatches("anything", "") {
		t.Fatal("empty query must match everything")
	}
}
