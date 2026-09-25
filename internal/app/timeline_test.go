package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// openFromList presses key on the list with the cursor on the workflow
// named name and runs the open it starts, keeping only the fetch's answer so
// no poll tick runs.
func openFromList(t *testing.T, m *Root, name string, key tea.KeyPressMsg) *Root {
	t.Helper()
	for i := 0; i < 20 && m.listView.SelectedRef().Name != name; i++ {
		m = typeKey(m, 'j')
	}
	if got := m.listView.SelectedRef().Name; got != name {
		t.Fatalf("precondition: cursor on %q, want %q", got, name)
	}
	next, cmd := m.Update(key)
	m = next.(*Root)
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
	return m
}

// T on the list opens the selected workflow straight on its Timeline
// section, while enter keeps opening it on the section the pane last
// showed.
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
	m = typeKey(m, '1')
	if m.route != RouteList {
		t.Fatalf("1 on the list changed the route to %v", m.route)
	}
	m = openFromList(t, m, "demo-nightly-report", tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.detailView.Section(); got != "timeline" {
		t.Fatalf("enter opened section %q, want the timeline the pane last showed", got)
	}
}

// In the detail pane the digits jump to a section by position and T jumps
// to the timeline, except while the nodes tab's find input is open, where
// both are letters of the name being typed.
func TestDetailSectionKeys(t *testing.T) {
	m := openDetailNodes(t)
	steps := []struct {
		key  rune
		want string
	}{
		{'T', "timeline"}, {'1', "summary"}, {'5', "resource"}, {'2', "nodes"},
		{'9', "nodes"}, {'3', "timeline"},
	}
	for _, s := range steps {
		m = typeKey(m, s.key)
		if got := m.detailView.Section(); got != s.want {
			t.Fatalf("after %q: section %q, want %q", s.key, got, s.want)
		}
	}

	m = typeKey(m, '2')
	m = typeKey(m, '/')
	for _, r := range "T13" {
		m = typeKey(m, r)
	}
	if got := m.detailView.Section(); got != "nodes" {
		t.Fatalf("typing into the find moved to section %q", got)
	}
	if s := screen(m); !strings.Contains(s, "find T13") {
		t.Fatalf("the typed name is not on screen:\n%s", s)
	}
}

// enter on a pod row of the timeline opens that pod's log, as on the nodes
// tab, and y copies the pod name.
func TestTimelineRowOpensItsLogs(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openFromList(t, m, "demo-release-gate", tea.KeyPressMsg{Code: 'T', Text: "T"})
	m = typeKey(m, 'j') // build
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

// The help overlay fits a 40-row, 80-column terminal whole: its last line is
// on screen and no line is cut.
func TestHelpFitsAnEightyByFortyTerminal(t *testing.T) {
	m := resize(t, loadDemoList(t), 80, 40)
	m = typeKey(m, '?')
	s := screen(m)
	for _, want := range []string{"KEYS", "Timeline", "1-9 section", "T open on the timeline", "X open on the explanation", "Explain   why it ended", "y confirms"} {
		if !strings.Contains(s, want) {
			t.Errorf("help lacks %q:\n%s", want, s)
		}
	}
	for _, l := range viewLines(m) {
		if strings.Contains(l, "…") {
			t.Errorf("help line is cut: %q", l)
		}
		if w := ansi.StringWidth(l); w != 80 {
			t.Errorf("line is %d cells: %q", w, l)
		}
	}
}
