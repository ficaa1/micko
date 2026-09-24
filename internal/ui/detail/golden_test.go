package detail

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// updateGolden regenerates golden files when -update is passed
// (conventional golden-test flag). UPDATE_GOLDEN=1 does the same, as it
// does in the other view packages.
var updateGolden = flag.Bool("update", false, "rewrite golden files")

// TestDetailGoldenFixtures pins deterministic rendering for the canonical
// fixtures: DAG (retry/suspend), offloaded, steps. Summary and nodes tabs
// both pinned; the resource tab content is fixture JSON-derived (already
// pinned by resource tests) and stays out of these goldens to avoid churn
// from fixture JSON cosmetics.
func TestDetailGoldenFixtures(t *testing.T) {
	epoch := testkit.FixtureEpoch
	cases := map[string]core.Workflow{
		"dag":       testkit.FixtureDAGWorkflow("ns", "fixture-dag"),
		"offloaded": testkit.FixtureOffloadedWorkflow("ns", "fixture-offloaded"),
		"steps":     stepsFixture(),
	}
	for name, wf := range cases {
		state := DetailViewStateFromWorkflow(wf, epoch)
		compareGolden(t, filepath.Join("testdata", name+".summary.golden"),
			RenderDetail(state, "summary"))
		compareGolden(t, filepath.Join("testdata", name+".nodes.golden"),
			RenderDetail(state, "nodes"))
	}
}

// compareGolden compares (and with -update, rewrites) a golden file.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	if *updateGolden || os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(name, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("golden %s missing (run with -update to seed): %v", name, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
