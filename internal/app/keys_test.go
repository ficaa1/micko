package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/actions"
)

// logsRoot is a root on the logs route of one workflow.
func logsRoot(t *testing.T) *Root {
	t.Helper()
	wf := workflowFixture("wf-1")
	m := testRoot(t, fixtureReader(wf))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"})
	return m
}

// detailRoot is the demo list with the selected workflow opened.
func detailRoot(t *testing.T) *Root {
	t.Helper()
	m := resize(t, loadDemoList(t), 120, 30)
	return openFromList(t, m, m.listView.SelectedRef().Name, key("enter"))
}

// q quits while browsing; ctrl+c quits everywhere.
func TestQuitKeys(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *Root
		key   string
	}{
		{"q on the list", loadDemoList, "q"},
		{"q on the detail", detailRoot, "q"},
		{"q on the logs", logsRoot, "q"},
		{"ctrl+c on the list", loadDemoList, "ctrl+c"},
		{"ctrl+c in the list search", func(t *testing.T) *Root { m := loadDemoList(t); keys(m, "/"); return m }, "ctrl+c"},
		{"ctrl+c in the help overlay", func(t *testing.T) *Root { m := loadDemoList(t); keys(m, "?"); return m }, "ctrl+c"},
		{"ctrl+c in the palette", func(t *testing.T) *Root { m, _ := demoRoot(t); keys(m, ":"); return m }, "ctrl+c"},
		{"ctrl+c in the action menu", func(t *testing.T) *Root {
			m := testRoot(t, fixtureReader())
			m.route = RouteDetail
			m.actionView = actions.NewWithOptions(core.Ref{Name: "wf", Namespace: "ns", UID: "uid"}, actions.Options{AllowActions: true})
			m.actionView.OpenMenu()
			return m
		}, "ctrl+c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.setup(t)
			if cmd := keys(m, c.key); !m.quitting || cmd == nil {
				t.Fatalf("quitting=%v cmd=%v", m.quitting, cmd != nil)
			}
		})
	}
}

// A focused text input takes every printable key.
func TestTextEntryOwnsPrintableKeys(t *testing.T) {
	const typed = "q?r0fyPa:"
	cases := []struct {
		name  string
		setup func(t *testing.T) *Root
		open  string
		// shown is how the input echoes typed on screen.
		shown string
	}{
		{"list search", loadDemoList, "/", "Search: " + typed},
		{"logs search", logsRoot, "/", "search: " + typed + "_"},
		{"palette", func(t *testing.T) *Root { m, _ := demoRoot(t); return m }, ":", ": " + typed + "_"},
		{"namespace picker", func(t *testing.T) *Root {
			m := newRoot(fixtureReader(workflowFixture("wf")), "ns", 0)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			return m
		}, "n", typed},
		{"profile picker", func(t *testing.T) *Root {
			conn := &fakeConnector{}
			m := profileRoot(t, conn)
			m.Adopt(mustConnect(t, conn, "dev"))
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			return m
		}, "P", typed},
		{"node find", openDetailNodes, "/", "find " + typed},
		{"cron filter", func(t *testing.T) *Root { m, _ := cronRoot(t); return m }, "/", "Search: " + typed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.setup(t)
			typeKeys(m, c.open)
			route, gen := m.route, m.connGen
			if !m.textEntryActive() && !m.paletteOpen() && !m.namespaceDialogOpen() && !m.profileDialogOpen() {
				t.Fatalf("%q opened no input", c.open)
			}
			deliver(m, typeKeys(m, typed))
			switch {
			case m.quitting:
				t.Fatal("q quit")
			case m.help.IsOpen():
				t.Fatal("? opened help")
			case m.rawMode:
				t.Fatal("f entered the raw view")
			case m.route != route || m.connGen != gen || m.deps.allNamespaces:
				t.Fatalf("a letter acted: route %v→%v, generation %d→%d", route, m.route, gen, m.connGen)
			case m.actionView != nil && m.actionView.State() != actions.StateIdle:
				t.Fatal("a opened the action menu")
			case c.open != ":" && m.paletteOpen():
				t.Fatal(": opened the palette")
			}
			if s := screen(m); !strings.Contains(s, c.shown) {
				t.Fatalf("the input does not show %q:\n%s", c.shown, s)
			}
		})
	}
}

