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

// TestHelpOverlayClosedRendersNothing keeps the caller simple: a closed
// overlay contributes no lines to the frame.
func TestHelpOverlayClosedRendersNothing(t *testing.T) {
	var h HelpOverlay
	if got := h.View(80, 20); got != "" {
		t.Fatalf("closed overlay must render nothing, got %q", got)
	}
}

// TestHelpOverlayFitsWholeAt80x40: every line of the overlay is shown, and
// none is clipped, inside the body of an 80x40 terminal.
func TestHelpOverlayFitsWholeAt80x40(t *testing.T) {
	lines := helpLines()
	if len(lines) > helpFitHeight {
		t.Fatalf("help has %d lines; an 80x40 body holds %d", len(lines), helpFitHeight)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > helpFitWidth {
			t.Errorf("help line is %d cells, an 80-column body holds %d: %q", w, helpFitWidth, l)
		}
	}
}

// TestHelpOverlayDocumentsTheFilterAndWideKeys: the filter syntax and the
// wide toggle have no other on-screen reference than this overlay.
func TestHelpOverlayDocumentsTheFilterAndWideKeys(t *testing.T) {
	var h HelpOverlay
	h.Toggle()
	v := h.View(helpFitWidth, helpFitHeight)
	for _, want := range []string{"w wide columns", "a|b", "!word", "/regex/", "~fuzzy", "phase=failed",
		"age<2h", "dur>10m", "label:k=v", "label:!k", "tmpl=x", "cron=x"} {
		if !strings.Contains(v, want) {
			t.Errorf("help does not mention %q:\n%s", want, v)
		}
	}
}
