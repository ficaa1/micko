package workflowlist

import (
	"strings"
	"testing"

	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// paneModel builds a list with n fixture rows at a known size.
func paneModel(t *testing.T, n, w, h int) Model {
	t.Helper()
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", n)), testkit.FixtureEpoch)
	m.SetSize(w, h)
	return m
}

// The shell owns the footer band. A pane that draws its own footer would
// print the key hints twice.
func TestBodyLinesExcludeTheFooter(t *testing.T) {
	m := paneModel(t, 6, 100, 12)
	for i, l := range m.BodyLines(testkit.FixtureEpoch) {
		if strings.Contains(l, "? help") {
			t.Fatalf("body line %d carries footer hints: %q", i, l)
		}
	}
}

// The pane title lives in the shell border, so repeating it as the first
// body row wastes a line.
func TestBodyLinesExcludeThePaneTitle(t *testing.T) {
	m := paneModel(t, 6, 100, 12)
	body := m.BodyLines(testkit.FixtureEpoch)
	if strings.Contains(body[0], "WORKFLOWS") {
		t.Fatalf("body repeats the pane title: %q", body[0])
	}
	if !strings.Contains(m.PaneTitle(), "Workflows") {
		t.Fatalf("pane title lost: %q", m.PaneTitle())
	}
}

// The body must fit the budget it was given. Overflow would be clipped by
// the shell, which silently loses rows the list thinks it is showing.
func TestBodyLinesFitTheHeightBudget(t *testing.T) {
	for _, h := range []int{3, 5, 8, 20} {
		m := paneModel(t, 30, 100, h)
		if got := len(m.BodyLines(testkit.FixtureEpoch)); got > h {
			t.Fatalf("height %d: body is %d lines", h, got)
		}
	}
}

// Color is never the only carrier of a status (UI-03/07). Each row shows a
// glyph AND the phase word.
func TestRowsCarryASymbolAndTheWord(t *testing.T) {
	m := paneModel(t, 6, 100, 20)
	body := strings.Join(m.BodyLines(testkit.FixtureEpoch), "\n")
	for _, phase := range []string{"Failed", "Running", "Succeeded", "Pending"} {
		if !strings.Contains(body, shared.PhaseSymbol(phase)+" "+phase) {
			t.Fatalf("phase %q is not rendered as symbol + word:\n%s", phase, body)
		}
	}
}

// The window position belongs in the footer status, so a windowed list is
// distinguishable from a short one.
func TestWindowStatusReportsThePosition(t *testing.T) {
	m := paneModel(t, 30, 100, 8)
	_ = m.BodyLines(testkit.FixtureEpoch)
	if got := m.WindowStatus(); !strings.Contains(got, "/30") {
		t.Fatalf("window status %q does not report the total", got)
	}
	full := paneModel(t, 3, 100, 40)
	_ = full.BodyLines(testkit.FixtureEpoch)
	if got := full.WindowStatus(); strings.Contains(got, "-") {
		t.Fatalf("a fully visible list should not claim a window: %q", got)
	}
}

// Hints are the contract the help overlay documents. Drifting apart makes
// the overlay a lie. `?` is not listed here: it is root-owned, so the shell
// footer advertises it once for every route.
func TestHintsAdvertiseTheDocumentedKeys(t *testing.T) {
	m := paneModel(t, 3, 100, 20)
	h := m.Hints()
	if strings.Contains(h, "? help") {
		t.Fatalf("the pane repeats the shell's global help hint: %q", h)
	}
	for _, key := range []string{"enter", "l", "/", "s", "r"} {
		if !strings.Contains(h, key) {
			t.Fatalf("hints %q omit key %q", h, key)
		}
	}
}

// The narrow-terminal notice stays a whole-pane message: below 60 columns
// there is no room for a table, and quit/help must still be reachable.
func TestNarrowPaneKeepsTheResizeNotice(t *testing.T) {
	m := paneModel(t, 6, 40, 12)
	body := strings.Join(m.BodyLines(testkit.FixtureEpoch), "\n")
	if !strings.Contains(body, "too small") || !strings.Contains(body, "q quit") {
		t.Fatalf("narrow notice lost:\n%s", body)
	}
}
