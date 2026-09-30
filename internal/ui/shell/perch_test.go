package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// perchedFrame is base() with Mićko on, on a terminal large enough for him.
func perchedFrame() Frame {
	f := base()
	f.Width, f.Height = 80, 40
	f.Mascot = true
	return f
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

// Mićko sits above the pane with his feet and beak drawn onto its border.
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
		t.Fatalf("top border on row %d, want below the header and Mićko's rows:\n%s", top, strings.Join(ls[:top+1], "\n"))
	}
	for _, want := range []string{"_.-~~~~-._", "^v^v^v", "o )"} {
		if !strings.Contains(strings.Join(ls[1:top], "\n"), want) {
			t.Errorf("Mićko's rows lack %q:\n%s", want, strings.Join(ls[:top+1], "\n"))
		}
	}
	for _, want := range []string{"/_/", `\v/`, "Workflows", "5 collected"} {
		if !strings.Contains(ls[top], want) {
			t.Errorf("border lacks %q: %q", want, ls[top])
		}
	}
}

// Mićko shifts left or steps off to make room for the title and count without moving the pane.
func TestMickoMakesWayForTheTitleAndCount(t *testing.T) {
	f := perchedFrame()
	short := lines(f.Render(plain()))
	for _, c := range []struct {
		name, right string
		perched     bool
	}{
		{"moves left", "list: 1234 workflows, 12 running, 3 failed", true},
		{"steps off", "list: STALE (last good 19 workflows): dial tcp 127.0.0.1:50422: connect: connection refused", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			right := c.right
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
			if got := strings.Contains(ls[top], `\v/`); got != c.perched {
				t.Fatalf("beak visible = %v, want %v: %q", got, c.perched, ls[top])
			}
			if c.perched {
				if at, before := strings.Index(ls[top], "/_/"), strings.Index(short[topRow(t, short)], "/_/"); at >= before {
					t.Fatalf("feet column = %d, want left of %d", at, before)
				}
				// Perched: he sits wholly between the title and the count.
				at := strings.Index(ls[top], `\v/`)
				if at > strings.Index(ls[top], right[:12]) {
					t.Fatalf("Mićko is past the count: %q", ls[top])
				}
			}
		})
	}
}

// Skins preserve the mascot characters and their positions.
func TestMickoThemeOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	f := perchedFrame()
	want := lines(f.Render(plain()))
	top := topRow(t, want)
	for _, name := range []string{"default", "nord", "catppuccin-latte"} {
		th := skinned(t, name)
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

// A raised head restores the border under the beak while the feet stay in place.
func TestMickoLiftsHisHeadOffTheBorder(t *testing.T) {
	f := perchedFrame()
	rest := lines(f.Render(plain()))
	top := topRow(t, rest)
	for _, pose := range []shared.Art{shared.MickoPerchLeft, shared.MickoPerchRight} {
		f.MascotPose = pose
		ls := lines(f.Render(plain()))
		if got := topRow(t, ls); got != top {
			t.Fatalf("border moved to row %d, want %d", got, top)
		}
		if strings.Contains(ls[top], `\v/`) {
			t.Errorf("beak still on the border: %q", ls[top])
		}
		if strings.Index(ls[top], "/_/") != strings.Index(rest[top], "/_/") {
			t.Errorf("feet moved:\n%s\n%s", rest[top], ls[top])
		}
		beak := strings.Index(rest[top], `\v/`)
		if got := []rune(ls[top])[len([]rune(rest[top][:beak]))]; got != '─' {
			t.Errorf("border under his beak is %q, want line: %q", got, ls[top])
		}
		for _, l := range ls {
			if w := ansi.StringWidth(l); w != f.Width {
				t.Fatalf("line is %d cells: %q", w, l)
			}
		}
	}
}

// Mascot rows are reserved only when requested and both terminal dimensions meet the minimum.
func TestMascotActivation(t *testing.T) {
	for _, c := range []struct {
		name          string
		w, h          int
		enabled, fits bool
	}{
		{"disabled", 80, 40, false, true},
		{"height below minimum", 80, 39, true, false},
		{"width below minimum", 79, 40, true, false},
		{"wide short terminal", 120, 24, true, false},
		{"minimum", 80, 40, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := PerchFits(c.w, c.h); got != c.fits {
				t.Fatalf("PerchFits(%d,%d) = %v, want %v", c.w, c.h, got, c.fits)
			}
			for _, floor := range []bool{false, true} {
				f := base()
				f.Width, f.Height, f.Mascot, f.MascotFloor = c.w, c.h, c.enabled, floor
				out := f.Render(plain())
				active := c.enabled && c.fits
				marker := "^v^v^v"
				if floor {
					marker = "╭─╮"
				}
				if got := strings.Contains(out, marker); got != active {
					t.Fatalf("floor=%v: mascot visible = %v, want %v", floor, got, active)
				}
				want := c.h - 4
				top := 1
				if active {
					want -= 3
					if floor {
						want--
					}
					if !floor {
						top = 4
					}
				}
				if got := f.BodyHeight(); got != want {
					t.Fatalf("floor=%v: BodyHeight() = %d, want %d", floor, got, want)
				}
				if got := topRow(t, lines(out)); got != top {
					t.Fatalf("floor=%v: top border row = %d, want %d", floor, got, top)
				}
			}
		})
	}
}
