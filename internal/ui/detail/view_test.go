package detail

import (
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/testkit"
)

// View tests: the composed detail view (summary | nodes | resource tabs)
// renders deterministically for goldens; unavailable/loading/error states
// are distinguishable.

func TestDetailViewStateUnavailable(t *testing.T) {
	wf := testkit.FixtureOffloadedWorkflow("ns", "fixture-offloaded")
	out := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	s := RenderDetail(out, "nodes")
	if !containsSub(s, "node status unavailable") {
		t.Fatalf("offloaded workflow must show unavailable state:\n%s", s)
	}
	if !containsSub(s, "offloaded and not hydrated") {
		t.Fatalf("unavailable reason must be shown:\n%s", s)
	}
	// Must NOT look like an empty workflow.
	if containsSub(s, "(no nodes yet") {
		t.Fatalf("offloaded state must not render as empty workflow:\n%s", s)
	}
}

func TestDetailViewStateEmptyStarted(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "pending", "Pending", testkit.FixtureEpoch)
	out := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	s := RenderDetail(out, "nodes")
	if !containsSub(s, "(no nodes yet") {
		t.Fatalf("empty-but-available must render the explicit empty state:\n%s", s)
	}
}

func TestDetailViewStateDeterministic(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	a := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	b := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	if RenderDetail(a, "summary|nodes|resource") != RenderDetail(b, "summary|nodes|resource") {
		t.Fatal("detail view must be deterministic for identical inputs")
	}
}

func TestDetailViewStateTabSelection(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	state := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	summaryOnly := RenderDetail(state, "summary")
	if containsSub(summaryOnly, "task-a") {
		t.Errorf("summary tab must not include node rows:\n%s", summaryOnly)
	}
	nodesTab := RenderDetail(state, "nodes")
	if !containsSub(nodesTab, "task-a") {
		t.Errorf("nodes tab must include node rows:\n%s", nodesTab)
	}
	resourceTab := RenderDetail(state, "resource")
	if containsSub(resourceTab, "task-a") {
		t.Errorf("resource tab must not include node rows:\n%s", resourceTab)
	}
}

// TestDetailViewStateSanitization pins sanitization at the composed-view level:
// untrusted fields (message, labels) cannot smuggle control sequences into
// the terminal output.
func TestDetailViewStateSanitization(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "wf", "Failed", testkit.FixtureEpoch)
	wf.Summary.Message = "boom \x1b[31mor owned\x1b[0m"
	wf.Summary.Labels = map[string]string{"evil": "x\x07y"}
	state := DetailViewStateFromWorkflow(wf, testkit.FixtureEpoch)
	s := RenderDetail(state, "summary")
	for _, bad := range []byte{0x1b, 0x07} {
		if strings.IndexByte(s, bad) >= 0 {
			t.Errorf("composed view contains raw byte 0x%02x:\n%s", bad, s)
		}
	}
	// The text itself survives (letters still present).
	if !containsSub(s, "or owned") {
		t.Errorf("message text must survive sanitization:\n%s", s)
	}
}
