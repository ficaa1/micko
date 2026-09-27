package namespaces

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// The cursor row is a bar across the dialog, the namespace in use is in the
// accent when the cursor is elsewhere, and the themed dialog reads exactly
// like the plain one.
func TestThemedPickerHighlightsTheCursorRow(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("one-dark", false)
	if err != nil {
		t.Fatal(err)
	}
	m := newPicker("beta", "alpha", "beta", "gamma")
	m.SetSize(40, 20)
	press(m, "down")
	want := m.BodyLines()
	m.SetTheme(th)
	got := m.BodyLines()
	for i := range got {
		if strings.TrimRight(ansi.Strip(got[i]), " ") != want[i] {
			t.Errorf("line %d: %q, want %q", i, ansi.Strip(got[i]), want[i])
		}
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, th.SelectRow("  gamma", 40)) {
		t.Errorf("cursor row is not a bar across the dialog:\n%q", joined)
	}
	if !strings.Contains(joined, th.Accent.Render("* beta")) {
		t.Errorf("the namespace in use is not in the accent:\n%q", joined)
	}
}
