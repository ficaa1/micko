package profiles

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// The cursor row is a bar across the dialog, the connected profile is in the
// accent when the cursor is elsewhere, and the themed dialog reads exactly
// like the plain one.
func TestThemedPickerHighlightsTheCursorRow(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	skin, ok := shared.LookupSkin("rose-pine")
	if !ok {
		t.Fatalf("unknown skin %q", "rose-pine")
	}
	th := skin.Theme(false)
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.SetSize(50, 20)
	m.Open("dev", "dev")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	want := m.BodyLines()
	m.SetTheme(th)
	got := m.BodyLines()
	for i := range got {
		if strings.TrimRight(ansi.Strip(got[i]), " ") != want[i] {
			t.Errorf("line %d: %q, want %q", i, ansi.Strip(got[i]), want[i])
		}
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, th.SelectRow("  prod", 50)) {
		t.Errorf("cursor row is not a bar across the dialog:\n%q", joined)
	}
	if !strings.Contains(joined, th.Accent.Render("* dev")) {
		t.Errorf("the connected profile is not in the accent:\n%q", joined)
	}
}
