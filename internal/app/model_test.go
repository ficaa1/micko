package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// A fetched workflow is applied to the detail route, and one whose UID no
// longer matches (deleted and re-created under the same name) is refused.
func TestDetailLoadAppliesAndValidatesUID(t *testing.T) {
	wf := workflowFixture("wf-1")
	m := testRoot(t, fixtureReader(wf))
	m.Update(OpenWorkflowMsg{Ref: wf.Summary.Ref})

	msgs := runCmd(m.startDetailFetch())
	if len(msgs) != 1 {
		t.Fatalf("msgs = %d", len(msgs))
	}
	dm, ok := msgs[0].(detailLoadedMsg)
	if !ok || dm.Err != nil {
		t.Fatalf("msg = %#v", msgs[0])
	}
	m.Update(dm)
	if m.detailState.workflow.Summary.Ref.Name != "wf-1" {
		t.Fatalf("detail not applied: %+v", m.detailState)
	}

	bad := dm
	bad.Workflow.Summary.Ref.UID = "different-uid"
	m.Update(bad)
	if m.detailState.lastErr == nil {
		t.Fatal("a UID mismatch was applied")
	}
}

// The reply to a workflow the reader has left never reaches the workflow
// they opened next.
func TestALeftWorkflowsReplyIsDropped(t *testing.T) {
	a, b := workflowFixture("wf-a"), workflowFixture("wf-b")
	m := testRoot(t, fixtureReader(a, b))
	_, first := m.Update(OpenWorkflowMsg{Ref: a.Summary.Ref})
	late := runCmd(first)
	m.Update(BackMsg{})
	_, second := m.Update(OpenWorkflowMsg{Ref: b.Summary.Ref})
	for _, msg := range late {
		m.Update(msg)
	}
	if got := m.detailState.workflow.Summary.Ref.Name; got == "wf-a" {
		t.Fatal("the reply for the workflow the reader left was applied")
	}
	for _, msg := range runCmd(second) {
		m.Update(msg)
	}
	if got := m.detailState.workflow.Summary.Ref.Name; got != "wf-b" {
		t.Fatalf("detail shows %q, want wf-b", got)
	}
}

// A list reply stamped for another connection or selection, or one that
// was canceled, is never applied.
func TestListRepliesThatAreDropped(t *testing.T) {
	ghost := []core.Summary{testkit.SyntheticWorkflow("ns", "ghost", "Running", testkit.FixtureEpoch).Summary}
	for _, c := range []struct {
		name string
		msg  func(m *Root) listLoadedMsg
	}{
		{"old connection", func(m *Root) listLoadedMsg {
			return listLoadedMsg{genStamp: genStamp{Conn: m.connGen - 1, Sel: m.selGen}, Page: core.Page{Items: ghost}, Done: true}
		}},
		{"other selection", func(m *Root) listLoadedMsg {
			return listLoadedMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen + 1}, Page: core.Page{Items: ghost}, Done: true}
		}},
		{"canceled", func(m *Root) listLoadedMsg {
			return listLoadedMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, Page: core.Page{Items: ghost}, Done: true, Canceled: true}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader())
			m.Update(c.msg(m))
			if n := len(m.listState.items); n != 0 {
				t.Fatalf("applied %d items", n)
			}
		})
	}
}

// A snapshot spread over several pages is collected whole before it is
// applied.
func TestListSnapshotCollectsEveryPage(t *testing.T) {
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m := newRoot(f, "demo", time.Second)
	m.deps.pageSize = 2
	msgs := runCmd(m.startListGeneration())
	if len(msgs) != 1 {
		t.Fatalf("expected one listLoaded, got %d messages", len(msgs))
	}
	lm, ok := msgs[0].(listLoadedMsg)
	if !ok || lm.Err != nil || !lm.Done {
		t.Fatalf("msg = %#v", msgs[0])
	}
	want := workflowsIn(f, "demo")
	if len(lm.Page.Items) != want {
		t.Fatalf("items = %d, want %d (all pages collected)", len(lm.Page.Items), want)
	}
	m.Update(lm)
	if len(m.listState.items) != want {
		t.Fatalf("applied items = %d", len(m.listState.items))
	}
}

