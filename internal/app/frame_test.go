package app

import (
	"strings"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
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
// should find the footer and the border in the same rows. With Mićko on, on
// a terminal large enough for his perch, he perches on every route.
func TestPaneGeometryIsStableAcrossRoutes(t *testing.T) {
	for _, h := range []int{24, 40} {
		m := loadDemoList(t)
		m.SetMascot(true)
		m = resize(t, m, 100, h)
		listRows := borderRows(viewLines(m))

		for _, route := range []Route{RouteDetail, RouteLogs, RouteCron, RouteArchived} {
			m.route = route
			if got := borderRows(viewLines(m)); got != listRows {
				t.Fatalf("100x%d: border moved on route %v: list %q, got %q", h, route, listRows, got)
			}
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

// Mićko is off until asked for. `:mascot` perches him and says so, a second
// `:mascot` sends him off, and on a terminal too small for him the notice
// says why he does not show.
func TestMickoCommandTogglesThePerch(t *testing.T) {
	m, _ := demoRoot(t)
	m = resize(t, m, 100, 40)
	perched := func() bool { return strings.Contains(screen(m), "^v^v^v") }
	if perched() {
		t.Fatal("Mićko perched before being turned on")
	}
	top := borderRows(viewLines(m))

	runLine(m, "mascot")
	if !perched() {
		t.Fatalf(":mascot did not perch him:\n%s", screen(m))
	}
	if !strings.Contains(screen(m), "Mićko is perched") {
		t.Fatalf("no notice after :mascot:\n%s", screen(m))
	}
	if borderRows(viewLines(m)) == top {
		t.Fatal("the pane did not make room for him")
	}

	runLine(m, "mascot")
	if perched() || !strings.Contains(screen(m), "Mićko flew off") {
		t.Fatalf("second :mascot did not send him off:\n%s", screen(m))
	}
	if got := borderRows(viewLines(m)); got != top {
		t.Fatalf("border rows %q after he left, want %q", got, top)
	}

	m = resize(t, m, 100, 24)
	runLine(m, "mascot")
	if perched() || !strings.Contains(screen(m), "80x40") {
		t.Fatalf("small terminal: want no perch and a notice naming the size:\n%s", screen(m))
	}
}

// Once on, Mićko moves through his routine one beat at a time, and the
// pose on screen follows. A beat from before he was turned off and on again
// is dropped, so only one routine ever runs.
func TestMickoMovesThroughHisRoutine(t *testing.T) {
	m, _ := demoRoot(t)
	m = resize(t, m, 100, 40)
	on := m.toggleMascot()
	if on == nil {
		t.Fatal("turning him on started no routine")
	}
	beat := on()
	if _, ok := beat.(mascotBeatMsg); !ok {
		t.Fatalf("routine sent %T, want a beat", beat)
	}
	_, next := m.Update(beat)
	if m.mascotBeat != 1 || next == nil {
		t.Fatalf("after one beat: at beat %d, next scheduled %v", m.mascotBeat, next != nil)
	}

	// Walk on until he looks away from the pane.
	for i := 0; i < len(shared.MickoPerchBeats) && !strings.Contains(screen(m), ")>") && !strings.Contains(screen(m), "<("); i++ {
		_, next = m.Update(next())
	}
	if !strings.Contains(screen(m), ")>") && !strings.Contains(screen(m), "<(") {
		t.Fatalf("a whole routine went by without him looking around:\n%s", screen(m))
	}

	stale := next()
	m.toggleMascot()
	if cmd := m.toggleMascot(); cmd == nil {
		t.Fatal("turning him back on started no routine")
	}
	if _, cmd := m.Update(stale); cmd != nil || m.mascotBeat != 0 {
		t.Fatalf("a beat from the old routine moved him to beat %d", m.mascotBeat)
	}
	m.toggleMascot()
	if cmd := m.nextMascotBeat(); cmd != nil {
		t.Fatal("a routine was scheduled while he is off")
	}
}
