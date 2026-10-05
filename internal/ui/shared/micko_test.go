package shared

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Masks cover drawing characters with valid parts and leave blank cells unpainted.
func TestMickoMasksMatchTheirDrawings(t *testing.T) {
	for name, a := range map[string]Art{"perch": MickoPerch, "perch-left": MickoPerchLeft, "perch-right": MickoPerchRight, "floor": MickoFloor, "wordmark": MickoWordmark} {
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
				if name != "wordmark" && !masked && r != ' ' {
					t.Errorf("%s row %d col %d: %q is not masked", name, row, col, r)
				}
				if masked && !strings.ContainsRune("rbkwgh", mask[col]) {
					t.Errorf("%s row %d col %d: unknown part %q", name, row, col, mask[col])
				}
			}
		}
	}
}

// Perched beats keep their row widths, feet and positive hold durations.
func TestMickoPerchPosesShareAShape(t *testing.T) {
	w := MickoPerch.Width()
	last := len(MickoPerch.Lines) - 1
	feet := strings.Index(MickoPerch.Lines[last], "/_/")
	for i, b := range MickoPerchBeats {
		if len(b.Pose.Lines) != len(MickoPerch.Lines) || len(b.Pose.Mask) != len(MickoPerch.Lines) {
			t.Fatalf("beat %d has %d lines and %d mask rows", i, len(b.Pose.Lines), len(b.Pose.Mask))
		}
		for row, l := range b.Pose.Lines {
			if n, m := len([]rune(l)), len([]rune(b.Pose.Mask[row])); n != w || m != w {
				t.Errorf("beat %d row %d is %d cells with a %d-cell mask, want %d: %q", i, row, n, m, w, l)
			}
		}
		if got := strings.Index(b.Pose.Lines[last], "/_/"); got != feet {
			t.Errorf("beat %d: feet at column %d, want %d", i, got, feet)
		}
		if b.Hold <= 0 {
			t.Errorf("beat %d holds for %v", i, b.Hold)
		}
	}
}

// His routine rests his beak on the pane, blinks, and looks both ways.
func TestMickoPerchRoutine(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range MickoPerchBeats {
		head := b.Pose.Lines[1] + b.Pose.Lines[2] + b.Pose.Lines[3]
		switch {
		case strings.Contains(head, `\v/`) && strings.Contains(head, " o "):
			seen["resting"] = true
		case strings.Contains(head, " - "):
			seen["blinking"] = true
		case strings.Contains(head, "<("):
			seen["looking left"] = true
		case strings.Contains(head, ")>"):
			seen["looking right"] = true
		}
	}
	for _, want := range []string{"resting", "blinking", "looking left", "looking right"} {
		if !seen[want] {
			t.Errorf("no beat has him %s", want)
		}
	}
	if MickoPerchBeats[0].Pose.Lines[3] != MickoPerch.Lines[3] {
		t.Error("the routine does not start at rest")
	}
}

// A themed row is the plain row coloured, with no character added or lost.
func TestMickoRenderRowOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th := skinTheme(t, "gruvbox-dark", false)
	for _, a := range []Art{MickoPerch, MickoPerchLeft, MickoPerchRight, MickoFloor, MickoWordmark} {
		for row, l := range a.Lines {
			got := a.RenderRow(th, row, th.Title)
			if ansi.Strip(got) != l {
				t.Errorf("row %d: %q, want %q", row, ansi.Strip(got), l)
			}
			if a.RenderRow(NewTheme(true), row, NewTheme(true).Title) != l {
				t.Errorf("row %d: plain render is not the line", row)
			}
		}
	}
}

// The enabled wordmark appears only when the entire drawing fits below the keys.
func TestHelpWordmarkOnlyWhenItFitsWhole(t *testing.T) {
	var h HelpOverlay
	h.Toggle()
	room := 37
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

// Floor beats keep their shared geometry, resting direction and mirror position.
func TestMickoFloorPosesShareAShape(t *testing.T) {
	for _, c := range []struct {
		row  int
		want string
	}{{2, " <( o  )    "}, {4, " `-^-^-'===="}} {
		if got := MickoFloor.Lines[c.row][:12]; got != c.want {
			t.Errorf("resting floor row %d = %q, want %q", c.row, got, c.want)
		}
	}

	w := MickoFloor.Width()
	for i, b := range MickoFloorBeats {
		if len(b.Pose.Lines) != len(MickoFloor.Lines) || len(b.Pose.Mask) != len(MickoFloor.Lines) {
			t.Fatalf("beat %d has %d lines and %d mask rows", i, len(b.Pose.Lines), len(b.Pose.Mask))
		}
		for row, l := range b.Pose.Lines {
			if n, m := len([]rune(l)), len([]rune(b.Pose.Mask[row])); n != w || m != w {
				t.Errorf("beat %d row %d is %d cells with a %d-cell mask, want %d: %q", i, row, n, m, w, l)
			}
			if got, want := []rune(l)[w-3], []rune(MickoFloor.Lines[row])[w-3]; got != want {
				t.Errorf("beat %d row %d: mirror edge %q, want %q", i, row, got, want)
			}
		}
		if b.Hold <= 0 {
			t.Errorf("beat %d holds for %v", i, b.Hold)
		}
	}
}

// Floor hops lift and tuck the bird, kiss only at the mirror, and return to rest quickly.
func TestMickoHopsToHisMirror(t *testing.T) {
	var lifted, tucked, kissedAway bool
	var quick time.Duration
	for i, b := range MickoFloorBeats {
		for row, l := range b.Pose.Lines {
			for col, r := range []rune(l) {
				if m := []rune(b.Pose.Mask[row]); r != ' ' && m[col] == ' ' || r == ' ' && m[col] != ' ' {
					t.Fatalf("beat %d row %d col %d: %q masked %q", i, row, col, r, m[col])
				}
			}
		}
		air := strings.TrimSpace(b.Pose.Lines[0]) != ""
		lifted = lifted || air
		if air {
			floor := strings.TrimSpace(b.Pose.Lines[len(b.Pose.Lines)-1])
			if floor != "┴" {
				t.Errorf("beat %d: off the floor, but the floor holds %q", i, floor)
			}
			tucked = tucked || strings.Contains(b.Pose.Lines[3], "`-----'") || strings.Contains(b.Pose.Lines[3], "'-----`")
		}
		if strings.Contains(b.Pose.Lines[2], "│<│") && !strings.Contains(b.Pose.Lines[2], ")>│<│") {
			kissedAway = true
		}
		if b.Hold < 100*time.Millisecond {
			quick += b.Hold
		}
	}
	if !lifted || !tucked {
		t.Errorf("no beat has him in the air (lifted %v) with his feet tucked (%v)", lifted, tucked)
	}
	if kissedAway {
		t.Error("he kisses the mirror from across the floor")
	}
	// Four hops, each well under a second in the air.
	if quick <= 0 || quick > 2*time.Second {
		t.Errorf("hops spend %v in quick frames, want some and under 2s", quick)
	}
	first, last := MickoFloorBeats[0].Pose, MickoFloorBeats[len(MickoFloorBeats)-1].Pose
	if !reflect.DeepEqual(first, MickoFloor) || !reflect.DeepEqual(last, MickoFloor) {
		t.Error("the routine does not start and end where he watches you")
	}
}
