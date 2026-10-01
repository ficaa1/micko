package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// skinned returns a named skin with color enabled.
func skinned(t *testing.T, name string) shared.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	skin, ok := shared.LookupSkin(name)
	if !ok {
		t.Fatalf("unknown skin %q", name)
	}
	return skin.Theme(false)
}

// Every skin keeps the terminal dimensions and reserves the border and footer rows for chrome.
func TestEverySkinKeepsTheFrameGeometry(t *testing.T) {
	for _, name := range append([]string{"plain"}, shared.SkinNames()...) {
		t.Run(name, func(t *testing.T) {
			th := plain()
			if name != "plain" {
				th = skinned(t, name)
			}
			for _, c := range []struct {
				name          string
				w, h, rows    int
				mascot, floor bool
			}{
				{"wide short body", 140, 12, 1, false, false},
				{"bordered empty body", 80, 7, 0, false, false},
				{"bordered long body", 80, 6, 50, false, false},
				{"border threshold", 60, 12, 2, false, false},
				{"below border threshold", 59, 12, 2, false, false},
				{"narrow", 48, 12, 2, false, false},
				{"perch", 80, 40, 2, true, false},
				{"floor", 80, 40, 2, true, true},
			} {
				t.Run(c.name, func(t *testing.T) {
					for _, armed := range []bool{false, true} {
						f := base()
						f.Width, f.Height, f.Mascot, f.MascotFloor = c.w, c.h, c.mascot, c.floor
						f.Body = make([]string, c.rows)
						for i := range f.Body {
							f.Body[i] = "body row"
						}
						f.Hints, f.Help = "/ filter   enter open", "? help"
						if armed {
							f.Mode = ""
						}
						out := f.Render(th)
						got := lines(out)
						if len(got) != c.h {
							t.Fatalf("armed=%v: height = %d, want %d", armed, len(got), c.h)
						}
						for i, l := range got {
							if cw := ansi.StringWidth(l); cw != c.w {
								t.Fatalf("armed=%v row %d: width = %d, want %d: %q", armed, i, cw, c.w, l)
							}
						}
						last := ansi.Strip(got[len(got)-1])
						if !strings.Contains(last, "? help") {
							t.Fatalf("footer = %q, want help", last)
						}
						if head := ansi.Strip(got[0]); !strings.Contains(head, f.Mode) {
							t.Fatalf("header = %q, want mode %q", head, f.Mode)
						}
						b := th.Borders()
						if c.w < 60 {
							if strings.Contains(ansi.Strip(out), b.TopLeft) || strings.Contains(ansi.Strip(out), b.Left) {
								t.Fatalf("narrow render has border: %q", out)
							}
							for _, want := range []string{"list", "filter", "watch"} {
								if !strings.Contains(last, want) {
									t.Fatalf("footer = %q, want %q", last, want)
								}
							}
						} else {
							top := 1
							if c.mascot && !c.floor {
								top = 4
							}
							if !strings.HasPrefix(ansi.Strip(got[top]), b.TopLeft) || !strings.HasPrefix(ansi.Strip(got[len(got)-2]), b.BottomLeft) {
								t.Fatalf("borders moved from rows %d/%d: %q", top, len(got)-2, out)
							}
						}
					}
				})
			}
		})
	}
}

// The default skin preserves the plain frame text.
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

// Footer keys and descriptions have distinct styles without changing their text or separators.
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

// An empty mode leaves the header without a badge.
func TestModeBadgeIsOptional(t *testing.T) {
	th := skinned(t, "nord")
	f := base()
	if head := lines(f.Render(th))[0]; !strings.Contains(head, th.BadgeReadOnly.Render(" READ ONLY ")) {
		t.Errorf("read-only badge missing: %q", head)
	}
	f.Mode = ""
	if head := lines(f.Render(th))[0]; strings.Contains(head, "READ ONLY") {
		t.Errorf("empty mode has a read-only badge: %q", head)
	}
}

// A notice replaces the hints and renders as a message.
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

// The header background covers its margins, labels and gaps.
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

// Footer truncation preserves the surviving span's style and marks the cut with an ellipsis.
func TestFooterTruncation(t *testing.T) {
	th := skinned(t, "nord")
	for _, c := range []struct {
		name                             string
		width                            int
		route, hints, help, want, styled string
	}{
		{"description cut", 9, "", "a abcdefgh", "", "a abcdef…", th.HintDesc.Render(" abcdef…")},
		{"key cut", 2, "", "abcdef", "", "a…", th.HintKey.Render("a…")},
		{"help takes available width", 6, "list", "a open", "? help", "? help", th.HintDesc.Render(" help")},
		{"unknown width", 0, "", "a abcdefgh", "", "a abcdefgh", th.HintDesc.Render(" abcdefgh")},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := base()
			f.Width, f.Route, f.Hints, f.Help, f.Status = c.width, c.route, c.hints, c.help, ""
			out := lines(f.Render(th))
			foot := out[len(out)-1]
			if got := ansi.Strip(foot); got != c.want {
				t.Fatalf("footer = %q, want %q", got, c.want)
			}
			if !strings.Contains(foot, c.styled) {
				t.Fatalf("footer = %q, want styled surviving span %q", foot, c.styled)
			}
		})
	}
}