// ? opens help on every route without moving the frame; it owns the keys until closed.
func TestHelpOverlay(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T) *Root
		close string
	}{
		{"list, esc", loadDemoList, "esc"},
		{"list, q", loadDemoList, "q"},
		{"detail", detailRoot, "esc"},
		{"logs", logsRoot, "esc"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := resize(t, c.setup(t), 100, 24)
			route, sel := m.route, m.listView.SelectedRef()
			before := viewLines(m)
			if strings.Contains(screen(m), "KEYS") {
				t.Fatal("help starts open")
			}
			keys(m, "?")
			after := viewLines(m)
			if !strings.Contains(strings.Join(after, "\n"), "KEYS") {
				t.Fatalf("? did not open help:\n%s", screen(m))
			}
			if len(after) != len(before) || after[0] != before[0] {
				t.Fatalf("help moved the frame:\n%q\n%q", before[0], after[0])
			}
			keys(m, "j")
			if m.listView.SelectedRef() != sel {
				t.Fatal("j moved the selection behind the overlay")
			}
			keys(m, c.close)
			if m.quitting || m.help.IsOpen() || strings.Contains(screen(m), "KEYS") {
				t.Fatalf("%s: quitting=%v open=%v", c.close, m.quitting, m.help.IsOpen())
			}
			if m.route != route {
				t.Fatalf("closing help moved the route to %v", m.route)
			}
		})
	}
}

// Help fits 80×40 without cutting a line.
func TestHelpFitsAnEightyByFortyTerminal(t *testing.T) {
	m := resize(t, loadDemoList(t), 80, 40)
	keys(m, "?")
	s := screen(m)
	for _, want := range []string{"KEYS", "Timeline", "1-9 section", "T / X / E open on the timeline / explanation / events", "Explain   why it ended", "Events    live Kubernetes events", "y confirms"} {
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

// The first esc clears a find or filter; the second leaves.
func TestEscClearsBeforeLeaving(t *testing.T) {
	for _, c := range []struct {
		name   string
		setup  func(t *testing.T) *Root
		active func(m *Root) bool
		from   Route
		to     Route
	}{
		{"node find being typed", func(t *testing.T) *Root {
			m := openDetailNodes(t)
			typeKeys(m, "/build")
			return m
		}, func(m *Root) bool { return m.textEntryActive() }, RouteDetail, RouteList},
		{"committed node find", func(t *testing.T) *Root {
			m := openDetailNodes(t)
			typeKeys(m, "/build")
			keys(m, "enter")
			return m
		}, func(m *Root) bool { return strings.Contains(screen(m), "match 1/1") }, RouteDetail, RouteList},
		{"log filter", func(t *testing.T) *Root {
			m, _, _ := demoLogsRoot(t)
			typeKeys(m, "/ERR")
			keys(m, "enter", "&")
			return m
		}, func(m *Root) bool { return m.logsView.EscapeClears() && !m.textEntryActive() }, RouteLogs, RouteList},
		{"filter on a cron drill-down", func(t *testing.T) *Root {
			m, _ := cronRoot(t)
			deliver(m, keys(m, "enter"))
			typeKeys(m, "/179")
			deliver(m, keys(m, "enter"))
			return m
		}, func(m *Root) bool { return m.listView.Query() != "" }, RouteList, RouteCron},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.setup(t)
			if !c.active(m) || m.route != c.from {
				t.Fatalf("precondition: active %v on route %v", c.active(m), m.route)
			}
			deliver(m, keys(m, "esc"))
			if c.active(m) || m.route != c.from {
				t.Fatalf("first esc: active %v, route %v", c.active(m), m.route)
			}
			deliver(m, keys(m, "esc"))
			if m.route != c.to {
				t.Fatalf("second esc: route %v, want %v", m.route, c.to)
			}
		})
	}
}
