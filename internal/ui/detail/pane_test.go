package detail

import (
	"strings"
	"testing"

	"argo-tui/internal/testkit"
)

// loadedModel is a detail pane with a workflow applied at a known size.
func loadedModel(t *testing.T) *Model {
	t.Helper()
	m := New()
	m.SetWorkflow(resourceFixture(), testkit.FixtureEpoch)
	m.SetSize(80, 20)
	return m
}

// The shell draws the workflow name in the border, so the body must not
// repeat it as its own heading row.
func TestDetailBodyLinesExcludeTheTitle(t *testing.T) {
	m := loadedModel(t)
	if !strings.Contains(m.PaneTitle(), m.state.Summary.Ref.Name) {
		t.Fatalf("pane title lost the workflow name: %q", m.PaneTitle())
	}
	body := m.BodyLines()
	if strings.HasPrefix(body[0], "DETAIL ") {
		t.Fatalf("body repeats the pane title: %q", body[0])
	}
}

// The tab strip must survive: it is the only thing telling a reader which
// of the three sections is on screen and that tab cycles them.
func TestDetailBodyLinesKeepTheTabStrip(t *testing.T) {
	body := strings.Join(loadedModel(t).BodyLines(), "\n")
	for _, tab := range []string{"Summary", "Nodes", "Resource"} {
		if !strings.Contains(body, tab) {
			t.Fatalf("tab %q missing from the body:\n%s", tab, body)
		}
	}
}

// The active tab must be marked, or the strip says nothing about state.
func TestActiveTabIsMarked(t *testing.T) {
	m := loadedModel(t)
	first := strings.Join(m.BodyLines(), "\n")
	m.tab = "nodes"
	second := strings.Join(m.BodyLines(), "\n")
	if first == second {
		t.Fatal("the tab strip renders identically for summary and nodes")
	}
}

// The body must fit the height budget SetSize gave it, or the shell clips
// content the pane believes is visible.
func TestDetailBodyLinesFitTheHeightBudget(t *testing.T) {
	for _, h := range []int{3, 6, 20} {
		m := loadedModel(t)
		m.SetSize(80, h)
		m.tab = "resource"
		if got := len(m.BodyLines()); got > h {
			t.Fatalf("height %d: body is %d lines", h, got)
		}
	}
}

// The states that are not a loaded workflow must still produce a body, or
// the pane renders empty with no explanation.
func TestDetailBodyLinesCoverTheNonLoadedStates(t *testing.T) {
	cases := map[string]func(*Model){
		"loading":  func(m *Model) { m.loading = true },
		"notFound": func(m *Model) { m.notFound = true },
		"error":    func(m *Model) { m.lastErr = "boom" },
	}
	for name, setup := range cases {
		m := loadedModel(t)
		setup(m)
		body := strings.Join(m.BodyLines(), "\n")
		if strings.TrimSpace(body) == "" {
			t.Fatalf("state %q rendered an empty body", name)
		}
	}
}

// Hints must name the keys the help overlay documents for detail.
func TestDetailHintsAdvertiseTheDocumentedKeys(t *testing.T) {
	h := loadedModel(t).Hints()
	for _, key := range []string{"tab", "esc"} {
		if !strings.Contains(h, key) {
			t.Fatalf("hints %q omit key %q", h, key)
		}
	}
}
