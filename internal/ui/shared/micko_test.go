package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every mask names a part only under a character, and the perch masks every
// character it has: a stray mask cell would colour a blank, and an unmasked
// one would draw in the terminal's colours inside Mićko.
func TestMickoMasksMatchTheirDrawings(t *testing.T) {
	for name, a := range map[string]Art{"perch": MickoPerch, "wordmark": MickoWordmark} {
		if len(a.Lines) != len(a.Mask) {
			t.Fatalf("%s: %d lines, %d mask rows", name, len(a.Lines), len(a.Mask))
		}
		for row, l := range a.Lines {
			line, mask := []rune(l), []rune(a.Mask[row])
			if len(mask) > len(line) {
				t.Errorf("%s row %d: mask is longer than the line", name, row)
			}
			for col, r := range line {
				masked := col < len(mask) && mask[col] != ' '
				if masked && r == ' ' {
					t.Errorf("%s row %d col %d: mask on a blank", name, row, col)
				}
				if name == "perch" && !masked && r != ' ' {
					t.Errorf("%s row %d col %d: %q is not masked", name, row, col, r)
				}
				if masked && !strings.ContainsRune("rbkw", mask[col]) {
					t.Errorf("%s row %d col %d: unknown part %q", name, row, col, mask[col])
				}
			}
		}
	}
}

// The perch is drawn at one column for all its rows, so they must be the
// same width or his head would drift from his body.
func TestMickoPerchRowsShareAWidth(t *testing.T) {
	w := MickoPerch.Width()
	for row, l := range MickoPerch.Lines {
		if n := len([]rune(l)); n != w {
			t.Errorf("row %d is %d cells, want %d: %q", row, n, w, l)
		}
	}
}

// A themed row is the plain row coloured, with no character added or lost.
func TestMickoRenderRowOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := SkinTheme("gruvbox-dark", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Art{MickoPerch, MickoWordmark} {
		for row, l := range a.Lines {
			got := a.RenderRow(th, row, th.Title)
			if ansi.Strip(got) != l {
				t.Errorf("row %d: %q, want %q", row, ansi.Strip(got), l)
			}
			if a.RenderRow(plainTheme(), row, plainTheme().Title) != l {
				t.Errorf("row %d: plain render is not the line", row)
			}
		}
	}
}

// The wordmark signs the help overlay only with Mićko turned on, and only
// when the whole of it fits below the keys: never on the 80x40 body the keys
// are written for, never cut.
func TestHelpWordmarkOnlyWhenItFitsWhole(t *testing.T) {
	var h HelpOverlay
	h.Toggle()
	room := len(helpLines()) + 1 + len(MickoWordmark.Lines)
	if strings.Contains(h.View(100, room), "_ __ ___") {
		t.Error("wordmark drawn with Mićko off")
	}
	h.SetMascot(true)
	if strings.Contains(h.View(helpFitWidth, helpFitHeight), "_ __ ___") {
		t.Error("wordmark drawn on the 80x40 body, where the keys need the room")
	}
	if strings.Contains(h.View(0, 0), "_ __ ___") {
		t.Error("wordmark drawn on an unsized overlay")
	}
	v := h.View(100, room)
	for _, l := range MickoWordmark.Lines {
		if !strings.Contains(v, l) {
			t.Fatalf("wordmark row %q missing at height %d:\n%s", l, room, v)
		}
	}
	if got := len(strings.Split(v, "\n")); got != room {
		t.Fatalf("overlay is %d lines, want %d", got, room)
	}
	if strings.Contains(h.View(100, room-1), "_ __ ___") || strings.Contains(h.View(MickoWordmark.Width()-1, room), "_ __ ___") {
		t.Error("wordmark drawn without room for all of it")
	}
}
