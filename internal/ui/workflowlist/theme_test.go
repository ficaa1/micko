package workflowlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// themedList is a list of mixed phases drawn in a truecolor skin.
func themedList(t *testing.T) (*Model, shared.Theme) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("gruvbox-dark", false)
	if err != nil {
		t.Fatal(err)
	}
	m := New(th, false)
	m.SetSize(100, 20)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 6)), testkit.FixtureEpoch)
	return &m, th
}

// A themed list reads exactly like the plain one: the theme adds colour and
// never a character, so the phase glyph and word are always there.
func TestThemedListIsThePlainListStyled(t *testing.T) {
	m, _ := themedList(t)
	got := m.BodyLines(testkit.FixtureEpoch)
	m.SetTheme(shared.NewTheme(true))
	want := m.BodyLines(testkit.FixtureEpoch)
	if len(got) != len(want) {
		t.Fatalf("themed list has %d lines, plain %d", len(got), len(want))
	}
	for i := range got {
		// The selected row is padded to the pane; trailing blanks are not text.
		if strings.TrimRight(ansi.Strip(got[i]), " ") != strings.TrimRight(want[i], " ") {
			t.Errorf("line %d differs:\n%q\n%q", i, ansi.Strip(got[i]), want[i])
		}
	}
}

// Only the phase cell carries the phase colour, so the name reads in the
// ordinary text colour on every row; the selected row is one bar across the
// pane instead.
func TestRowsColourThePhaseCellAndSelectAcrossThePane(t *testing.T) {
	m, th := themedList(t)
	lines := m.BodyLines(testkit.FixtureEpoch)
	var sel, other string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, sgr(th.Selected)):
			sel = l
		case strings.Contains(l, "fixture-wf-") && other == "":
			other = l
		}
	}
	if sel == "" || other == "" {
		t.Fatalf("no selected or plain row in:\n%s", strings.Join(lines, "\n"))
	}
	if w := ansi.StringWidth(sel); w != 100 {
		t.Errorf("selected row is %d cells, want the pane's 100", w)
	}
	if !strings.HasPrefix(other, "fixture-wf-") {
		t.Errorf("the name is styled: %q", other)
	}
	phase := strings.Fields(ansi.Strip(other))[2]
	if !strings.Contains(other, sgr(th.PhaseStyle(phase))) {
		t.Errorf("phase cell %q not in its phase colour: %q", phase, other)
	}
	if !strings.Contains(strings.Join(lines, "\n"), sgr(th.TableHeader)) {
		t.Error("column heads are not in the table header style")
	}
}

func sgr(s interface{ Render(...string) string }) string {
	out := s.Render("x")
	return out[:strings.Index(out, "x")]
}
