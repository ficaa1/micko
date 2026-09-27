package app

import (
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// The poll tick refetches the open workflow only while no fetch is running.
// A reply that arrives after its own request was superseded still ends that
// request, so it has to release the flag. Leaving it set froze the detail
// view for the rest of the session: the phase stopped moving and `r` was
// the only way to see anything new.
func TestAStaleDetailReplyDoesNotStallTheRefresh(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
	if !m.detailState.loading {
		t.Fatal("opening a workflow must start a fetch")
	}

	// Opening the logs for a node bumps the selection generation while the
	// detail fetch is still running, which is what makes the reply stale.
	stale := detailLoadedMsg{
		genStamp:  genStamp{Conn: m.connGen, Sel: m.selGen},
		RequestID: m.ids.last(),
		Ref:       wf.Summary.Ref,
		Workflow:  wf,
	}
	m.selGen++
	m.Update(stale)

	if m.detailState.loading {
		t.Fatal("a stale reply left the detail view marked as loading")
	}
	_, cmd := m.Update(tickMsg{})
	var refetched bool
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(detailLoadedMsg); ok {
			refetched = true
		}
	}
	if !refetched {
		t.Fatal("the tick did not refetch the open workflow")
	}
}

// A canceled reply ends its request too. Without this the same stall
// happens whenever a fetch is replaced while it is still running.
func TestACanceledDetailReplyDoesNotStallTheRefresh(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})

	m.Update(detailLoadedMsg{
		genStamp:  genStamp{Conn: m.connGen, Sel: m.selGen},
		RequestID: m.ids.last(),
		Ref:       wf.Summary.Ref,
		Canceled:  true,
	})
	if m.detailState.loading {
		t.Fatal("a canceled reply left the detail view marked as loading")
	}
}

// Two fetches of the same kind overlap whenever a key starts one while the
// poll tick still has one running. The older reply must not retire the
// newer request's cancel function: quitting, leaving the view and starting
// the next fetch all cancel through that entry, so losing it leaks a
// request nothing can stop.
func TestALateReplyKeepsTheNewerRequestCancelable(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.selection = wf.Summary.Ref

	m.startDetailFetch()
	first := m.ids.last()
	m.startDetailFetch()
	second := m.ids.last()
	if first == second {
		t.Fatal("two fetches shared one request id")
	}

	m.Update(detailLoadedMsg{
		genStamp:  genStamp{Conn: m.connGen, Sel: m.selGen},
		RequestID: first,
		Ref:       wf.Summary.Ref,
		Workflow:  wf,
	})

	if !m.hasInflight("detail") {
		t.Fatal("the older reply retired the newer request's cancel function")
	}
}
