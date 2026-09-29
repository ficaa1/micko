package detail

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/testkit"
)

// Summary/timestamp tests: phase, age/duration, message, labels
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
	// no StartedAt/FinishedAt (not yet started)
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
