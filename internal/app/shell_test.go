package app

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// screen is the rendered frame as a reader sees it: the text with the
// styling removed. The root draws in the default skin, which styles a key
// apart from its description, so "? help" is only contiguous on screen.
func screen(m *Root) string { return ansi.Strip(m.View().Content) }

// viewLines returns the rendered frame as terminal lines, without styling.
func viewLines(m *Root) []string {
	return strings.Split(strings.TrimRight(screen(m), "\n"), "\n")
}

// resize delivers a WindowSizeMsg the way the runtime does.
func resize(t *testing.T, m *Root, w, h int) *Root {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(*Root)
}

// TestFrameNeverExceedsTerminalHeight is the root-level half of the clamp.
// The alternate screen has no scrollback, so a frame taller than the terminal
// silently loses its bottom — which is exactly where the key hints live.
func TestFrameNeverExceedsTerminalHeight(t *testing.T) {
	m := loadDemoList(t)
	for _, h := range []int{9, 14, 24, 40} {
		m = resize(t, m, 100, h)
		if got := len(viewLines(m)); got > h {
			t.Fatalf("height %d: frame is %d lines:\n%s", h, got, screen(m))
		}
	}
}

// TestListFooterSurvivesShortTerminal: the footer is the only place the key
// hints appear, so it must render even when the terminal is very short.
func TestListFooterSurvivesShortTerminal(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 9)
	if v := screen(m); !strings.Contains(v, "? help") {
		t.Fatalf("key hints lost on a 9-row terminal:\n%s", v)
	}
}

// TestManyWorkflowsStillFit pins a namespace with far more workflows than
// there are rows on screen.
func TestManyWorkflowsStillFit(t *testing.T) {
	m := loadDemoList(t)
	items := make([]core.Summary, 0, 60)
	for i, wf := range testkit.FixtureWorkflowList("demo", 60) {
		s := wf.Summary
		s.Ref.UID = "uid-" + itoa(i)
		items = append(items, s)
	}
	m.listState.items = items
	m.listView.SetItems(items, testkit.FixtureEpoch)
	m = resize(t, m, 120, 20)
	if got := len(viewLines(m)); got > 20 {
		t.Fatalf("60 workflows on a 20-row terminal rendered %d lines:\n%s", got, screen(m))
	}
	if v := screen(m); !strings.Contains(v, "? help") {
		t.Fatalf("footer lost with 60 workflows:\n%s", v)
	}
}

// --- help overlay ------------------------------------------------------------

// TestHelpKeyOpensOverlay: `?` is advertised in the footer, so it must work.
func TestHelpKeyOpensOverlay(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 30)
	if strings.Contains(screen(m), "KEYS") {
		t.Fatal("help must start closed")
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)
	v := screen(m)
	if !strings.Contains(v, "KEYS") {
		t.Fatalf("? did not open the help overlay:\n%s", v)
	}
	if len(viewLines(m)) > 30 {
		t.Fatalf("help overlay overflowed the terminal:\n%s", v)
	}

	// Esc closes it and returns to the list.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if strings.Contains(screen(m), "KEYS") {
		t.Fatalf("Esc did not close the help overlay:\n%s", screen(m))
	}
	if m.route != RouteList {
		t.Fatalf("closing help changed the route to %v", m.route)
	}
}

// TestHelpOverlayOwnsInput: while help is open it is modal, so navigation
// keys must not move the selection behind it.
func TestHelpOverlayOwnsInput(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 30)
	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)

	before := m.listView.SelectedRef()
	next, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m = next.(*Root)
	if m.listView.SelectedRef() != before {
		t.Fatal("j moved the selection while the help overlay was open")
	}

	// q closes the overlay rather than quitting (KeyCtxDialog, keys.go).
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(*Root)
	if m.quitting {
		t.Fatal("q must close the help overlay, not quit the program")
	}
	if strings.Contains(screen(m), "KEYS") {
		t.Fatal("q did not close the help overlay")
	}
	_ = cmd
}

// TestCtrlCQuitsThroughHelpOverlay: Ctrl-C is the one global escape hatch and
// must work even while a modal owns input.
func TestCtrlCQuitsThroughHelpOverlay(t *testing.T) {
	m := loadDemoList(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = next.(*Root)
	if !m.quitting {
		t.Fatal("ctrl+c must quit even with the help overlay open")
	}
	if cmd == nil {
		t.Fatal("ctrl+c must return the quit command")
	}
}

// TestHelpKeyDoesNotTypeIntoSearch: `?` is a printable character, so while
// the search input has focus it must reach the buffer, not open help.
func TestHelpKeyDoesNotTypeIntoSearch(t *testing.T) {
	m := loadDemoList(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(*Root)
	if !m.listView.SearchOn {
		t.Fatal("precondition: / did not focus the search input")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)
	if strings.Contains(screen(m), "KEYS") {
		t.Fatal("? opened help while typing a search query")
	}
	if m.listView.SearchValue() != "?" {
		t.Fatalf("? did not reach the search buffer, got %q", m.listView.SearchValue())
	}
}

// Printable global keys belong to the focused logs editor. They must not open
// help or quit the application while a search query is being typed.
func TestPrintableGlobalKeysReachLogsSearch(t *testing.T) {
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	wf := workflowFixture("wf-1")
	f.Workflows[wf.Summary.Ref] = wf
	m := testRoot(t, f)

	next, _ := m.Update(OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"})
	m = next.(*Root)
	next, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(*Root)
	if m.logsView == nil || !m.logsView.EscapeConsumed() {
		t.Fatal("precondition: / did not focus the logs search input")
	}

	for _, key := range []rune{'?', 'q'} {
		next, _ = m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		m = next.(*Root)
	}
	if m.help.IsOpen() {
		t.Fatal("? opened help while typing a logs search query")
	}
	if m.quitting {
		t.Fatal("q quit while typing a logs search query")
	}
	if got := m.logsView.View(); !strings.Contains(got, "search: ?q_") {
		t.Fatalf("printable keys did not reach logs search buffer:\n%s", got)
	}
}

// TestHelpOpensOnEveryRoute: help is global, so it must open from detail and
// logs too, and closing it must restore the route it was opened from.
func TestHelpOpensOnEveryRoute(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 30)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(*Root)
	for _, msg := range runCmd(cmd) {
		next, cmd = m.Update(msg)
		m = next.(*Root)
	}
	if m.route != RouteDetail {
		t.Fatalf("precondition: route = %v, want detail", m.route)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)
	if !strings.Contains(screen(m), "KEYS") {
		t.Fatalf("? did not open help on the detail route:\n%s", screen(m))
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteDetail {
		t.Fatalf("closing help left the detail route (now %v)", m.route)
	}
}

var _ = time.Second
