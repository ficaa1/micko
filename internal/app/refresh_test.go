package app

import (
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// The tick carries the only poll chain, so dropping it off the list route
// would stop every refresh for the rest of the session: the reader returns
// to the list and sees a phase from minutes ago, or an age frozen at the
// moment they left.
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

// Two tick chains double the poll rate, and double again on every route
// change. Only one may ever be alive.
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

// An action changes the row the reader is about to look at. Returning to the
// list with the pre-action snapshot reads as "the action did nothing".
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

// r is the manual refresh everywhere else; the detail route had no way to
// ask for current data at all.
func TestRRefreshesTheOpenWorkflow(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	m.detailState.loading = false

	cmd := rawKey(m, "r")
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

// Only an accepted list snapshot clears the stale block on mutations. The
// poll collects one on the list route alone, so a reader whose snapshot went
// stale while they read a workflow would stay blocked for as long as they
// stayed on it.
func TestAStaleSnapshotIsRecollectedOffTheListRoute(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	m.detailState.loading = false
	m.connectionReady = true
	m.connectionFresh = false

	m.Update(tickMsg{})
	if !m.listState.loading {
		t.Fatal("a stale snapshot was not recollected on the detail route: actions stay blocked")
	}
}

// The recollection above runs only while the block is up. A fresh session
// reading one workflow must not pay for a full snapshot on every tick.
func TestAFreshSnapshotIsNotRecollectedOffTheListRoute(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	m.detailState.loading = false
	m.connectionReady = true
	m.connectionFresh = true

	m.Update(tickMsg{})
	if m.listState.loading {
		t.Fatal("a fresh snapshot was recollected off the list route")
	}
}
