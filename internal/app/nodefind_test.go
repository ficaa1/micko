package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// openDetailNodes opens the first demo workflow on its nodes tab.
func openDetailNodes(t *testing.T) *Root {
	t.Helper()
	m := resize(t, loadDemoList(t), 120, 30)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(*Root)
	// The open is two steps: the list's intent, then the fetch it starts.
	// The second step keeps only the fetch's answer, so no poll tick runs.
	for step := 0; step < 2; step++ {
		var cmds []tea.Cmd
		for _, msg := range runCmd(cmd) {
			if _, ok := msg.(detailLoadedMsg); !ok && step > 0 {
				continue
			}
			next, c := m.Update(msg)
			m = next.(*Root)
			cmds = append(cmds, c)
		}
		cmd = tea.Batch(cmds...)
	}
	if m.route != RouteDetail || m.detailState.loading {
		t.Fatalf("precondition: route = %v loading %v, want a loaded detail", m.route, m.detailState.loading)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	return next.(*Root)
}

func typeKey(m *Root, r rune) *Root {
	next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return next.(*Root)
}

// The nodes tab's find input is text entry: every global letter the root
// binds (q quit, ? help, f raw, y copy, P profiles, r refresh, a actions)
// types into it instead. esc cancels the input and then clears nothing
// more, so the reader stays on the workflow; the next esc goes back.
func TestNodeFindIsTextEntry(t *testing.T) {
	m := openDetailNodes(t)
	m = typeKey(m, '/')
	if !m.textEntryActive() {
		t.Fatal("/ on the nodes tab did not open text entry")
	}
	for _, r := range "q?fyPra" {
		m = typeKey(m, r)
	}
	switch {
	case m.quitting:
		t.Fatal("q quit while typing a node name")
	case m.help.IsOpen():
		t.Fatal("? opened help while typing a node name")
	case m.rawMode:
		t.Fatal("f entered the raw view while typing a node name")
	case m.actionView != nil && m.actionView.State() != 0:
		t.Fatal("a opened the actions menu while typing a node name")
	case m.route != RouteDetail:
		t.Fatalf("a typed key changed the route to %v", m.route)
	}
	if s := screen(m); !strings.Contains(s, "find q?fyPra") {
		t.Fatalf("the typed name is not on screen:\n%s", s)
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteDetail || m.textEntryActive() {
		t.Fatalf("esc in the find input: route %v, text entry %v", m.route, m.textEntryActive())
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteList {
		t.Fatalf("esc after the find did not go back: route %v", m.route)
	}
}

// A committed find keeps esc for itself: the first esc clears the match,
// the second leaves the workflow.
func TestEscClearsANodeFindBeforeGoingBack(t *testing.T) {
	m := openDetailNodes(t)
	for _, r := range "/build" {
		m = typeKey(m, r)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(*Root)
	if !strings.Contains(screen(m), "match 1/1") {
		t.Fatalf("find did not match:\n%s", screen(m))
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteDetail || strings.Contains(screen(m), "match 1/1") {
		t.Fatalf("first esc: route %v\n%s", m.route, screen(m))
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteList {
		t.Fatalf("second esc: route %v", m.route)
	}
}
