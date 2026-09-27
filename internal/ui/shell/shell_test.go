package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// plain builds a frame with a no-color theme so tests compare visible cells
// rather than escape sequences.
func plain() shared.Theme { return shared.NewTheme(true) }

func lines(s string) []string { return strings.Split(s, "\n") }

// base is a frame wide and tall enough for the bordered layout.
func base() Frame {
	return Frame{
		Width:      80,
		Height:     12,
		App:        "argo-tui 0.2.0-beta.1",
		Server:     "synthetic demo",
		Namespace:  "demo",
		Mode:       "READ ONLY",
		Title:      "Workflows",
		TitleRight: "5 collected",
		Body:       []string{"row one", "row two"},
		Route:      "list",
		Hints:      "/ filter   enter open   ? help",
		Status:     "watch • 1/5",
	}
}

// The frame is the whole terminal window. Any other height means the
// alternate screen keeps stale rows or loses the footer.
func TestRenderFillsExactHeight(t *testing.T) {
	for _, h := range []int{6, 7, 12, 40} {
		f := base()
		f.Height = h
		got := lines(f.Render(plain()))
		if len(got) != h {
			t.Fatalf("height %d: got %d lines, want %d:\n%s", h, len(got), h, f.Render(plain()))
		}
	}
}

// Stable pane geometry: every row is the full terminal width, so the right
// border sits in one column and never wobbles between frames.
func TestRenderFillsExactWidth(t *testing.T) {
	f := base()
	for i, l := range lines(f.Render(plain())) {
		if w := ansi.StringWidth(l); w != f.Width {
			t.Fatalf("line %d width %d, want %d: %q", i, w, f.Width, l)
		}
	}
}

// A short body must not pull the bottom border up. The pane keeps its size
// and the body is padded, so the footer never moves as rows arrive.
func TestShortBodyIsPaddedNotCollapsed(t *testing.T) {
	f := base()
	f.Body = []string{"only row"}
	got := lines(f.Render(plain()))
	if !strings.HasPrefix(got[len(got)-2], "└") {
		t.Fatalf("bottom border not on the second-to-last line:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[len(got)-1], "? help") {
		t.Fatalf("footer missing from the last line: %q", got[len(got)-1])
	}
}

// A body taller than the pane is clipped inside the border. The border and
// the footer are chrome: they outrank content.
func TestOverlongBodyIsClippedInsideTheBorder(t *testing.T) {
	f := base()
	f.Height = 8
	f.Body = make([]string, 50)
	for i := range f.Body {
		f.Body[i] = "row"
	}
	got := lines(f.Render(plain()))
	if len(got) != 8 {
		t.Fatalf("got %d lines, want 8", len(got))
	}
	if !strings.HasPrefix(got[len(got)-2], "└") {
		t.Fatalf("bottom border lost:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[len(got)-1], "? help") {
		t.Fatal("footer lost to an overlong body")
	}
}

// The pane title and its right-hand count live in the top border, so the
// list says what it holds without spending a body row.
func TestTitleAndCountRideTheTopBorder(t *testing.T) {
	top := lines(base().Render(plain()))[1]
	if !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Fatalf("top border malformed: %q", top)
	}
	if !strings.Contains(top, "Workflows") || !strings.Contains(top, "5 collected") {
		t.Fatalf("title or count missing: %q", top)
	}
}

// The context band names the server, the namespace, and the safety mode.
// Losing the mode would hide whether mutations are possible.
func TestHeaderCarriesContextAndMode(t *testing.T) {
	head := lines(base().Render(plain()))[0]
	for _, want := range []string{"demo", "READ ONLY"} {
		if !strings.Contains(head, want) {
			t.Fatalf("header missing %q: %q", want, head)
		}
	}
}

// A narrow terminal drops the border rather than spending 4 of its columns
// on it. The header, the body and the footer all survive.
func TestNarrowTerminalDropsTheBorder(t *testing.T) {
	f := base()
	f.Width = 48
	out := f.Render(plain())
	if strings.Contains(out, "┌") || strings.Contains(out, "│") {
		t.Fatalf("border drawn below the minimum width:\n%s", out)
	}
	got := lines(out)
	if len(got) != f.Height {
		t.Fatalf("got %d lines, want %d", len(got), f.Height)
	}
	// The hints are truncated to fit, but the footer band still exists and
	// still leads with the route and the first hints.
	last := got[len(got)-1]
	if !strings.Contains(last, "list") || !strings.Contains(last, "filter") {
		t.Fatalf("footer lost in the narrow layout: %q", last)
	}
	if !strings.Contains(last, "watch") {
		t.Fatalf("right-aligned status squeezed out before the hints: %q", last)
	}
}

// BodyHeight is the contract children size themselves against. If it
// disagrees with Render the pane scrolls or leaves a gap.
func TestBodyHeightMatchesWhatRenderAccepts(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		micko bool
	}{{80, 12, false}, {48, 12, false}, {100, 6, false}, {80, 40, true}, {120, 50, true}, {120, 50, false}} {
		f := base()
		f.Width, f.Height, f.Micko = tc.w, tc.h, tc.micko
		n := f.BodyHeight()
		f.Body = make([]string, n)
		for i := range f.Body {
			f.Body[i] = "x"
		}
		got := lines(f.Render(plain()))
		count := 0
		for _, l := range got {
			if strings.Contains(l, "x") {
				count++
			}
		}
		if count != n {
			t.Fatalf("%dx%d: BodyHeight()=%d but %d body rows rendered", tc.w, tc.h, n, count)
		}
	}
}

