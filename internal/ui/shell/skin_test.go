package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// skinned builds the theme of a named skin with colour forced on, whatever
// the environment running the tests says.
func skinned(t *testing.T, name string) shared.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme(name, false)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// The stable-geometry guarantee holds in colour as well as in plain text:
// every skin, at every width, fills exactly the terminal, keeps its footer
// on the last row, and drops the border below the minimum width. Styling
// that leaked a cell, or a painted band that forgot one, would tear the
// alternate screen.
func TestEverySkinKeepsTheFrameGeometry(t *testing.T) {
	for _, name := range shared.SkinNames() {
		th := skinned(t, name)
		for _, w := range []int{140, 80, 60, 48} {
			for _, mode := range []bool{false, true} {
				f := base()
				f.Width, f.Height = w, 12
				f.Hints, f.Help = "/ filter   enter open", "? help"
				f.ActionsEnabled = mode
				if mode {
					f.Mode = "ACTIONS ENABLED"
				}
				out := f.Render(th)
				got := lines(out)
				if len(got) != f.Height {
					t.Fatalf("%s at %d: %d lines, want %d", name, w, len(got), f.Height)
				}
				for i, l := range got {
					if cw := ansi.StringWidth(l); cw != w {
						t.Fatalf("%s at %d: line %d is %d cells: %q", name, w, i, cw, l)
					}
				}
				last := ansi.Strip(got[len(got)-1])
				if !strings.Contains(last, "? help") {
					t.Fatalf("%s at %d: footer lost the help hint: %q", name, w, last)
				}
				head := ansi.Strip(got[0])
				if !strings.Contains(head, f.Mode) {
					t.Fatalf("%s at %d: header lost the mode %q: %q", name, w, f.Mode, head)
				}
				b := th.Borders()
				plainOut := ansi.Strip(out)
				if w < minBorderWidth {
					if strings.Contains(plainOut, b.TopLeft) || strings.Contains(plainOut, b.Left) {
						t.Fatalf("%s at %d: border drawn below the minimum width:\n%s", name, w, plainOut)
					}
					continue
				}
				if !strings.HasPrefix(ansi.Strip(got[1]), b.TopLeft) || !strings.HasPrefix(ansi.Strip(got[len(got)-2]), b.BottomLeft) {
					t.Fatalf("%s at %d: border not in its rows:\n%s", name, w, plainOut)
				}
			}
		}
	}
}

// The default skin styles the frame without changing a character of it: a
// reader of the screen text, or a test of it, sees exactly the plain frame.
// The painted skins add a margin cell at each end of the header band and
// pad the badge, and round the corners, so only the default is held to it.
func TestDefaultSkinIsThePlainFrameStyled(t *testing.T) {
	th := skinned(t, shared.SkinDefault)
	for _, w := range []int{100, 48, 0} {
		f := base()
		f.Width = w
		if w == 0 {
			f.Height = 0
		}
		if got, want := ansi.Strip(f.Render(th)), f.Render(plain()); got != want {
			t.Fatalf("width %d: styled text differs from plain:\n%s\n---\n%s", w, got, want)
		}
	}
}

// Each key in the footer is drawn as a key and its description as muted
// text. The hint text itself is unchanged, separators included.
func TestFooterStylesKeysApartFromDescriptions(t *testing.T) {
	th := skinned(t, "catppuccin-mocha")
	f := base()
	foot := lines(f.Render(th))[f.Height-1]
	for _, key := range []string{"/", "enter", "?"} {
		if !strings.Contains(foot, th.HintKey.Render(key)) {
			t.Errorf("key %q is not drawn as a key: %q", key, foot)
		}
	}
	if !strings.Contains(foot, th.HintDesc.Render(" filter")) {
		t.Errorf("description is not muted: %q", foot)
	}
	if !strings.Contains(ansi.Strip(foot), "/ filter   enter open   ? help") {
		t.Errorf("hint text changed: %q", ansi.Strip(foot))
	}
}

// The safety mode is a badge, and the armed state gets the louder one. The
// words stay: the badge colour only repeats them.
func TestModeBadgeFollowsActionsEnabled(t *testing.T) {
	th := skinned(t, "nord")
	f := base()
	if head := lines(f.Render(th))[0]; !strings.Contains(head, th.BadgeReadOnly.Render(" READ ONLY ")) {
		t.Errorf("read-only badge missing: %q", head)
	}
	f.Mode, f.ActionsEnabled = "ACTIONS ENABLED", true
	if head := lines(f.Render(th))[0]; !strings.Contains(head, th.BadgeActions.Render(" ACTIONS ENABLED ")) {
		t.Errorf("actions badge missing: %q", head)
	}
}

// A notice is the answer to the last key, so it replaces the hints and is
// drawn as a message: its first word is not a key.
func TestNoticeReplacesTheHints(t *testing.T) {
	th := skinned(t, "nord")
	f := base()
	f.Notice = "copied workflow name"
	foot := lines(f.Render(th))[f.Height-1]
	if strings.Contains(ansi.Strip(foot), "filter") {
		t.Errorf("hints drawn beside a notice: %q", ansi.Strip(foot))
	}
	if !strings.Contains(foot, th.Accent.Render("copied workflow name")) {
		t.Errorf("notice not drawn as one message: %q", foot)
	}
}

// A painted band has no holes: every span on it, and the gap between the
// sides, carries the band's background.
func TestPaintedHeaderBandHasNoHoles(t *testing.T) {
	th := skinned(t, "gruvbox-dark")
	f := base()
	head := lines(f.Render(th))[0]
	bg := th.Band.Render(" ")
	if !strings.HasPrefix(head, bg) {
		t.Errorf("band does not start painted: %q", head)
	}
	if !strings.HasSuffix(head, bg) {
		t.Errorf("band does not end painted: %q", head)
	}
	if !strings.Contains(head, th.Muted.Background(th.Band.GetBackground()).Render("server: ")) {
		t.Errorf("a label lost the band background: %q", head)
	}
}

// Truncating a run of spans keeps the styles of what survives and ends in
// an ellipsis inside the span that crossed the edge.
func TestTruncSpansCutsInsideTheCrossingSpan(t *testing.T) {
	th := skinned(t, "nord")
	ss := []span{{"abc", th.Accent}, {"defgh", th.Muted}}
	got := truncSpans(ss, 6)
	if spansWidth(got) != 6 || len(got) != 2 || got[1].text != "de…" {
		t.Fatalf("truncSpans = %+v", got)
	}
	if got := truncSpans(ss, 20); len(got) != 2 {
		t.Fatalf("truncSpans cut a run that fit: %+v", got)
	}
	if got := truncSpans(ss, 0); got != nil {
		t.Fatalf("truncSpans(0) = %+v", got)
	}
}
