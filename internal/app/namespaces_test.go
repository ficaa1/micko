package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/namespaces"
)

func nsRoot(t *testing.T, extra ...string) (*Root, *testkit.FakeReader) {
	t.Helper()
	wf := workflowFixture("wf")
	f := &testkit.FakeReader{
		Workflows:  map[core.Ref]core.Workflow{wf.Summary.Ref: wf},
		Namespaces: extra,
	}
	m := NewRoot(f, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second)
	return m, f
}

// n opens the picker on the list and the fetch fills it in.
func TestNamespaceKeyOpensThePickerAndLoadsNames(t *testing.T) {
	m, _ := nsRoot(t, "other-ns")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !m.namespaceDialogOpen() {
		t.Fatal("n did not open the namespace picker")
	}
	if cmd == nil {
		t.Fatal("the picker opened without asking the server")
	}
	msg, ok := cmd().(namespacesLoadedMsg)
	if !ok {
		t.Fatalf("fetch message = %T", cmd())
	}
	m.Update(msg)
	got := strings.Join(m.nsView.Names(), ",")
	if !strings.Contains(got, "other-ns") || !strings.Contains(got, "ns") {
		t.Fatalf("names = %q, want both namespaces", got)
	}
}

// The picker owns printable keys while it is open. Without that, q would quit
// and ? would open help in the middle of typing a namespace.
func TestPickerOwnsPrintableKeys(t *testing.T) {
	m, _ := nsRoot(t)
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	for _, r := range "q?" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.quitting {
		t.Fatal("q quit while the namespace picker had the keyboard")
	}
	if m.help.IsOpen() {
		t.Fatal("? opened help while the namespace picker had the keyboard")
	}
	if !m.namespaceDialogOpen() {
		t.Fatal("the picker closed itself")
	}
}

// A switch is a connection-generation change: every stale reply is discarded
// and the session starts again on the list.
func TestSwitchNamespaceStartsAConnectionGeneration(t *testing.T) {
	m, _ := nsRoot(t, "other-ns")
	wf := workflowFixture("wf")
	m.handleListLoaded(listLoadedMsg{genStamp: genStamp{}, Page: core.Page{Items: []core.Summary{wf.Summary}}})
	m.openWorkflow(wf.Summary.Ref)
	before := m.connGen

	cmd := m.switchNamespace("other-ns")
	if cmd == nil {
		t.Fatal("the switch did not start a list collection")
	}
	if m.connGen == before {
		t.Fatal("the switch did not bump the connection generation")
	}
	if m.deps.namespace != "other-ns" {
		t.Fatalf("namespace = %q", m.deps.namespace)
	}
	if m.route != RouteList {
		t.Fatal("the switch left the reader on the old namespace's workflow")
	}
	if len(m.listState.items) != 0 {
		t.Fatal("the old namespace's workflows survived the switch")
	}

	// A reply that was already in flight belongs to the old namespace.
	m.handleListLoaded(listLoadedMsg{genStamp: genStamp{Conn: before}, Page: core.Page{Items: []core.Summary{wf.Summary}}})
	if len(m.listState.items) != 0 {
		t.Fatal("a reply from the old generation was accepted")
	}
}

// Switching to the namespace already in use must change nothing.
func TestSwitchToTheSameNamespaceIsInert(t *testing.T) {
	m, _ := nsRoot(t)
	if cmd := m.switchNamespace("ns"); cmd != nil {
		t.Fatal("re-selecting the current namespace restarted the session")
	}
}

// The picker's intent reaches the switch through the update loop.
func TestSwitchMessageIsRouted(t *testing.T) {
	m, _ := nsRoot(t, "other-ns")
	_, cmd := m.Update(namespaces.SwitchMsg{Namespace: "other-ns"})
	if cmd == nil {
		t.Fatal("the switch message did not start a list collection")
	}
	if m.deps.namespace != "other-ns" {
		t.Fatalf("namespace = %q", m.deps.namespace)
	}
}