// An unknown height (before the first WindowSizeMsg) must still render, so
// the very first frame is not blank.
func TestUnknownSizeStillRenders(t *testing.T) {
	f := base()
	f.Width, f.Height = 0, 0
	out := f.Render(plain())
	if !strings.Contains(out, "row one") || !strings.Contains(out, "? help") {
		t.Fatalf("unsized render lost content:\n%s", out)
	}
}

// Untrusted server text reaches the frame through the title and the status.
// The frame must never let a control byte or a stray newline out.
func TestFrameTextIsSanitized(t *testing.T) {
	f := base()
	f.Title = "wf\x1b[31mred"
	f.Status = "a\nb"
	f.Namespace = "ns\x07"
	out := f.Render(plain())
	for _, bad := range []string{"\x1b[31m", "\x07"} {
		if strings.Contains(out, bad) {
			t.Fatalf("unsanitized control sequence %q survived:\n%q", bad, out)
		}
	}
	if len(lines(out)) != f.Height {
		t.Fatal("an embedded newline changed the frame height")
	}
}

// A long right-hand status must be shortened, not dropped. During an outage
// that status is the stale warning, and dropping it leaves the operator with
// no sign that the data is old.
func TestLongTitleRightIsTruncatedNotDropped(t *testing.T) {
	f := Frame{
		Width:      80,
		Height:     6,
		Title:      "Workflows",
		TitleRight: "list: STALE (last good 19 workflows): dial tcp 127.0.0.1:50422: connect: connection refused",
	}
	got, _ := f.topBorder(shared.NewTheme(false))
	if !strings.Contains(got, "Workflows") {
		t.Fatalf("title lost: %q", got)
	}
	if !strings.Contains(got, "STALE") {
		t.Fatalf("stale status was dropped instead of shortened: %q", got)
	}
	if w := ansi.StringWidth(got); w != 80 {
		t.Fatalf("border width = %d, want 80: %q", w, got)
	}
}

// When the pane is too narrow for any useful status, the title wins.
func TestVeryNarrowTitleKeepsTheTitle(t *testing.T) {
	f := Frame{Width: 20, Height: 4, Title: "Workflows", TitleRight: "list: STALE (last good 19): connection refused"}
	got, _ := f.topBorder(shared.NewTheme(false))
	if w := ansi.StringWidth(got); w != 20 {
		t.Fatalf("border width = %d, want 20: %q", w, got)
	}
}