// A failed poll keeps the last good snapshot, records the error and starts
// the stale age.
func TestListErrorKeepsLastGoodData(t *testing.T) {
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m := newRoot(f, "demo", time.Second)
	m.Update(runCmd(m.startListGeneration())[0])
	nGood := len(m.listState.items)

	f.ListErr = core.ErrUnavailablef("connection refused")
	lm := runCmd(m.startListGeneration())[0].(listLoadedMsg)
	if lm.Err == nil {
		t.Fatal("expected an error")
	}
	m.Update(lm)
	if len(m.listState.items) != nGood {
		t.Fatalf("last good data lost: %d != %d", len(m.listState.items), nGood)
	}
	if m.listState.lastErr == nil || m.listState.staleSince.IsZero() {
		t.Fatalf("error %v, stale since %v", m.listState.lastErr, m.listState.staleSince)
	}
}

// Init starts the first list collection.
func TestInitStartsListGeneration(t *testing.T) {
	m := newRoot(testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch)), "demo", time.Second)
	for _, msg := range runCmd(m.Init()) {
		if _, ok := msg.(listLoadedMsg); ok {
			return
		}
	}
	t.Fatal("Init did not collect the list")
}

// j moves the list's selection and enter opens the selected workflow.
func TestListKeysMoveSelectionAndEnterOpensDetail(t *testing.T) {
	m := loadDemoList(t)
	first := m.listView.SelectedRef().Name
	keys(m, "j")
	afterJ := m.listView.SelectedRef().Name
	if afterJ == first {
		t.Fatalf("j did not move the selection (still %q)", first)
	}
	for _, msg := range runCmd(keys(m, "enter")) {
		m.Update(msg)
	}
	if m.route != RouteDetail || m.detailState.ref.Name != afterJ {
		t.Fatalf("enter: route %v, detail %q, want %q", m.route, m.detailState.ref.Name, afterJ)
	}
}

// AGE is measured from the injected clock, not the zero time (which makes
// every AGE a "-").
func TestListAgeRendersUsingInjectedClock(t *testing.T) {
	m := resize(t, loadDemoList(t), 100, 30)
	// demo-data-pull started FixtureEpoch-40m.
	if v := screen(m); !strings.Contains(v, "40m") {
		t.Fatalf("no '40m' AGE:\n%s", v)
	}
}

// A long list error wraps at the terminal width instead of being clipped.
func TestListErrorTextWrapsAtWidth(t *testing.T) {
	m := resize(t, loadDemoList(t), 80, 24)
	long := "server returned an HTML page instead of API data (content-type text/html; charset=UTF-8); this endpoint expects interactive browser login configure a server service-account token do not retry automatically -- tail-marker"
	m.Update(listLoadedMsg{
		genStamp: genStamp{Conn: m.connGen, Sel: m.selGen},
		Done:     true,
		Err:      core.NewAPIError(core.ErrUnauthenticated, 401, long),
	})
	v := screen(m)
	if !strings.Contains(v, "tail-marker") {
		t.Fatalf("the error is clipped:\n%s", v)
	}
	for _, ln := range strings.Split(v, "\n") {
		if ansi.StringWidth(ln) > 80 {
			t.Fatalf("line exceeds 80 columns: %q", ln)
		}
	}
}

// esc unwinds the way the reader came: logs opened from the list return to
// the list, logs opened from a workflow return to it and then to the list.
func TestBackReturnsWhereItCameFrom(t *testing.T) {
	wf := workflowFixture("wf-1")
	for _, c := range []struct {
		name string
		path []tea.Msg
		back []Route
	}{
		{"logs from the list", []tea.Msg{OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"}}, []Route{RouteList}},
		{"logs from the detail", []tea.Msg{OpenWorkflowMsg{Ref: wf.Summary.Ref}, OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"}}, []Route{RouteDetail, RouteList}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader(wf))
			for _, msg := range c.path {
				m.Update(msg)
			}
			if m.route != RouteLogs {
				t.Fatalf("precondition: route %v", m.route)
			}
			for i, want := range c.back {
				m.Update(BackMsg{})
				if m.route != want {
					t.Fatalf("back %d: route %v, want %v", i+1, m.route, want)
				}
			}
			if m.logState.running {
				t.Error("the stream is still marked running")
			}
		})
	}
}
