package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Overlay operations show or hide the rendered help.
func TestHelpOverlayLifecycle(t *testing.T) {
	var h HelpOverlay
	for _, c := range []struct {
		name string
		op   func()
		open bool
	}{
		{"initial", func() {}, false}, {"toggle open", h.Toggle, true}, {"toggle closed", h.Toggle, false},
		{"palette open", h.Open, true}, {"open remains open", h.Open, true}, {"close", h.Close, false}, {"close remains closed", h.Close, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.op()
			if got := h.IsOpen(); got != c.open {
				t.Errorf("IsOpen = %v, want %v", got, c.open)
			}
			view := h.View(80, 20)
			if (view != "") != c.open {
				t.Errorf("View = %q, want visible %v", view, c.open)
			}
		})
	}
}

// The overlay documents the keys and query syntax readers use on each route.
func TestHelpOverlayDocumentsKeys(t *testing.T) {
	var h HelpOverlay
	h.Open()
	view := h.View(76, 32)
	for _, want := range []string{
		"j", "k", "enter", "l", "/", "s", "r", "?", "esc", "q", "ctrl+c", "tab",
		"& only matching lines", "w wrap long lines", "L source labels", "ctrl+t server timestamps",
		"w wide columns", "a|b", "!word", "/regex/", "~fuzzy", "phase=failed", "age<2h", "dur>10m", "label:k=v", "label:!k", "tmpl=x", "cron=x",
	} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(view, want) {
				t.Errorf("help lacks %q:\n%s", want, view)
			}
		})
	}
}

// Help fits its supplied box and displays all keys in the common terminal body.
func TestHelpOverlayGeometry(t *testing.T) {
	var h HelpOverlay
	h.Open()
	for _, width := range []int{40, 76, 100} {
		for _, height := range []int{1, 6, 10, 20, 32, 40} {
			view := h.View(width, height)
			lines := strings.Split(view, "\n")
			if len(lines) > height {
				t.Errorf("%dx%d: %d rows, want at most %d", width, height, len(lines), height)
			}
			for row, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d row %d: %d cells, want at most %d", width, height, row, got, width)
				}
			}
		}
	}
	view := h.View(76, 32)
	if got := len(strings.Split(view, "\n")); got != 32 {
		t.Errorf("common body: %d rows, want 32", got)
	}
	if strings.Contains(view, "…") || !strings.Contains(view, "results stay until esc") {
		t.Errorf("common body clips keys:\n%s", view)
	}
}

// Themed help keeps plain text and width budgets while emphasizing section names.
func TestHelpOverlayThemeOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var plain, themed HelpOverlay
	plain.Toggle()
	themed.Toggle()
	skin, ok := LookupSkin("nord")
	if !ok {
		t.Fatalf("unknown skin %q", "nord")
	}
	th := skin.Theme(false)
	themed.SetTheme(th)
	for _, w := range []int{0, 40, 100} {
		want := plain.View(w, 30)
		got := themed.View(w, 30)
		if ansi.Strip(got) != want {
			t.Fatalf("width %d: themed overlay text differs:\n%s\n---\n%s", w, ansi.Strip(got), want)
		}
		for _, l := range strings.Split(got, "\n") {
			if w > 0 && ansi.StringWidth(l) > w {
				t.Fatalf("width %d: themed line is %d cells: %q", w, ansi.StringWidth(l), l)
			}
		}
	}
	if !strings.Contains(themed.View(100, 30), th.Accent.Render("Global")) {
		t.Error("section names are not drawn in the accent")
	}
}
