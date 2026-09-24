package detail

import (
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/testkit"
)

// Tea child model tests: tab switching, session-only reveal lifecycle,
// back intent, and the loading/not-found/error states (UI-04, DET-12 ⛨).

func TestTeaModelTabCycles(t *testing.T) {
	m := New()
	tabs := []string{"summary", "nodes", "timeline", "resource", "summary"}
	for i, want := range tabs[1:] {
		updated, _ := m.Update(tea.KeyPressMsg{Code: 9}) // tab
		mm := updated.(*Model)
		if mm.tab != want {
			t.Fatalf("tab[%d] = %q, want %q", i, mm.tab, want)
		}
		m = mm
	}
}

func TestTeaModelRevealSessionOnly(t *testing.T) {
	m := New()
	wf := resourceFixture()
	m.SetWorkflow(wf, testkit.FixtureEpoch)

	// Reveal toggles on explicitly.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	mm := updated.(*Model)
	if !mm.revealResource {
		t.Fatal("v must enable session reveal")
	}
	// Reveal must reset when a different workflow (new UID) is applied.
	next := resourceFixture()
	next.Summary.Ref.UID = "other-uid"
	mm.SetWorkflow(next, testkit.FixtureEpoch)
	if mm.revealResource {
		t.Fatal("reveal must reset on workflow change (session-only, DET-12)")
	}
	// Applying the SAME workflow again keeps the reveal off (it was reset).
	mm.SetWorkflow(next, testkit.FixtureEpoch)
	if mm.revealResource {
		t.Fatal("reveal stays off after reset")
	}
}

func TestTeaModelBackIntent(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 27}) // esc
	if cmd == nil {
		t.Fatal("esc in detail must emit a back intent command")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatalf("esc intent = %T, want BackMsg", cmd())
	}
}

func TestTeaModelStates(t *testing.T) {
	m := New()
	m.SetLoading()
	if v := m.View().Content; v != "detail: loading..." {
		t.Fatalf("loading view = %q", v)
	}
	m.SetNotFound()
	if v := m.View().Content; v != "workflow no longer available" {
		t.Fatalf("not-found view = %q", v)
	}
	m.SetError("forbidden: RBAC")
	if v := m.View().Content; v != "detail error: forbidden: RBAC" {
		t.Fatalf("error view = %q", v)
	}
}

func TestTeaModelViewAppliesWorkflow(t *testing.T) {
	m := New()
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	m.SetWorkflow(wf, testkit.FixtureEpoch)
	v := m.View().Content
	if !containsSub(v, "fixture-dag") || !containsSub(v, "phase: Failed") {
		t.Fatalf("summary view wrong:\n%s", v)
	}
	// Tab to nodes renders outline rows.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 9})
	mm := updated.(*Model)
	if got := mm.View().Content; !containsSub(got, "task-a") {
		t.Fatalf("nodes tab missing rows:\n%s", got)
	}
}

func TestTeaModelSetSize(t *testing.T) {
	m := New()
	m.SetSize(120, 40)
	if m.width != 120 || m.height != 40 {
		t.Fatalf("size not stored: %dx%d", m.width, m.height)
	}
}

func TestTeaModelInitNil(t *testing.T) {
	if cmd := New().Init(); cmd != nil {
		t.Fatal("detail child must not start autonomous effects (plan §4)")
	}
}
