package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// T opens the Timeline section; enter reopens the last section shown.
func TestListTOpensTheTimeline(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openFromList(t, m, "demo-release-gate", tea.KeyPressMsg{Code: 'T', Text: "T"})
	if got := m.detailView.Section(); got != "timeline" {
		t.Fatalf("section = %q, want timeline", got)
	}
	s := screen(m)
	for _, want := range []string{"[Timeline]", "critical path", "approve-production", "now"} {
		if !strings.Contains(s, want) {
			t.Errorf("timeline screen lacks %q:\n%s", want, s)
		}
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	typeKeys(m, "1")
	if m.route != RouteList {
		t.Fatalf("1 on the list changed the route to %v", m.route)
	}
	m = openFromList(t, m, "demo-nightly-report", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.detailView.Section(); got != "timeline" {
		t.Fatalf("enter opened section %q, want the timeline the pane last showed", got)
	}
}

// Digits and T switch sections, except while typing a find.
func TestDetailSectionKeys(t *testing.T) {
	m := openDetailNodes(t)
	steps := []struct {
		key  rune
		want string
	}{
		{'T', "timeline"}, {'1', "summary"}, {'6', "resource"}, {'2', "nodes"},
		{'9', "nodes"}, {'3', "timeline"},
	}
	for _, s := range steps {
		typeKeys(m, string(s.key))
		if got := m.detailView.Section(); got != s.want {
			t.Fatalf("after %q: section %q, want %q", s.key, got, s.want)
		}
	}

	typeKeys(m, "2")
	typeKeys(m, "/")
	for _, r := range "T13" {
		typeKeys(m, string(r))
	}
	if got := m.detailView.Section(); got != "nodes" {
		t.Fatalf("typing into the find moved to section %q", got)
	}
	if s := screen(m); !strings.Contains(s, "find T13") {
		t.Fatalf("the typed name is not on screen:\n%s", s)
	}
}

// enter on a timeline pod row opens its log; y copies the pod name.
func TestTimelineRowOpensItsLogs(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openFromList(t, m, "demo-release-gate", tea.KeyPressMsg{Code: 'T', Text: "T"})
	typeKeys(m, "j") // build
	row, ok := m.detailView.SelectedNode()
	if !ok || row.DisplayName != "build" || row.PodName == "" {
		t.Fatalf("precondition: selected %+v", row)
	}
	if got := m.copyText(); got != row.PodName {
		t.Errorf("y copies %q, want the pod name %q", got, row.PodName)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(*Root)
	for _, msg := range runCmd(cmd) {
		next, _ = m.Update(msg)
		m = next.(*Root)
	}
	if m.route != RouteLogs || m.logsView == nil {
		t.Fatalf("enter on a timeline pod row: route %v", m.route)
	}
	if !strings.Contains(screen(m), row.PodName) {
		t.Errorf("the log pane does not name pod %q:\n%s", row.PodName, screen(m))
	}
}
