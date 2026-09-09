package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestWrapNoopWhenUnsized: wrapping is a no-op when no width is known (the
// test harness and goldens render at width 0; wrapping must not change them).
func TestWrapNoopWhenUnsized(t *testing.T) {
	s := "a long error message that must stay exactly as-is"
	if got := Wrap(s, 0); got != s {
		t.Fatalf("Wrap(s, 0) changed the string: %q", got)
	}
	if got := Wrap(s, -1); got != s {
		t.Fatalf("Wrap(s, -1) changed the string: %q", got)
	}
}

// TestWrapSplitsLongLineAtWidth: long text must break into lines that each
// fit the width, preserving every word and keeping the width-bound guarantee
// (the anti-clipping invariant for long error text).
func TestWrapSplitsLongLineAtWidth(t *testing.T) {
	s := "server returned an HTML page instead of API data (content-type text/html; charset=UTF-8); this endpoint expects interactive browser login configure a server token do not retry automatically tail-marker"
	w := Wrap(s, 40)
	lines := strings.Split(w, "\n")
	if len(lines) < 3 {
		t.Fatalf("long text not wrapped into multiple lines: %q", w)
	}
	for _, ln := range lines {
		if ansi.StringWidth(ln) > 40 {
			t.Fatalf("line exceeds 40 columns: %q", ln)
		}
	}
	if !strings.Contains(w, "tail-marker") {
		t.Fatalf("trailing word lost after wrap: %q", w)
	}
	// Every original word must survive the wrap (word-boundary wrap never
	// drops or reorders content).
	joined := strings.Join(strings.Fields(w), " ")
	for _, word := range strings.Fields(s) {
		if !strings.Contains(joined, word) {
			t.Fatalf("word %q lost after wrap: %q", word, w)
		}
	}
}
