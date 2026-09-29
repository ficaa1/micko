package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The alternate screen is repainted, not scrolled: a frame that is not
// exactly the terminal size leaves stale rows from the previous frame, or
// loses its bottom rows, which is where the key hints live. On every route
// and size, many workflows included, the frame fills the window exactly,
// the header names the namespace and the safety mode, and the footer
// advertises help. Below the border width the frame drops the border.
func TestFrameFillsTheWholeWindow(t *testing.T) {
	m := loadDemoList(t)
	var items []core.Summary
	for i, wf := range testkit.FixtureWorkflowList("demo", 60) {
		s := wf.Summary
		s.Ref.UID = "uid-" + strconv.Itoa(i)
		items = append(items, s)
	}
	m.listState.items = items
	m.listView.SetItems(items, testkit.FixtureEpoch)
	for _, route := range []Route{RouteList, RouteDetail, RouteLogs} {
		m.route = route
		for _, tc := range []struct{ w, h int }{{100, 30}, {80, 24}, {120, 12}, {100, 9}, {64, 10}, {50, 14}, {120, 40}} {
			m = resize(t, m, tc.w, tc.h)
			got := viewLines(m)
			if len(got) != tc.h {
				t.Fatalf("%v %dx%d: %d lines, want %d:\n%s", route, tc.w, tc.h, len(got), tc.h, screen(m))
			}
			for i, l := range got {
				if w := ansi.StringWidth(l); w != tc.w {
					t.Fatalf("%v %dx%d line %d is %d cells: %q", route, tc.w, tc.h, i, w, l)
				}
			}
			if tc.w >= 64 {
				for _, want := range []string{"demo", "READ ONLY"} {
					if !strings.Contains(got[0], want) {
						t.Fatalf("%v %dx%d header lost %q: %q", route, tc.w, tc.h, want, got[0])
					}
				}
			}
			if !strings.Contains(got[len(got)-1], "help") {
				t.Fatalf("%v %dx%d footer lost the help hint: %q", route, tc.w, tc.h, got[len(got)-1])
			}
			if border := strings.Contains(screen(m), "┌"); border != (tc.w > 50) {
				t.Fatalf("%v %dx%d: border drawn = %v", route, tc.w, tc.h, border)
			}
		}
	}
}

// A terminal too small for the list says so, and a resize back clears it.
func TestListTooSmallNotice(t *testing.T) {
	m := resize(t, loadDemoList(t), 40, 10)
	if v := screen(m); !strings.Contains(v, "too small") {
		t.Fatalf("40x10 shows no notice:\n%s", v)
	}
	m = resize(t, m, 100, 40)
	if v := screen(m); strings.Contains(v, "too small") {
		t.Fatalf("100x40 still shows the notice:\n%s", v)
	}
}

// Pane geometry must not move between routes. A reader who presses enter
// should find the footer and the border in the same rows. With Mićko on, on
// a terminal large enough for him, he is on every route, perched or on the
// floor.
func TestPaneGeometryIsStableAcrossRoutes(t *testing.T) {
	for _, spot := range []config.Mascot{config.MascotOff, config.MascotPerch, config.MascotFloor} {
		for _, h := range []int{24, 40} {
			m := loadDemoList(t)
			m.SetMascot(spot)
			m = resize(t, m, 100, h)
			listRows := borderRows(viewLines(m))

			for _, route := range []Route{RouteDetail, RouteLogs, RouteCron, RouteArchived} {
				m.route = route
				if got := borderRows(viewLines(m)); got != listRows {
					t.Fatalf("%q 100x%d: border moved on route %v: list %q, got %q", spot, h, route, listRows, got)
				}
			}
		}
	}
}

// borderRows reports which line indexes carry a horizontal pane border.
func borderRows(lines []string) string {
	var out []string
	for i, l := range lines {
		if strings.HasPrefix(l, "┌") || strings.HasPrefix(l, "└") {
			out = append(out, strconv.Itoa(i))
		}
	}
	return strings.Join(out, ",")
}

// Mićko is off until asked for. `:mascot` perches him and says so, a second
// `:mascot` moves him to the floor, a third sends him off, and on a terminal
// too small for him the notice says why he does not show.
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
	floored := func() bool { return strings.Contains(screen(m), "╭─╮") }
	if perched() || !floored() || !strings.Contains(screen(m), "Mićko is on the floor") {
		t.Fatalf("second :mascot did not move him to the floor:\n%s", screen(m))
	}
	// On the floor he takes the pane's own last rows, so the pane is where
	// it was with him off.
	if got := borderRows(viewLines(m)); got != top {
		t.Fatalf("border rows %q with him on the floor, want %q", got, top)
	}

	runLine(m, "mascot")
	if perched() || floored() || !strings.Contains(screen(m), "Mićko flew off") {
		t.Fatalf("third :mascot did not send him off:\n%s", screen(m))
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
	on := m.cycleMascot()
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
	if cmd := m.cycleMascot(); cmd == nil || m.mascot != config.MascotFloor {
		t.Fatal("moving him to the floor started no routine")
	}
	if _, cmd := m.Update(stale); cmd != nil || m.mascotBeat != 0 {
		t.Fatalf("a beat from the perch routine moved him to beat %d", m.mascotBeat)
	}

	// On the floor he plays the floor routine, and gets to his mirror.
	next = m.nextMascotBeat()
	for i := 0; i < len(shared.MickoFloorBeats) && !strings.Contains(screen(m), "│<│"); i++ {
		_, next = m.Update(next())
	}
	if !strings.Contains(screen(m), "│<│") {
		t.Fatalf("a whole routine on the floor went by without a kiss:\n%s", screen(m))
	}

	m.cycleMascot()
	if m.mascot != config.MascotOff {
		t.Fatalf("third cycle left him at %q", m.mascot)
	}
	if cmd := m.nextMascotBeat(); cmd != nil {
		t.Fatal("a routine was scheduled while he is off")
	}
}
