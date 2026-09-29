package workflowlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// renderCells returns the column text for a summary (view formatting rules).
func TestAgeTextRules(t *testing.T) {
	base := testkit.FixtureEpoch
	started := base.Add(-4 * time.Minute)
	s := core.Summary{
		Ref:       core.Ref{Name: "wf"},
		CreatedAt: base.Add(-time.Hour),
		StartedAt: &started,
	}
	if got := ageText(s, base); got != "4m" {
		t.Fatalf("ageText = %q, want 4m (started preferred)", got)
	}
	s.StartedAt = nil
	if got := ageText(s, base); got != "1h" {
		t.Fatalf("ageText = %q, want 1h", got)
	}
	s.CreatedAt = time.Time{}
	if got := ageText(s, base); got != "-" {
		t.Fatalf("missing timestamps must render '-', got %q", got)
	}
}

func TestDurationTextRules(t *testing.T) {
	base := testkit.FixtureEpoch
	started := base.Add(-30 * time.Minute)
	fin := base.Add(-25 * time.Minute)
	s := core.Summary{Ref: core.Ref{Name: "wf"}, StartedAt: &started, FinishedAt: &fin}
	if got := durationText(s); got != "5m" {
		t.Fatalf("duration = %q, want 5m", got)
	}
	s.FinishedAt = nil
	if got := durationText(s); got != "ongoing" {
		t.Fatalf("running duration = %q, want ongoing", got)
	}
	s.StartedAt = nil
	if got := durationText(s); got != "-" {
		t.Fatalf("no-start duration = %q, want '-'", got)
	}
}

func TestSanitizeInRenderPath(t *testing.T) {
	m := New(testTheme())
	evil := core.Summary{
		Ref:       core.Ref{Namespace: "ns", Name: "ok\x1b]0;pwned\x07name", UID: "uid"},
		Phase:     "Running",
		Message:   "hello\x1b[31mred",
		CreatedAt: testkit.FixtureEpoch,
	}
	m.SetItems([]core.Summary{evil}, testkit.FixtureEpoch)
	v := m.ViewAt(testkit.FixtureEpoch)
	if strings.ContainsRune(v, 0x1b) {
		t.Fatalf("ESC leaked into render:\n%q", v)
	}
	if !strings.Contains(v, "pwned") {
		// OSC payload content is stripped wholesale by the sanitizer;
		// assert the sequence bytes are gone rather than the payload text.
		t.Log("note: OSC payload removed entirely (expected)")
	}
	if strings.Contains(v, "hello\x1b[31mred") {
		t.Fatal("CSI sequence not neutralized in message column")
	}
}

func TestWideUnicodeAlignment(t *testing.T) {
	// Two rows, one with wide glyphs: name column alignment must keep the
	// phase columns aligned measured in CELLS (rune indexes legitimately
	// differ with wide glyphs; ansi.StringWidth is the truth).
	m := New(testTheme())
	w1 := core.Summary{
		Ref:       core.Ref{Name: "日本語のワークフロー名前", UID: "u1"},
		Phase:     "Running",
		CreatedAt: testkit.FixtureEpoch.Add(-time.Minute),
	}
	w2 := core.Summary{
		Ref:       core.Ref{Name: "ascii", UID: "u2"},
		Phase:     "Running",
		CreatedAt: testkit.FixtureEpoch.Add(-2 * time.Minute),
	}
	m.SetItems([]core.Summary{w1, w2}, testkit.FixtureEpoch)
	m.SetSize(80, 24)
	v := m.ViewAt(testkit.FixtureEpoch)
	lines := strings.Split(v, "\n")
	var offsets []int
	for _, ln := range lines {
		idx := strings.Index(ln, "Running")
		if idx < 0 || strings.HasPrefix(ln, "Sort") {
			continue
		}
		offsets = append(offsets, ansi.StringWidth(ln[:idx]))
	}
	if len(offsets) != 2 {
		t.Fatalf("expected 2 data rows, got %d:\n%s", len(offsets), v)
	}
	if offsets[0] != offsets[1] {
		t.Fatalf("phase columns misaligned with wide unicode (cells): %v\n%s", offsets, v)
	}
}

// The message column takes whatever the fixed columns leave. A failure message
// is the one cell with no natural length, and a wider terminal should show
// more of it rather than the same clipped fragment.
func TestTheMessageColumnGrowsWithThePane(t *testing.T) {
	m := New(shared.NewTheme(true))
	prev := 0
	for _, w := range []int{100, 140, 200} {
		m.SetSize(w, 20)
		msg := m.columnWidth(colMessage)
		if msg <= prev {
			t.Fatalf("width %d: message column = %d, want more than %d", w, msg, prev)
		}
		prev = msg
	}
}

// The row must fit the pane. A line wider than the pane wraps inside the
// border and pushes a second copy of every row onto the screen.
func TestARowNeverOverflowsThePane(t *testing.T) {
	row := core.Summary{
		Ref:     core.Ref{Name: strings.Repeat("n", 120), Namespace: "argo", UID: "u1"},
		Phase:   "Failed",
		Message: strings.Repeat("m", 400),
	}
	m := New(shared.NewTheme(true))
	for _, w := range []int{60, 80, 100, 120, 200} {
		m.SetSize(w, 20)
		m.SetItems([]core.Summary{row}, testkit.FixtureEpoch)
		for _, line := range m.BodyLines(testkit.FixtureEpoch) {
			if !strings.Contains(line, "NAME") && !strings.Contains(line, row.Ref.Name[:8]) {
				continue // the toolbar and status lines are the shell's to fit
			}
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("width %d: a table line is %d cells wide:\n%q", w, got, line)
			}
		}
	}
}

// A pane too narrow to carry a readable message drops the column instead of
// spending the width on a fragment that only says a message exists.
func TestANarrowPaneDropsTheMessageColumn(t *testing.T) {
	m := New(shared.NewTheme(true))
	m.SetSize(70, 20)
	if msg := m.columnWidth(colMessage); msg != 0 {
		t.Errorf("message column = %d at width 70, want it dropped", msg)
	}
}
