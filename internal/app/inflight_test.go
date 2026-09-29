package app

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// A stale or canceled detail reply still ends its fetch.
func TestAnEndedDetailReplyDoesNotStallTheRefresh(t *testing.T) {
	for _, c := range []struct {
		name  string
		reply func(m *Root, wf core.Workflow) detailLoadedMsg
	}{
		{"stale", func(m *Root, wf core.Workflow) detailLoadedMsg {
			msg := detailLoadedMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, RequestID: m.ids.next, Ref: wf.Summary.Ref, Workflow: wf}
			// Opening a node's logs bumps the selection mid-fetch.
			m.selGen++
			return msg
		}},
		{"canceled", func(m *Root, wf core.Workflow) detailLoadedMsg {
			return detailLoadedMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, RequestID: m.ids.next, Ref: wf.Summary.Ref, Canceled: true}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			wf := workflowFixture("wf-1")
			// A millisecond poll: running the tick's command waits it out.
			m := newRoot(fixtureReader(wf), "ns", time.Millisecond)
			m.listState.items = []core.Summary{wf.Summary}
			m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})
			if !m.detailState.loading {
				t.Fatal("opening a workflow must start a fetch")
			}
			m.Update(c.reply(m, wf))
			if m.detailState.loading {
				t.Fatal("the reply left the detail view marked as loading")
			}
			_, cmd := m.Update(tickMsg{})
			for _, msg := range runCmd(cmd) {
				if _, ok := msg.(detailLoadedMsg); ok {
					return
				}
			}
			t.Fatal("the tick did not refetch the open workflow")
		})
	}
}

// A late reply does not retire the newer request's cancel function.
func TestALateReplyKeepsTheNewerRequestCancelable(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.selection = wf.Summary.Ref

	m.startDetailFetch()
	first := m.ids.next
	m.startDetailFetch()
	second := m.ids.next
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
