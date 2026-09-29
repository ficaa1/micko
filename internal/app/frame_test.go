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

// The frame fills the window exactly on every route and size, with the header and help hint.
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

// The pane border stays in the same rows on every route, with or without Mićko.
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

// :mascot cycles Mićko between perched, floor and off.
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

// Mićko steps through one routine at a time; a beat from an earlier routine is dropped.
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
