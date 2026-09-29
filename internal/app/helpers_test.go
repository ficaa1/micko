package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
)

// newRoot is a read-only root over r, on the fixture clock, in namespace ns.
func newRoot(r core.Reader, ns string, interval time.Duration) *Root {
	return NewRoot(r, testkit.NewFakeClock(testkit.FixtureEpoch), ns, interval, actions.Options{ReadOnly: true})
}

// armedRoot is a root over r in namespace "ns" with actions enabled.
func armedRoot(r core.Reader, opts actions.Options) *Root {
	opts.AllowActions = true
	return NewRoot(r, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, opts)
}

// testRoot is a read-only root over f in namespace "ns".
func testRoot(t *testing.T, f *testkit.FakeReader) *Root {
	t.Helper()
	return newRoot(f, "ns", time.Second)
}

// workflowFixture is a running workflow in namespace "ns" with one root node.
func workflowFixture(name string) core.Workflow {
	wf := testkit.SyntheticWorkflow("ns", name, "Running", testkit.FixtureEpoch)
	wf.Nodes["root"] = core.Node{ID: "root", Name: name, DisplayName: name, Type: "Steps", Phase: "Running"}
	return wf
}

// fixtureReader is a fake reader holding the given workflows.
func fixtureReader(wfs ...core.Workflow) *testkit.FakeReader {
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	for _, wf := range wfs {
		f.Workflows[wf.Summary.Ref] = wf
	}
	return f
}

// workflowsIn counts the fake's workflows in namespace ns.
func workflowsIn(f *testkit.FakeReader, ns string) int {
	n := 0
	for ref := range f.Workflows {
		if ref.Namespace == ns {
			n++
		}
	}
	return n
}

// runCmd runs cmd, and every command of a batch, and collects the messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, runCmd(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

// deliver runs cmd and feeds its messages back until none are left, dropping ticks and mascot beats.
func deliver(m *Root, cmd tea.Cmd) {
	queue := runCmd(cmd)
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		switch msg.(type) {
		case tickMsg, mascotBeatMsg:
			continue
		}
		_, next := m.Update(msg)
		queue = append(queue, runCmd(next)...)
	}
}

// settle delivers only detail fetches and explain reads, so no tick or watch runs.
func settle(m *Root, cmd tea.Cmd) {
	for i := 0; i < 6 && cmd != nil; i++ {
		var cmds []tea.Cmd
		for _, msg := range runCmd(cmd) {
			switch msg.(type) {
			case detailLoadedMsg, explainLogMsg, OpenWorkflowMsg:
				_, c := m.Update(msg)
				cmds = append(cmds, c)
			}
		}
		cmd = tea.Batch(cmds...)
	}
}

// key is the key press for a name: space, esc, enter, tab, ctrl+c or one character.
func key(k string) tea.KeyPressMsg {
	switch k {
	case "space", " ":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

// keys presses each named key and returns the command of the last one.
func keys(m *Root, ks ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range ks {
		_, cmd = m.Update(key(k))
	}
	return cmd
}

// typeKeys types s one character at a time and returns every command.
func typeKeys(m *Root, s string) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range s {
		_, cmd := m.Update(key(string(r)))
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// pressKey sends a named, non-printable key.
func pressKey(m *Root, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return cmd
}

// runLine runs line in the palette.
func runLine(m *Root, line string) {
	deliver(m, typeKeys(m, ":"))
	typeKeys(m, line)
	deliver(m, pressKey(m, tea.KeyEnter))
}

// screen is the rendered frame without styling.
func screen(m *Root) string { return ansi.Strip(m.View().Content) }

// viewLines is the rendered frame as terminal lines, without styling.
func viewLines(m *Root) []string {
	return strings.Split(strings.TrimRight(screen(m), "\n"), "\n")
}

// resize delivers a WindowSizeMsg the way the runtime does.
func resize(t *testing.T, m *Root, w, h int) *Root {
	t.Helper()
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// loadDemoList is a demo root with the "demo" list loaded.
func loadDemoList(t *testing.T) *Root {
	t.Helper()
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m := newRoot(f, "demo", time.Second)
	for _, msg := range runCmd(m.startListGeneration()) {
		m.Update(msg)
	}
	if len(m.listState.items) != workflowsIn(f, "demo") {
		t.Fatalf("precondition: list not loaded (%d items)", len(m.listState.items))
	}
	return m
}

// demoRoot is a 140×40 demo root with the "demo" list loaded and a millisecond poll.
func demoRoot(t *testing.T) (*Root, *testkit.FakeReader) {
	t.Helper()
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m := newRoot(f, "demo", time.Millisecond)
	m.mascotSleep = func(time.Duration) {}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	deliver(m, m.startListGeneration())
	return m, f
}

// openFromList presses k on the named workflow and settles the open.
func openFromList(t *testing.T, m *Root, name string, k tea.KeyPressMsg) *Root {
	t.Helper()
	for i := 0; i < 20 && m.listView.SelectedRef().Name != name; i++ {
		typeKeys(m, "j")
	}
	if got := m.listView.SelectedRef().Name; got != name {
		t.Fatalf("precondition: cursor on %q, want %q", got, name)
	}
	_, cmd := m.Update(k)
	settle(m, cmd)
	if m.route != RouteDetail || m.detailState.loading {
		t.Fatalf("precondition: route = %v loading %v, want a loaded detail", m.route, m.detailState.loading)
	}
	return m
}

// openDetailNodes opens the first demo workflow on its nodes tab.
func openDetailNodes(t *testing.T) *Root {
	t.Helper()
	m := resize(t, loadDemoList(t), 120, 30)
	m = openFromList(t, m, m.listView.SelectedRef().Name, key("enter"))
	keys(m, "tab")
	return m
}

// waitFor polls cond until it holds or a second passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
