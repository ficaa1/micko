package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// perchedFrame is base() with Micko on, on a terminal large enough for him.
func perchedFrame() Frame {
	f := base()
	f.Width, f.Height = 80, 40
	f.Micko = true
	return f
}

// Micko is opt-in: a frame that does not ask for him keeps his rows as
// content however large the terminal.
func TestMickoIsOffUnlessAskedFor(t *testing.T) {
	f := perchedFrame()
	f.Micko = false
	ls := lines(f.Render(plain()))
	if top := topRow(t, ls); top != 1 {
		t.Fatalf("top border on row %d, want 1", top)
	}
	if strings.Contains(strings.Join(ls, "\n"), "^v^v^v") {
		t.Fatal("Micko drawn without being asked for")
	}
	if got, want := f.BodyHeight(), perchedFrame().BodyHeight()+len(shared.MickoPerch.Lines)-1; got != want {
		t.Fatalf("BodyHeight() = %d without Micko, want %d", got, want)
	}
}

// topRow is the index of the pane's top border.
func topRow(t *testing.T, ls []string) int {
	t.Helper()
	for i, l := range ls {
		if strings.HasPrefix(l, "┌") {
			return i
		}
	}
	t.Fatalf("no top border:\n%s", strings.Join(ls, "\n"))
	return -1
}

// Micko sits above the border with his feet and beak on it, and the frame
// is still exactly the terminal.
func TestMickoPerchesOnTheBorder(t *testing.T) {
	f := perchedFrame()
	ls := lines(f.Render(plain()))
	if len(ls) != f.Height {
		t.Fatalf("%d lines, want %d", len(ls), f.Height)
	}
	for i, l := range ls {
		if w := ansi.StringWidth(l); w != f.Width {
			t.Fatalf("line %d is %d cells: %q", i, w, l)
		}
	}
	top := topRow(t, ls)
	if top != 1+len(shared.MickoPerch.Lines)-1 {
		t.Fatalf("top border on row %d, want below the header and Micko's rows:\n%s", top, strings.Join(ls[:top+1], "\n"))
	}
	for _, want := range []string{"_.-~~~~-._", "^v^v^v", "o )"} {
		if !strings.Contains(strings.Join(ls[1:top], "\n"), want) {
			t.Errorf("Micko's rows lack %q:\n%s", want, strings.Join(ls[:top+1], "\n"))
		}
	}
	for _, want := range []string{"/_/", `\v/`, "Workflows", "5 collected"} {
		if !strings.Contains(ls[top], want) {
			t.Errorf("border lacks %q: %q", want, ls[top])
		}
	}
}

// A terminal too small to spare his rows keeps them as content: the header
// sits right above the border.
func TestMickoStaysOffSmallTerminals(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{80, 39}, {79, 40}, {120, 24}} {
		f := perchedFrame()
		f.Width, f.Height = tc.w, tc.h
		ls := lines(f.Render(plain()))
		if top := topRow(t, ls); top != 1 {
			t.Errorf("%dx%d: top border on row %d, want 1", tc.w, tc.h, top)
		}
		if strings.Contains(strings.Join(ls, "\n"), "^v^v^v") {
			t.Errorf("%dx%d: Micko drawn on a small terminal", tc.w, tc.h)
		}
	}
}

// Micko never covers the title or the count. A long count moves him left;
// one that leaves no room makes him step off, but his rows stay, so the
// pane does not move.
func TestMickoMakesWayForTheTitleAndCount(t *testing.T) {
	f := perchedFrame()
	short := lines(f.Render(plain()))
	for _, right := range []string{
		"list: 1234 workflows, 12 running, 3 failed",
		"list: STALE (last good 19 workflows): dial tcp 127.0.0.1:50422: connect: connection refused",
	} {
		f.TitleRight = right
		ls := lines(f.Render(plain()))
		top := topRow(t, ls)
		if top != topRow(t, short) {
			t.Fatalf("count %q moved the border to row %d", right, top)
		}
		if !strings.Contains(ls[top], "Workflows") {
			t.Fatalf("title covered: %q", ls[top])
		}
		// The count is either whole or shortened with an ellipsis; either
		// way its start is intact.
		if !strings.Contains(ls[top], right[:12]) {
			t.Fatalf("count covered: %q", ls[top])
		}
		if strings.Contains(ls[top], `\v/`) {
			// Perched: he sits wholly between the title and the count.
			at := strings.Index(ls[top], `\v/`)
			if at > strings.Index(ls[top], right[:12]) {
				t.Fatalf("Micko is past the count: %q", ls[top])
			}
		}
	}
}

// Colour is decoration: in every skin Micko is the same characters in the
// same cells as in the plain frame. (A skin's header margins and border
// corners differ from plain, so only his rows and cells are compared.)
func TestMickoThemeOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	f := perchedFrame()
	want := lines(f.Render(plain()))
	top := topRow(t, want)
	for _, name := range []string{"default", "nord", "catppuccin-latte"} {
		th, err := shared.SkinTheme(name, false)
		if err != nil {
			t.Fatal(err)
		}
		got := lines(ansi.Strip(f.Render(th)))
		for row := 1; row < top; row++ {
			if got[row] != want[row] {
				t.Errorf("%s row %d: %q, want %q", name, row, got[row], want[row])
			}
		}
		for _, part := range []string{"/_/", `\v/`} {
			if strings.Index(got[top], part) != strings.Index(want[top], part) {
				t.Errorf("%s: %q moved on the border:\n%s\n%s", name, part, got[top], want[top])
			}
		}
	}
}
