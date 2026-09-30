package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// plain returns a theme without terminal styling.
func plain() shared.Theme { return shared.NewTheme(true) }

func lines(s string) []string { return strings.Split(s, "\n") }

// base is a frame wide and tall enough for the bordered layout.
func base() Frame {
	return Frame{
		Width:      80,
		Height:     12,
		App:        "micko 0.2.0-beta.1",
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

// The pane heading keeps its title and useful state within the visible border.
func TestPaneHeading(t *testing.T) {
	for _, c := range []struct {
		name, title, right, wantRight string
		width                         int
		truncated                     bool
	}{
		{"collected count", "Workflows", "5 collected", "5 collected", 80, false},
		{"stale warning", "Workflows", "list: STALE (last good 19 workflows): connection refused by the workflow server", "list: STALE", 80, true},
		{"title takes scarce space", strings.Repeat("title", 10), "STALE connection refused", "", 60, false},
		{"state without title", "", "5 collected", "5 collected", 60, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := base()
			f.Width, f.Title, f.TitleRight = c.width, c.title, c.right
			top := lines(f.Render(plain()))[1]
			if !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") || ansi.StringWidth(top) != c.width {
				t.Fatalf("heading = %q, want %d-cell border", top, c.width)
			}
			if !strings.Contains(top, c.title) {
				t.Fatalf("heading = %q, want title %q", top, c.title)
			}
			if c.wantRight != "" && !strings.Contains(top, c.wantRight) {
				t.Fatalf("heading = %q, want state %q", top, c.wantRight)
			}
			if c.wantRight == "" && strings.Contains(top, "STALE") {
				t.Fatalf("heading = %q, want title to take the available space", top)
			}
			if got := strings.Contains(top, "…"); got != c.truncated {
				t.Fatalf("heading = %q, truncated = %v, want %v", top, got, c.truncated)
			}
		})
	}
}

// The header identifies the program, server, namespace and mutation mode.
func TestHeaderCarriesContextAndMode(t *testing.T) {
	head := lines(base().Render(plain()))[0]
	for _, want := range []string{"micko 0.2.0-beta.1", "server: synthetic demo", "ns: demo", "READ ONLY"} {
		if !strings.Contains(head, want) {
			t.Fatalf("header = %q, want %q", head, want)
		}
	}
}

// The budgets children receive match the cells and rows available in the rendered body.
func TestBodyBudget(t *testing.T) {
	for _, c := range []struct {
		name          string
		w, h, bw, bh  int
		mascot, floor bool
	}{
		{"bordered", 80, 12, 76, 8, false, false},
		{"plain", 48, 12, 48, 9, false, false},
		{"short", 100, 6, 96, 2, false, false},
		{"perched minimum", 80, 40, 76, 33, true, false},
		{"perched large", 120, 50, 116, 43, true, false},
		{"floor", 120, 50, 116, 42, true, true},
		{"mascot disabled", 120, 50, 116, 46, false, false},
		{"unknown", 0, 0, 0, 0, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := base()
			f.Width, f.Height, f.Mascot, f.MascotFloor = c.w, c.h, c.mascot, c.floor
			if got := f.BodyWidth(); got != c.bw {
				t.Fatalf("BodyWidth() = %d, want %d", got, c.bw)
			}
			if got := f.BodyHeight(); got != c.bh {
				t.Fatalf("BodyHeight() = %d, want %d", got, c.bh)
			}
			if c.h == 0 {
				return
			}
			f.Body = make([]string, c.bh)
			for i := range f.Body {
				f.Body[i] = "BODY" + strings.Repeat("x", c.bw-4)
			}
			count := 0
			for _, l := range lines(f.Render(plain())) {
				if strings.Contains(l, "BODY") {
					count++
					if !strings.Contains(l, strings.Repeat("x", c.bw-4)) {
						t.Fatalf("body clipped within width budget: %q", l)
					}
				}
			}
			if count != c.bh {
				t.Fatalf("rendered %d body rows, want %d", count, c.bh)
			}
		})
	}
}

// The first frame renders its content before the terminal size is known.
func TestUnknownSizeStillRenders(t *testing.T) {
	f := base()
	f.Width, f.Height = 0, 0
	out := f.Render(plain())
	if !strings.Contains(out, "row one") || !strings.Contains(out, "? help") {
		t.Fatalf("unsized render lost content:\n%s", out)
	}
}

// Every metadata field strips controls and flattens whitespace before reaching the terminal.
func TestFrameTextIsSanitized(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*Frame, string)
	}{
		{"app", func(f *Frame, s string) { f.App = s }},
		{"server", func(f *Frame, s string) { f.Server = s }},
		{"namespace", func(f *Frame, s string) { f.Namespace = s }},
		{"mode", func(f *Frame, s string) { f.Mode = s }},
		{"title", func(f *Frame, s string) { f.Title = s }},
		{"title state", func(f *Frame, s string) { f.TitleRight = s }},
		{"route", func(f *Frame, s string) { f.Route = s }},
		{"hints", func(f *Frame, s string) { f.Hints = s }},
		{"notice", func(f *Frame, s string) { f.Notice = s }},
		{"help", func(f *Frame, s string) { f.Help = s }},
		{"status", func(f *Frame, s string) { f.Status = s }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := base()
			f.Width = 140
			c.set(&f, "safe\x1b[31mred\x07\nnext\tend")
			out := f.Render(plain())
			if strings.ContainsAny(out, "\x1b\x07\t") || len(lines(out)) != f.Height || !strings.Contains(out, "safered next end") {
				t.Fatalf("render = %q, want safe flattened metadata in %d rows", out, f.Height)
			}
		})
	}
}
