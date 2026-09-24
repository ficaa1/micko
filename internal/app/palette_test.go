package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/palette"
)

// demoRoot is a root on the demo dataset with the "demo" namespace's list
// loaded. The poll interval is a millisecond so a delivered command that arms
// the tick returns at once; deliver drops the tick itself.
func demoRoot(t *testing.T) (*Root, *testkit.FakeReader) {
	t.Helper()
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m := NewRoot(f, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	deliver(m, m.startListGeneration())
	return m, f
}

// deliver runs cmd and feeds its messages back into the root, the way the
// program loop would, until nothing is left. Poll ticks are dropped: each one
// starts another list, and the chain would never end.
func deliver(m *Root, cmd tea.Cmd) {
	queue := runCmd(cmd)
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		if _, ok := msg.(tickMsg); ok {
			continue
		}
		_, next := m.Update(msg)
		queue = append(queue, runCmd(next)...)
	}
}

func typeKeys(m *Root, s string) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range s {
		msg := tea.KeyPressMsg{Code: r, Text: string(r)}
		if r == ' ' {
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		}
		_, cmd := m.Update(msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func pressKey(m *Root, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return cmd
}

// runLine opens the palette, types line and presses enter, delivering every
// message that follows.
func runLine(m *Root, line string) {
	deliver(m, typeKeys(m, ":"))
	typeKeys(m, line)
	deliver(m, pressKey(m, tea.KeyEnter))
}

// While the palette is open q and ? are letters, esc closes it, and ctrl+c
// still quits.
func TestPaletteKeyIsolation(t *testing.T) {
	m, _ := demoRoot(t)
	typeKeys(m, ":")
	if !m.paletteOpen() {
		t.Fatal(": did not open the palette")
	}
	typeKeys(m, "q?")
	if m.quitting || m.help.IsOpen() {
		t.Fatalf("q or ? acted as a command while typing (quitting=%v help=%v)", m.quitting, m.help.IsOpen())
	}
	if m.palView.Value() != "q?" {
		t.Fatalf("palette holds %q, want q?", m.palView.Value())
	}
	pressKey(m, tea.KeyEscape)
	if m.paletteOpen() {
		t.Fatal("esc left the palette open")
	}
	if m.route != RouteList {
		t.Fatalf("esc in the palette moved the route to %v", m.route)
	}
	typeKeys(m, ":")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quitting || cmd == nil {
		t.Fatal("ctrl+c did not quit while the palette was open")
	}
}

// The palette is not offered inside text entry: there `:` is a character.
func TestColonTypesIntoTheSearch(t *testing.T) {
	m, _ := demoRoot(t)
	typeKeys(m, "/:")
	if m.paletteOpen() {
		t.Fatal(": opened the palette inside the list search")
	}
	if got := m.listView.SearchValue(); got != ":" {
		t.Fatalf("search holds %q, want :", got)
	}
}

// While open, the palette sits on top of the pane: the input, the suggestions
// and the list below, with the palette's own hints in the footer.
func TestPaletteRendersAboveThePane(t *testing.T) {
	m, _ := demoRoot(t)
	typeKeys(m, ":w")
	v := ansi.Strip(m.View().Content)
	lines := strings.Split(v, "\n")
	if len(lines) != 40 {
		t.Fatalf("frame is %d lines, want the terminal's 40", len(lines))
	}
	if !strings.Contains(lines[2], ": w_") || !strings.Contains(lines[3], "› workflows") {
		t.Fatalf("palette not at the top of the pane:\n%s", strings.Join(lines[:6], "\n"))
	}
	if !strings.Contains(v, "demo-release-gate") {
		t.Fatal("the list under the palette disappeared")
	}
	if !strings.Contains(lines[len(lines)-1], "tab complete") {
		t.Fatalf("footer = %q, want the palette hints", lines[len(lines)-1])
	}
}

// `wf` returns to the workflow list from any route and stops the detail
// fetch it leaves.
func TestCommandWorkflowsLeavesDetail(t *testing.T) {
	m, _ := demoRoot(t)
	ref := m.listView.SelectedRef()
	m.openWorkflow(ref)
	if m.route != RouteDetail || !m.hasInflight("detail") {
		t.Fatal("precondition: detail open with its fetch running")
	}
	runLine(m, "wf")
	if m.route != RouteList {
		t.Fatalf("route = %v, want the list", m.route)
	}
	if m.hasInflight("detail") {
		t.Fatal("the detail fetch survived leaving the detail route")
	}
}

// `ns` with no argument opens the picker; with one it switches, which is a
// connection generation like the picker's.
func TestCommandNamespace(t *testing.T) {
	m, _ := demoRoot(t)
	runLine(m, "ns")
	if !m.namespaceDialogOpen() {
		t.Fatal("ns with no argument did not open the picker")
	}
	pressKey(m, tea.KeyEscape)

	before := m.connGen
	runLine(m, "ns demo-ml")
	if m.deps.namespace != "demo-ml" || m.connGen != before+1 {
		t.Fatalf("namespace = %q gen %d→%d, want demo-ml and one generation", m.deps.namespace, before, m.connGen)
	}
	if n := len(m.listState.items); n != 2 {
		t.Fatalf("demo-ml list = %d workflows, want 2", n)
	}

	runLine(m, "ns demo-ml")
	if !strings.Contains(m.flash, "already in namespace demo-ml") {
		t.Fatalf("flash = %q, want it to say nothing changed", m.flash)
	}
	runLine(m, "ns a b")
	if !strings.Contains(m.flash, "one namespace") || m.deps.namespace != "demo-ml" {
		t.Fatalf("a two-word argument switched or went unreported: ns=%q flash=%q", m.deps.namespace, m.flash)
	}
}

// `ns` completes from the configured names and the ones the server reports.
// Opening the palette asks for the latter.
func TestNamespaceCompletionUsesSeedAndDiscovered(t *testing.T) {
	m, f := demoRoot(t)
	f.Namespaces = []string{"argo"}
	m.SetNamespaceSeed([]string{"team-a"})
	deliver(m, typeKeys(m, ":"))
	got := strings.Join(m.paletteArgs("ns"), ",")
	if got != "argo,demo,demo-ml,team-a" {
		t.Fatalf("ns candidates = %q", got)
	}
}

// `all` and `0` both toggle the all-namespaces view: a new generation, a list
// with no namespace, the header saying "all", and back again.
func TestAllNamespacesToggle(t *testing.T) {
	m, f := demoRoot(t)
	before := m.connGen
	runLine(m, "all")
	if !m.deps.allNamespaces || m.connGen != before+1 {
		t.Fatalf("all: allNamespaces=%v gen %d→%d", m.deps.allNamespaces, before, m.connGen)
	}
	if n := len(m.listState.items); n != len(f.Workflows) {
		t.Fatalf("all namespaces collected %d workflows, want every one of %d", n, len(f.Workflows))
	}
	if !m.listView.AllNamespaces() {
		t.Fatal("the list was not told it spans namespaces")
	}
	v := m.View().Content
	if !strings.Contains(v, "ns: all") || !strings.Contains(v, "NAMESPACE") || !strings.Contains(v, "in 2 namespaces") {
		t.Fatalf("all-namespaces frame is missing its markers:\n%s", v)
	}

	deliver(m, typeKeys(m, "0"))
	if m.deps.allNamespaces || m.deps.namespace != "demo" {
		t.Fatalf("0 did not return to demo: all=%v ns=%q", m.deps.allNamespaces, m.deps.namespace)
	}
	if n := len(m.listState.items); n != workflowsIn(f, "demo") {
		t.Fatalf("back in demo the list holds %d", n)
	}
	deliver(m, typeKeys(m, "0"))
	if !m.deps.allNamespaces {
		t.Fatal("0 did not enter the all-namespaces view")
	}
}

// Entering the view is a reconnection: a reply that was in flight for the
// single namespace is discarded.
func TestAllNamespacesDiscardsTheOldScope(t *testing.T) {
	m, _ := demoRoot(t)
	stale := runCmd(m.startListGeneration())
	deliver(m, typeKeys(m, "0"))
	n := len(m.listState.items)
	for _, msg := range stale {
		m.Update(msg)
	}
	if len(m.listState.items) != n {
		t.Fatal("a reply from the single-namespace list replaced the all-namespaces snapshot")
	}
}

// The list request and the watch ask for the empty namespace.
func TestAllNamespacesRequestsTheEmptyNamespace(t *testing.T) {
	m, _ := demoRoot(t)
	m.deps.allNamespaces = true
	if ns := m.deps.listNamespace(); ns != "" {
		t.Fatalf("list namespace = %q, want empty", ns)
	}
	w := &recordingWatcher{req: make(chan core.WatchRequest, 1)}
	m.deps.watcher = w
	m.startWatch()
	if got := <-w.req; got.Namespace != "" {
		t.Fatalf("watch namespace = %q, want empty", got.Namespace)
	}
	m.cancelAll()
}

type recordingWatcher struct{ req chan core.WatchRequest }

func (w *recordingWatcher) Watch(ctx context.Context, req core.WatchRequest, _ func(core.WatchEvent) error) error {
	w.req <- req
	<-ctx.Done()
	return ctx.Err()
}

// Detail opened from the all-namespaces list asks for the row's own
// namespace, not the session's.
func TestDetailFromAllNamespacesUsesTheRowNamespace(t *testing.T) {
	m, _ := demoRoot(t)
	deliver(m, typeKeys(m, "0"))
	typeKeys(m, "/ml-batch")
	pressKey(m, tea.KeyEnter)
	sel := m.listView.SelectedRef()
	if sel.Namespace != testkit.DemoMLNamespace {
		t.Fatalf("precondition: selected %+v, want the demo-ml row", sel)
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	if m.route != RouteDetail {
		t.Fatalf("route = %v, want detail", m.route)
	}
	if got := m.detailState.workflow.Summary.Ref; got.Namespace != "demo-ml" || got.Name != "demo-ml-batch-infer" {
		t.Fatalf("detail loaded %+v, want demo-ml/demo-ml-batch-infer", got)
	}
	if m.deps.namespace != "demo" {
		t.Fatalf("opening a row moved the session to %q", m.deps.namespace)
	}
}

// A server that refuses the cluster-wide list shows the refusal on the pane,
// with the way back, instead of an empty list.
func TestAllNamespacesRefusalIsShown(t *testing.T) {
	m, f := demoRoot(t)
	f.ListErr = core.NewAPIError(core.ErrForbidden, 403,
		`Permission denied, you are not allowed to list workflows in namespace "".`)
	deliver(m, typeKeys(m, "0"))
	body := m.View().Content
	for _, want := range []string{"no workflows visible", "list forbidden", "Permission denied", "press 0 to return to demo"} {
		if !strings.Contains(body, want) {
			t.Errorf("pane lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "no workflows in") {
		t.Error("a refused list rendered as an empty one")
	}
}

// Any other failure of a first collection is still an error on the pane.
func TestFirstListFailureIsNotAnEmptyList(t *testing.T) {
	m, f := demoRoot(t)
	f.ListErr = core.NewAPIError(core.ErrUnsupported, 0, "the server manages namespace argo only and cannot list all namespaces")
	deliver(m, typeKeys(m, "0"))
	body := m.View().Content
	if !strings.Contains(body, "no workflows visible: all namespaces: the server manages namespace argo only") {
		t.Fatalf("pane does not state the refusal:\n%s", body)
	}
}

// `profile`/`ctx` opens the picker with no argument and switches to a
// configured profile with one. An unknown name is refused, never guessed.
func TestCommandProfile(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "prod"))
	conn.asked = nil

	runLine(m, "ctx")
	if !m.profileDialogOpen() {
		t.Fatal("ctx with no argument did not open the picker")
	}
	m.profView.Close()

	runLine(m, "profile staging")
	if len(conn.asked) != 0 || !strings.Contains(m.flash, "no profile named staging") {
		t.Fatalf("unknown profile: asked=%v flash=%q", conn.asked, m.flash)
	}
	runLine(m, "ctx prod")
	if len(conn.asked) != 0 || !strings.Contains(m.flash, "already connected") {
		t.Fatalf("current profile: asked=%v flash=%q", conn.asked, m.flash)
	}
	runLine(m, "ctx dev")
	if len(conn.asked) != 1 || conn.asked[0] != "dev" || m.profileCurrent != "dev" {
		t.Fatalf("ctx dev: asked=%v current=%q", conn.asked, m.profileCurrent)
	}
}

// A profile switch starts in the new profile's namespace, whatever view the
// old one was in.
func TestProfileSwitchLeavesAllNamespaces(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "prod"))
	deliver(m, m.toggleAllNamespaces())
	if !m.deps.allNamespaces {
		t.Fatal("precondition: all namespaces on")
	}
	runLine(m, "ctx dev")
	if m.deps.allNamespaces || m.listView.AllNamespaces() {
		t.Fatal("the all-namespaces view survived a profile switch")
	}
}

// The demo has no profiles; the command says so rather than opening an empty
// picker.
func TestCommandProfileWithoutProfiles(t *testing.T) {
	m, _ := demoRoot(t)
	runLine(m, "ctx dev")
	if !strings.Contains(m.flash, "no profiles to switch between") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestCommandHelpAndQuit(t *testing.T) {
	m, _ := demoRoot(t)
	runLine(m, "help")
	if !m.help.IsOpen() {
		t.Fatal("help did not open the overlay")
	}
	pressKey(m, tea.KeyEscape)
	runLine(m, "q")
	if !m.quitting {
		t.Fatal("q did not quit")
	}
}

// An unknown command is reported in the footer and nothing runs.
func TestUnknownCommandIsReported(t *testing.T) {
	m, _ := demoRoot(t)
	gen := m.connGen
	runLine(m, "workflo")
	if !strings.Contains(m.flash, "unknown command: workflo") {
		t.Fatalf("flash = %q", m.flash)
	}
	if m.connGen != gen || m.route != RouteList {
		t.Fatal("an unknown command changed the session")
	}
	if last := strings.Split(m.View().Content, "\n"); !strings.Contains(last[len(last)-1], "unknown command") {
		t.Fatal("the footer does not carry the report")
	}
	runLine(m, "all now")
	if !strings.Contains(m.flash, "all takes no argument") || m.deps.allNamespaces {
		t.Fatalf("argument to all: flash=%q all=%v", m.flash, m.deps.allNamespaces)
	}
}

// Every command in the registry is reachable from the palette by its name and
// every alias, and the registry holds no two commands with the same word.
func TestRegistryWordsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, c := range newRegistry() {
		if c.run == nil {
			t.Errorf("%s has no run", c.Name)
		}
		for _, w := range c.Terms() {
			if other, dup := seen[w]; dup {
				t.Errorf("%q names both %s and %s", w, other, c.Name)
			}
			seen[w] = c.Name
			if got, ok := palette.Find(paletteSpecs(newRegistry()), w); !ok || got.Name != c.Name {
				t.Errorf("%q does not resolve to %s", w, c.Name)
			}
		}
	}
	if !isListRoute(RouteList) || isListRoute(RouteDetail) || isListRoute(RouteLogs) {
		t.Error("only the registered kinds' routes are list routes")
	}
}

// The raw view prefixes each row with its namespace across namespaces.
func TestRawListCarriesTheNamespaceAcrossNamespaces(t *testing.T) {
	m, _ := demoRoot(t)
	deliver(m, typeKeys(m, "0"))
	for _, l := range m.listRawLines() {
		if !strings.HasPrefix(l, "demo\t") && !strings.HasPrefix(l, "demo-ml\t") {
			t.Fatalf("raw line %q has no namespace", l)
		}
	}
}
