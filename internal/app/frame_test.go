package app

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The alternate screen is repainted, not scrolled. A frame that is not
// exactly the terminal size leaves stale rows from the previous frame.
func TestFrameFillsTheWholeWindow(t *testing.T) {
	m := loadDemoList(t)
	for _, tc := range []struct{ w, h int }{{100, 30}, {80, 24}, {120, 12}, {64, 10}} {
		m = resize(t, m, tc.w, tc.h)
		got := viewLines(m)
		if len(got) != tc.h {
			t.Fatalf("%dx%d: %d lines, want %d:\n%s", tc.w, tc.h, len(got), tc.h, screen(m))
		}
		for i, l := range got {
			if w := ansi.StringWidth(l); w != tc.w {
				t.Fatalf("%dx%d line %d is %d cells: %q", tc.w, tc.h, i, w, l)
			}
		}
	}
}

// Pane geometry must not move between routes. A reader who presses enter
// should find the footer and the border in the same rows.
func TestPaneGeometryIsStableAcrossRoutes(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 24)
	listRows := borderRows(viewLines(m))

	for _, route := range []Route{RouteDetail, RouteLogs} {
		m.route = route
		if got := borderRows(viewLines(m)); got != listRows {
			t.Fatalf("border moved on route %v: list %q, got %q", route, listRows, got)
		}
	}
}

// borderRows reports which line indexes carry a horizontal pane border.
func borderRows(lines []string) string {
	var out []string
	for i, l := range lines {
		if strings.HasPrefix(l, "┌") || strings.HasPrefix(l, "└") {
			out = append(out, itoa(i))
		}
	}
	return strings.Join(out, ",")
}

// The header band names the server, the namespace and the safety mode. The
// mode must be visible on every route: it says whether mutations are
// possible at all.
func TestContextBandIsPersistentOnEveryRoute(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 24)
	for _, route := range []Route{RouteList, RouteDetail, RouteLogs} {
		m.route = route
		head := viewLines(m)[0]
		for _, want := range []string{"demo", "READ ONLY"} {
			if !strings.Contains(head, want) {
				t.Fatalf("route %v header lost %q: %q", route, want, head)
			}
		}
	}
}

// The footer band must advertise help on every route, because the overlay
// is the only place the key contract is written down.
func TestFooterAdvertisesHelpOnEveryRoute(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 24)
	for _, route := range []Route{RouteList, RouteDetail, RouteLogs} {
		m.route = route
		lines := viewLines(m)
		if last := lines[len(lines)-1]; !strings.Contains(last, "? help") {
			t.Fatalf("route %v footer lost the help hint: %q", route, last)
		}
	}
}

// Help replaces the pane body, not the frame. If the bands moved, opening
// help would look like the application restarted.
func TestHelpOverlayKeepsTheFrameGeometry(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 100, 24)
	before := viewLines(m)

	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next.(*Root)
	after := viewLines(m)

	if len(before) != len(after) {
		t.Fatalf("help changed the frame height: %d then %d", len(before), len(after))
	}
	if before[0] != after[0] {
		t.Fatalf("help changed the context band:\n%q\n%q", before[0], after[0])
	}
	if !strings.Contains(strings.Join(after, "\n"), "KEYS") {
		t.Fatal("help body missing")
	}
}

// Below the border width the frame degrades to plain bands. It must not
// draw a border it has no room for, and it must not lose the hints.
func TestNarrowTerminalDegradesWithoutLosingChrome(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 50, 14)
	out := screen(m)
	if strings.Contains(out, "┌") {
		t.Fatalf("border drawn at 50 columns:\n%s", out)
	}
	lines := viewLines(m)
	if len(lines) != 14 {
		t.Fatalf("narrow frame is %d lines, want 14", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "help") {
		t.Fatalf("narrow footer lost the help hint: %q", lines[len(lines)-1])
	}
}

// A resize must reach the children. A pane still sized for the old terminal
// either overflows the border or leaves a gap inside it.
func TestResizePropagatesToTheActivePane(t *testing.T) {
	m := loadDemoList(t)
	m = resize(t, m, 120, 40)
	tall := len(viewLines(m))
	m = resize(t, m, 120, 12)
	short := len(viewLines(m))
	if tall != 40 || short != 12 {
		t.Fatalf("resize not honoured: %d then %d", tall, short)
	}
}
