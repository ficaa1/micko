package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// flooredFrame is perchedFrame() with Mićko on the floor instead.
func flooredFrame() Frame {
	f := perchedFrame()
	f.MascotFloor = true
	return f
}

// bottomRow is the index of the pane's bottom border.
func bottomRow(t *testing.T, ls []string) int {
	t.Helper()
	for i, l := range ls {
		if strings.HasPrefix(l, "└") {
			return i
		}
	}
	t.Fatalf("no bottom border:\n%s", strings.Join(ls, "\n"))
	return -1
}

// On the floor Mićko sits in the pane's last rows with his feet and his
// mirror's stand on the bottom border. The pane stays where it is with him
// off, the frame is still exactly the terminal, and he takes his rows from
// the content, not from under it.
func TestMickoSitsOnTheFloor(t *testing.T) {
	f := flooredFrame()
	off := perchedFrame()
	off.Mascot = false
	ls, want := lines(f.Render(plain())), lines(off.Render(plain()))
	if len(ls) != f.Height {
		t.Fatalf("%d lines, want %d", len(ls), f.Height)
	}
	for i, l := range ls {
		if w := ansi.StringWidth(l); w != f.Width {
			t.Fatalf("line %d is %d cells: %q", i, w, l)
		}
	}
	top, bottom := topRow(t, ls), bottomRow(t, ls)
	if top != topRow(t, want) || bottom != bottomRow(t, want) {
		t.Fatalf("borders on rows %d and %d, want %d and %d", top, bottom, topRow(t, want), bottomRow(t, want))
	}
	if got, wantH := f.BodyHeight(), off.BodyHeight()-len(shared.MickoFloor.Lines)+1; got != wantH {
		t.Fatalf("BodyHeight() = %d on the floor, want %d", got, wantH)
	}
	corner := strings.Join(ls[bottom-3:bottom], "\n")
	for _, part := range []string{".--.", "<(", "(^v^v^)", "╭─╮", "│░│", "╰┬╯"} {
		if !strings.Contains(corner, part) {
			t.Errorf("his corner lacks %q:\n%s", part, corner)
		}
	}
	for _, part := range []string{"^-^", "====", "┴"} {
		if !strings.Contains(ls[bottom], part) {
			t.Errorf("bottom border lacks %q: %q", part, ls[bottom])
		}
	}
	// The mirror ends at the pane's content edge: one padding cell, then
	// the right border.
	if !strings.HasSuffix(ls[bottom-2], "│░│ │") {
		t.Errorf("mirror is not against the right edge: %q", ls[bottom-2])
	}
	if strings.Contains(strings.Join(ls[:top], "\n"), "^v^v^v") {
		t.Error("perched as well as on the floor")
	}
}

// Every floor pose keeps the frame whole and the borders put, and the one
// at the mirror kisses it.
func TestMickoFloorPosesKeepTheFrame(t *testing.T) {
	f := flooredFrame()
	rest := lines(f.Render(plain()))
	kissed := false
	for i, b := range shared.MickoFloorBeats {
		f.MascotPose = b.Pose
		ls := lines(f.Render(plain()))
		if topRow(t, ls) != topRow(t, rest) || bottomRow(t, ls) != bottomRow(t, rest) {
			t.Fatalf("beat %d moved a border", i)
		}
		for _, l := range ls {
			if w := ansi.StringWidth(l); w != f.Width {
				t.Fatalf("beat %d: line is %d cells: %q", i, w, l)
			}
		}
		kissed = kissed || strings.Contains(strings.Join(ls, "\n"), "│<│")
	}
	if !kissed {
		t.Error("no beat kisses the mirror")
	}
}

// Off the floor, or on a terminal too small, the pane keeps its rows.
func TestMickoStaysOffSmallFloors(t *testing.T) {
	f := flooredFrame()
	f.Width, f.Height = 120, 24
	if strings.Contains(f.Render(plain()), "╭─╮") {
		t.Error("Mićko on the floor of a small terminal")
	}
	full := f
	full.Mascot = false
	if f.BodyHeight() != full.BodyHeight() {
		t.Errorf("BodyHeight() = %d on a small terminal, want %d", f.BodyHeight(), full.BodyHeight())
	}
}
