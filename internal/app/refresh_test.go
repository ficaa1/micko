package app

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// The tick re-arms off the list route.
func TestTheTickSurvivesLeavingTheListRoute(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}

	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	if m.route != RouteDetail {
		t.Fatalf("route = %v, want detail", m.route)
	}
	m.detailState.loading = false

	_, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("a tick on the detail route produced no command: the poll chain is dead")
	}
	var rearmed bool
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(tickMsg); ok {
			rearmed = true
		}
	}
	if !rearmed {
		t.Fatal("the tick was not re-armed off the list route")
	}
}

// Only one tick chain is ever armed.
func TestTheTickChainNeverForks(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.route = RouteDetail

	first := m.armTick()
	if first == nil {
		t.Fatal("the first arm produced no tick")
	}
	if second := m.armTick(); second != nil {
		t.Fatal("a second tick was armed while one was already scheduled")
	}
	m.Update(tickMsg{})
	if !m.tickArmed {
		t.Fatal("handling a tick must re-arm exactly one replacement")
	}
}

func TestLeavingDetailRefreshesTheList(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	m.listState.loading = false

	cmd := m.back()
	if m.route != RouteList {
		t.Fatalf("route = %v, want list", m.route)
	}
	if cmd == nil {
		t.Fatal("returning to the list started no refresh")
	}
	if !m.listState.loading {
		t.Fatal("the list refresh was not started")
	}
}

// r refetches the open workflow.
func TestRRefreshesTheOpenWorkflow(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	m.detailState.loading = false

	cmd := keys(m, "r")
	if cmd == nil {
		t.Fatal("r on the detail route produced no refetch")
	}
	var got bool
	for _, msg := range runCmd(cmd) {
		if d, ok := msg.(detailLoadedMsg); ok && d.Workflow.Summary.Ref.Name == "wf-1" {
			got = true
		}
	}
	if !got {
		t.Fatal("r did not fetch the open workflow")
	}
}

// Off the list, a tick recollects the snapshot only while it is stale.
func TestATickRecollectsOnlyAStaleSnapshotOffTheList(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		wf := workflowFixture("wf-1")
		m := testRoot(t, fixtureReader(wf))
		m.listState.items = []core.Summary{wf.Summary}
		m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
		m.detailState.loading = false
		m.connectionFresh = fresh

		m.Update(tickMsg{})
		if m.listState.loading == fresh {
			t.Fatalf("fresh %v: recollected %v", fresh, m.listState.loading)
		}
	}
}

// While the watch is live, the tick polls what the watch covers only once a
// minute: the list, and an open workflow that is in the list's snapshot.
// Without a live watch it polls on every tick.
func TestALiveWatchSlowsThePoll(t *testing.T) {
	wf := workflowFixture("wf-1")
	other := workflowFixture("wf-2")
	cases := []struct {
		name     string
		open     *core.Workflow
		live     bool
		after    time.Duration
		wantPoll bool
	}{
		{"list, live watch", nil, true, 5 * time.Second, false},
		{"list, live watch, a minute on", nil, true, time.Minute, true},
		{"list, no watch", nil, false, 5 * time.Second, true},
		{"detail, live watch", &wf, true, 5 * time.Second, false},
		{"detail, live watch, a minute on", &wf, true, time.Minute, true},
		{"detail outside the snapshot", &other, true, 5 * time.Second, true},
		{"detail, no watch", &wf, false, 5 * time.Second, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader(wf, other))
			clock := m.deps.clock.(*testkit.FakeClock)
			m.listState.items = []core.Summary{wf.Summary}
			purpose := "list"
			m.startListGeneration()
			m.cancelInflight("list")
			m.listState.loading = false
			if c.open != nil {
				purpose = "detail"
				m.Update(OpenWorkflowMsg{Ref: c.open.Summary.Ref})
				m.cancelInflight("detail")
				m.detailState.loading = false
			}
			if c.live {
				m.watchMode = "watch"
				m.setInflight("watch", uint64(m.watchAttempt), func() {})
			}
			clock.Advance(c.after)
			m.Update(tickMsg{})
			if got := m.hasInflight(purpose); got != c.wantPoll {
				t.Errorf("%s polled = %v, want %v", purpose, got, c.wantPoll)
			}
		})
	}
}
