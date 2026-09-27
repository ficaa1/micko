package detail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

func nordTheme(t *testing.T) shared.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("nord", false)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// The tree connectors are drawn in the guide style, apart from the node they
// lead to, so the eye follows the names. Below the timing bar's tier the
// text is the plain row exactly; the bar itself changes glyphs with the
// theme and has its own test.
func TestNodeRowsDrawConnectorsAsGuides(t *testing.T) {
	th := nordTheme(t)
	rows, _ := FlattenOutline(BuildNodeOutline(gateWorkflow(), OutlineOptions{}), true)
	guided := 0
	for _, r := range rows {
		got := RenderFlatRow(r, 70, th, false)
		if want := RenderFlatRow(r, 70, shared.NewTheme(true), false); ansi.Strip(got) != want {
			t.Fatalf("themed row text differs:\n%q\n%q", ansi.Strip(got), want)
		}
		if r.Prefix != "" && r.Section == "" && !r.HasChildren {
			if !strings.HasPrefix(got, th.TreeGuide.Render(r.Indent)) {
				t.Errorf("connector %q is not drawn as a guide: %q", r.Indent, got)
			}
			guided++
		}
	}
	if guided == 0 {
		t.Fatal("no row had a connector to draw")
	}
}

// The selected node is a highlight bar across the pane, not a patch behind
// its text.
func TestSelectedNodeIsABarAcrossThePane(t *testing.T) {
	th := nordTheme(t)
	rows, _ := FlattenOutline(BuildNodeOutline(gateWorkflow(), OutlineOptions{}), true)
	got := RenderFlatRow(rows[0], 100, th, true)
	if w := ansi.StringWidth(got); w != 100 {
		t.Errorf("selected row is %d cells, want 100", w)
	}
	if !strings.HasPrefix(got, sgrOf(th.Selected)) {
		t.Errorf("selected row not drawn in the selection style: %q", got)
	}
}

// The active tab is drawn in the accent and the others muted; the brackets
// still mark the active one for a mono terminal.
func TestTabStripStylesTheActiveTab(t *testing.T) {
	th := nordTheme(t)
	got := tabStrip("nodes", th)
	if !strings.Contains(got, th.TabActive.Render("[Nodes]")) {
		t.Errorf("active tab not in the accent: %q", got)
	}
	if !strings.Contains(got, th.TabInactive.Render(" Summary ")) {
		t.Errorf("inactive tab not muted: %q", got)
	}
	if ansi.Strip(got) != tabStrip("nodes", shared.NewTheme(true)) {
		t.Errorf("themed strip text differs: %q", ansi.Strip(got))
	}
}

func sgrOf(s interface{ Render(...string) string }) string {
	out := s.Render("x")
	return out[:strings.Index(out, "x")]
}
