package shared

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Phase glyphs keep their meaning and one-cell alignment without color.
func TestPhaseSymbols(t *testing.T) {
	for _, c := range []struct{ phase, want string }{{"Running", "●"}, {"Succeeded", "✓"}, {"Failed", "✗"}, {"Error", "✗"}, {"Pending", "○"}, {"Suspended", "◐"}, {"WeirdFuturePhase", "•"}} {
		t.Run(c.phase, func(t *testing.T) {
			got := PhaseSymbol(c.phase)
			if got != c.want {
				t.Errorf("PhaseSymbol(%q) = %q, want %q", c.phase, got, c.want)
			}
			if width := ansi.StringWidth(got); width != 1 {
				t.Errorf("symbol %q is %d cells, want 1", got, width)
			}
		})
	}
}
