package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestHelpOverlayToggle: `?` opens and closes; Esc closes.
func TestHelpOverlayToggle(t *testing.T) {
	var h HelpOverlay
	if h.IsOpen() {
		t.Fatal("help must start closed")
	}
	h.Toggle()
	if !h.IsOpen() {
		t.Fatal("Toggle must open the overlay")
	}
	h.Toggle()
	if h.IsOpen() {
		t.Fatal("Toggle must close the overlay")
	}
	h.Toggle()
	h.Close()
	if h.IsOpen() {
		t.Fatal("Close must close the overlay")
	}
}

// TestHelpOverlayDocumentsEveryAdvertisedKey: the list footer advertises a
// key set. Each of those keys must appear in the overlay, so the footer
// never promises something the help cannot explain.
func TestHelpOverlayDocumentsEveryAdvertisedKey(t *testing.T) {
	var h HelpOverlay
	h.Toggle()
	v := h.View(100, 30)
	for _, key := range []string{"j", "k", "enter", "l", "/", "s", "r", "?", "esc", "q", "ctrl+c", "tab"} {
		if !strings.Contains(v, key) {
			t.Fatalf("help overlay does not document %q:\n%s", key, v)
		}
	}
}

// TestHelpOverlayFitsBudget: the overlay is drawn inside the frame, so it
// must respect the height it is given, at every size the app allows.
func TestHelpOverlayFitsBudget(t *testing.T) {
	var h HelpOverlay
	h.Toggle()
	for _, h2 := range []int{6, 10, 20, 40} {
		v := h.View(80, h2)
		got := len(strings.Split(strings.TrimRight(v, "\n"), "\n"))
		if got > h2 {
			t.Fatalf("height %d: overlay rendered %d lines:\n%s", h2, got, v)
		}
	}
}

// A themed overlay is the same text as a plain one: the theme picks out the
// title and the section names but adds and removes no character, and a line
// clipped to the width still fits it.
func TestHelpOverlayThemeOnlyStyles(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var plain, themed HelpOverlay
	plain.Toggle()
	themed.Toggle()
	th, err := SkinTheme("nord", false)
	if err != nil {
		t.Fatal(err)
	}
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

// TestHelpOverlayClosedRendersNothing keeps the caller simple: a closed
// overlay contributes no lines to the frame.
func TestHelpOverlayClosedRendersNothing(t *testing.T) {
	var h HelpOverlay
	if got := h.View(80, 20); got != "" {
		t.Fatalf("closed overlay must render nothing, got %q", got)
	}
}
