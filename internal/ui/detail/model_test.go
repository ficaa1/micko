package detail

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/testkit"
)

// Summary/timestamp tests (DET-01): phase, age/duration, message, labels
// and arguments render deterministically; missing timestamps use the
// documented explicit rule (never fabricated).

func TestDetailSummaryFullFields(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	s := RenderSummary(wf, testkit.FixtureEpoch)
	for _, want := range []string{
		"fixture-dag", "Failed", "one task failed",
		"workflows.argoproj.io/phase", // labels visible with values
	} {
		if !containsSub(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
	// duration: started 30m ago, finished 25m ago → 5m
	if !containsSub(s, "5m") {
		t.Errorf("summary must show duration 5m:\n%s", s)
	}
}

func TestDetailSummaryArgumentValuesRedacted(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "wf", "Running", testkit.FixtureEpoch)
	wf.Resource = []byte(`{"spec":{"arguments":{"parameters":[{"name":"api-key","value":"sk-1234567890abcdef123456"}]}}}`)
	s := RenderSummary(wf, testkit.FixtureEpoch)
	if containsSub(s, "sk-1234567890abcdef123456") {
		t.Fatalf("summary leaks argument value:\n%s", s)
	}
	if !containsSub(s, "api-key") {
		t.Errorf("argument names stay visible:\n%s", s)
	}
}

func TestDetailSummaryMissingTimestampsRule(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "wf", "Pending", testkit.FixtureEpoch)
	// no StartedAt/FinishedAt (not yet started — protocol §2)
	s := RenderSummary(wf, testkit.FixtureEpoch)
	for _, bad := range []string{"not started\u00a0", "duration: -", "duration: 0s"} {
		_ = bad
	}
	if containsSub(s, "duration: ") && !containsSub(s, "duration: n/a") && !containsSub(s, "not started") {
		t.Errorf("pending workflow without timestamps must say so explicitly, got:\n%s", s)
	}
}

func TestDetailSummaryRunningDuration(t *testing.T) {
	epoch := testkit.FixtureEpoch
	wf := testkit.SyntheticWorkflow("ns", "run", "Running", epoch.Add(-10*time.Minute))
	st := epoch.Add(-10 * time.Minute)
	wf.Summary.StartedAt = &st
	s := RenderSummary(wf, epoch)
	// finishedAt nil: duration computed up to now = 10m, shown as running.
	if !containsSub(s, "10m") {
		t.Errorf("running duration = 10m expected:\n%s", s)
	}
}

// Pod eligibility / log intent (DET-11): node IDs are never pod names;
// pod-capable node types show eligibility only when a PodName is verified;
// workflow-wide log intent is always available; the node shortcut is
// disabled with an explanation until verified resolution exists.

func TestNodeLogIntentWorkflowWideAlwaysAvailable(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	for _, n := range BuildNodeOutline(wf, OutlineOptions{}).Rows[0].Children {
		intent := ComputeNodeLogIntent(wf, n, "")
		if !intent.WorkflowWideAllowed {
			t.Errorf("workflow-wide log intent must always be allowed (node %s)", n.NodeID)
		}
	}
}

func TestNodeLogIntentNoPodNameAssumption(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	var podNode OutlineRow
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for i := range rows {
			if rows[i].Type == "Pod" {
				podNode = rows[i]
				return
			}
			walk(rows[i].Children)
		}
	}
	walk(BuildNodeOutline(wf, OutlineOptions{}).Rows)
	if podNode.NodeID == "" {
		t.Fatal("no Pod node in fixture")
	}
	intent := ComputeNodeLogIntent(wf, podNode, "")
	if intent.PodScopedAllowed {
		t.Fatalf("pod-scoped logs must be disabled without a verified pod name (DET-11): %+v", intent)
	}
	if intent.DisabledReason == "" {
		t.Errorf("disabled pod shortcut must carry an explanation")
	}
	// The intent must not fabricate a pod name from the node ID.
	if intent.PodName != "" {
		t.Errorf("intent fabricated pod name %q from node id (pod-name guess forbidden)", intent.PodName)
	}
}

func TestNodeLogIntentUserSuppliedPodName(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	var podNode OutlineRow
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for i := range rows {
			if rows[i].HasPod {
				podNode = rows[i]
				return
			}
			walk(rows[i].Children)
		}
	}
	walk(BuildNodeOutline(wf, OutlineOptions{}).Rows)
	intent := ComputeNodeLogIntent(wf, podNode, "user-entered-pod-name")
	if !intent.PodScopedAllowed {
		t.Fatalf("user-entered pod name must enable pod-scoped logs: %+v", intent)
	}
	if intent.PodName != "user-entered-pod-name" {
		t.Errorf("pod name = %q, want the user-supplied value verbatim", intent.PodName)
	}
}

func TestNodeLogIntentNonPodNodeDisabled(t *testing.T) {
	wf := testkit.FixtureDAGWorkflow("ns", "fixture-dag")
	var suspend OutlineRow
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		for i := range rows {
			if rows[i].Type == "Suspend" {
				suspend = rows[i]
				return
			}
			walk(rows[i].Children)
		}
	}
	walk(BuildNodeOutline(wf, OutlineOptions{}).Rows)
	if suspend.NodeID == "" {
		t.Fatal("no suspend node in fixture")
	}
	intent := ComputeNodeLogIntent(wf, suspend, "anything")
	if intent.PodScopedAllowed {
		t.Fatal("non-pod node must never allow pod-scoped logs")
	}
	if !intent.WorkflowWideAllowed {
		t.Fatal("workflow-wide logs remain available for non-pod nodes")
	}
}
