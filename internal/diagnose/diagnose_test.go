package diagnose_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/detail"
)

// The rules are tested through the input the detail pane builds, so the
// tree they walk is the one the Nodes tab and the Timeline draw.

// demo returns the demo workflow named name and the clock it was built at.
func demo(t *testing.T, name string) (core.Workflow, *testkit.FakeReader) {
	t.Helper()
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	for ref, wf := range r.Workflows {
		if ref.Name == name {
			return wf, r
		}
	}
	t.Fatalf("no demo workflow %q", name)
	return core.Workflow{}, nil
}

// explainDemo explains a demo workflow with its first failing pod's log
// read in full from the demo backend, as the live section does.
func explainDemo(t *testing.T, name string) diagnose.Report {
	t.Helper()
	wf, r := demo(t, name)
	in := detail.ExplainInput(wf, testkit.FixtureEpoch)
	first := diagnose.Explain(in)
	if first.LogPod != "" {
		var lines []string
		for _, rec := range r.PodLogs[first.LogPod] {
			lines = append(lines, rec.Content)
		}
		in.Log = &diagnose.Log{NodeID: first.LogNode, PodName: first.LogPod, Container: "main",
			State: diagnose.LogRead, Lines: lines, Tail: diagnose.LogTail}
	}
	return diagnose.Explain(in)
}

// key is a finding as "severity rule cause", the part a test pins exactly.
func key(f diagnose.Finding) string {
	return strings.TrimSpace(f.Severity.String() + " " + f.Rule + " " + f.Cause)
}

func keys(r diagnose.Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, key(f))
	}
	return out
}

// evidence is the finding's evidence as "label: text" lines.
func evidence(f diagnose.Finding) string {
	var b strings.Builder
	for _, ev := range f.Evidence {
		b.WriteString(ev.Label + ": " + ev.Text + "\n")
		for _, l := range ev.Log {
			b.WriteString("  " + l.Text + "\n")
		}
	}
	return b.String()
}

// Every demo workflow gets an explanation that fits it: the findings, in
// order, and what their headlines, evidence and next steps must say.
func TestExplainTheDemo(t *testing.T) {
	cases := []struct {
		name     string
		keys     []string
		headline []string
		evidence []string
		next     []string
		logPod   bool
	}{
		{
			name:     "demo-hello-world",
			keys:     []string{"info succeeded"},
			headline: []string{"Succeeded in 4m00s"},
			evidence: []string{"ran: 1 pod succeeded"},
		},
		{
			name: "demo-nightly-report",
			keys: []string{"error root-failure exit-code", "info dependents", "info exit-handler"},
			headline: []string{
				"transform failed all 3 attempts: exit code 1 each time",
				"1 node did not run because of the failure: load",
				"The exit handler ran and succeeded",
			},
			evidence: []string{
				"attempts: transform(0) failed exit 1 after 40s · transform(1) failed exit 1 after 45s · transform(2) failed exit 1 after 50s",
				"exit code: 1: the program reported a general error",
				"when: failed 3m34s into the run, after running 50s",
				"SchemaViolation: revenue_eur: expected non-null",
				"omitted: load, waits for transform (depends condition not met)",
			},
			next:   []string{"Retrying did not help", "retry the workflow (a, r)"},
			logPod: true,
		},
		{
			name:     "demo-train-pipeline",
			keys:     []string{"info running"},
			headline: []string{"Running for 4m00s of an estimated 9m00s"},
			evidence: []string{
				"running: train-shard(2:2) 2m40s, train-shard(3:3) 2m40s, train-shard(4:4) 2m40s",
				"waiting: train-shard(5:5), evaluate",
				"past estimate: train-shard(2:2) 2m40s of ~2m00s",
			},
		},
		{
			name:     "demo-data-pull",
			keys:     []string{"info running"},
			headline: []string{"Running for 40m00s of an estimated 55m00s"},
			evidence: []string{"running: pull(us) 11m49s, pull(apac) 11m49s"},
		},
		{
			name:     "demo-cleanup",
			keys:     []string{"info not-started"},
			headline: []string{"Not started: submitted 1m00s ago"},
			next:     []string{"workflow controller is running"},
		},
		{
			name:     "demo-release-gate",
			keys:     []string{"info suspended", "info running"},
			headline: []string{"Waiting for a person at approve-production for 4m50s", "Running for 12m00s"},
			evidence: []string{"since: 7m10s into the run"},
			next:     []string{"Resume it (a, u)"},
		},
		{
			name:     "demo-oom-backfill",
			keys:     []string{"error root-failure oom"},
			headline: []string{"backfill-2026-q2 failed: out of memory (OOMKilled, exit code 137)"},
			evidence: []string{"WARN heap usage 1.9Gi of 2Gi limit", "Killed", "pod: demo-oom-backfill-backfill-quarter-"},
			next:     []string{"Raise the memory limit of template backfill-quarter"},
			logPod:   true,
		},
		{
			name:     "demo-deploy-multi-layer",
			keys:     []string{"info succeeded"},
			headline: []string{"Succeeded in 11m00s"},
			evidence: []string{"ran: 5 pods succeeded, 16 nodes skipped by their conditions"},
		},
		{
			name:     "demo-param-check",
			keys:     []string{"error validation"},
			headline: []string{"failed validation before any node ran: {{inputs.parameters.bucket}} did not resolve"},
			evidence: []string{"message: invalid spec:", "unresolved: {{inputs.parameters.bucket}}"},
			next:     []string{"argo lint"},
		},
		{
			name:     "demo-etl-hourly-1790000000",
			keys:     []string{"error root-failure exit-code"},
			headline: []string{"quality-check failed: exit code 2"},
			evidence: []string{"FAIL expect_row_count_between(1e5, 5e5): got 41210", "exit code: 2: a usage error or a failed check"},
			logPod:   true,
		},
		{
			name:     "demo-etl-hourly-1789996400",
			keys:     []string{"info succeeded"},
			headline: []string{"Succeeded in 6m00s"},
		},
		{
			name:     "demo-etl-hourly-1789992800",
			keys:     []string{"info succeeded"},
			headline: []string{"Succeeded in 6m00s"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := explainDemo(t, tc.name)
			if got := strings.Join(keys(r), " | "); got != strings.Join(tc.keys, " | ") {
				t.Fatalf("findings = %s\nwant       %s", got, strings.Join(tc.keys, " | "))
			}
			var heads, evs, nexts strings.Builder
			for _, f := range r.Findings {
				heads.WriteString(f.Headline + "\n")
				evs.WriteString(evidence(f))
				nexts.WriteString(f.Next + "\n")
				if f.Next == "" {
					t.Errorf("%s has no next step", key(f))
				}
			}
			for i, h := range tc.headline {
				if !strings.Contains(r.Findings[min(i, len(r.Findings)-1)].Headline, h) && !strings.Contains(heads.String(), h) {
					t.Errorf("no headline says %q:\n%s", h, heads.String())
				}
			}
			for _, e := range tc.evidence {
				if !strings.Contains(evs.String(), e) {
					t.Errorf("no evidence says %q:\n%s", e, evs.String())
				}
			}
			for _, n := range tc.next {
				if !strings.Contains(nexts.String(), n) {
					t.Errorf("no next step says %q:\n%s", n, nexts.String())
				}
			}
			if (r.LogPod != "") != tc.logPod {
				t.Errorf("log pod = %q, want one: %v", r.LogPod, tc.logPod)
			}
		})
	}
}

// The first failure's log target is the final attempt of a Retry: the one
// whose failure decided the outcome.
func TestLogTargetIsTheLastAttempt(t *testing.T) {
	wf, _ := demo(t, "demo-nightly-report")
	r := diagnose.Explain(detail.ExplainInput(wf, testkit.FixtureEpoch))
	if got := wf.Nodes[r.LogNode].DisplayName; got != "transform(2)" {
		t.Fatalf("log node = %q, want transform(2)", got)
	}
	if r.LogPod != wf.Nodes[r.LogNode].PodName {
		t.Fatalf("log pod %q is not the node's pod %q", r.LogPod, wf.Nodes[r.LogNode].PodName)
	}
}

// Findings come most severe first, and the same input always gives the
// same report.
func TestExplainOrdersBySeverityAndIsDeterministic(t *testing.T) {
	b := newWF("Failed")
	b.task("a", "Succeeded", ran(0, 10), exit("0"))
	b.task("b", "Failed", ran(10, 5), exit("1"), after("a"))
	b.retry("c", "Succeeded", ran(0, 40), attempt("Failed", ran(0, 10), exit("137")), attempt("Succeeded", ran(15, 20), exit("0")))
	in := b.input()
	first := diagnose.Explain(in)
	for i := 0; i < 20; i++ {
		again := diagnose.Explain(b.input())
		if strings.Join(keys(again), "|") != strings.Join(keys(first), "|") {
			t.Fatalf("run %d: %v, first %v", i, keys(again), keys(first))
		}
	}
	for i := 1; i < len(first.Findings); i++ {
		if first.Findings[i].Severity > first.Findings[i-1].Severity {
			t.Fatalf("finding %d is more severe than the one before: %v", i, keys(first))
		}
	}
	if got := strings.Join(keys(first), " | "); got != "error root-failure exit-code | warning retried" {
		t.Fatalf("findings = %s", got)
	}
}

// explainOne is the report for a synthetic workflow.
func explainOne(b *wfBuilder) diagnose.Report { return diagnose.Explain(b.input()) }

func findRule(t *testing.T, r diagnose.Report, rule string) diagnose.Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Rule == rule {
			return f
		}
	}
	t.Fatalf("no %s finding in %v", rule, keys(r))
	return diagnose.Finding{}
}

// Each well-known exit code is named with what it means and what to do.
func TestExitCodes(t *testing.T) {
	cases := map[string]string{
		"1":   "general error",
		"2":   "usage error or a failed check",
		"126": "could not be executed",
		"127": "was not found",
		"137": "SIGKILL",
		"139": "segmentation fault",
		"143": "SIGTERM",
		"134": "killed by signal 6",
		"42":  "the program's own exit status",
	}
	for code, want := range cases {
		b := newWF("Failed")
		b.task("job", "Failed", ran(0, 30), exit(code), msg("Error (exit code "+code+")"))
		f := findRule(t, explainOne(b), diagnose.RuleRootFailure)
		if f.Cause != diagnose.CauseExitCode {
			t.Errorf("exit %s: cause %q", code, f.Cause)
		}
		if !strings.Contains(f.Headline, "exit code "+code) {
			t.Errorf("exit %s: headline %q", code, f.Headline)
		}
		if !strings.Contains(evidence(f), "exit code: "+code+": ") || !strings.Contains(evidence(f), want) {
			t.Errorf("exit %s: evidence lacks %q:\n%s", code, want, evidence(f))
		}
		if f.Next == "" {
			t.Errorf("exit %s: no next step", code)
		}
	}
}

// The message outranks the exit code: OOMKilled names the kill that 137
// alone does not, and a pull failure or a deadline names its own cause.
func TestCauseFromTheMessage(t *testing.T) {
	cases := []struct {
		exit, msg, cause, headline string
	}{
		{"137", "OOMKilled (exit code 137)", diagnose.CauseOOM, "out of memory"},
		{"137", "", diagnose.CauseExitCode, "exit code 137"},
		{"", "ErrImagePull: rpc error: pull access denied for registry.example/app", diagnose.CauseImagePull, "could not pull its image (ErrImagePull)"},
		{"", "ImagePullBackOff: Back-off pulling image \"registry.example/app:9\"", diagnose.CauseImagePull, "(ImagePullBackOff)"},
		{"", "Pod was active on the node longer than the specified deadline", diagnose.CauseDeadline, "deadline exceeded"},
		{"143", "Step exceeded its deadline", diagnose.CauseDeadline, "deadline exceeded"},
		{"", "Unschedulable: 0/3 nodes are available: 3 Insufficient cpu.", diagnose.CausePodPending, "its pod never started (Unschedulable)"},
		{"", "failed to save outputs: key not found", diagnose.CauseMessage, "failed to save outputs"},
	}
	for _, tc := range cases {
		b := newWF("Failed")
		b.task("job", "Error", ran(0, 30), exit(tc.exit), msg(tc.msg))
		f := findRule(t, explainOne(b), diagnose.RuleRootFailure)
		if f.Cause != tc.cause || !strings.Contains(f.Headline, tc.headline) {
			t.Errorf("exit %q message %q: cause %q headline %q; want %q, %q", tc.exit, tc.msg, f.Cause, f.Headline, tc.cause, tc.headline)
		}
	}
}

// The root failure is the first node to fail on its own, not the groups
// that failed because of it, and not a node that ran after it failed.
func TestRootFailureIsTheFirstToFail(t *testing.T) {
	b := newWF("Failed")
	b.task("early", "Failed", ran(0, 10), exit("1"))
	b.task("late", "Failed", ran(0, 40), exit("2"))
	b.task("handler", "Failed", ran(15, 5), exit("3"), after("early"))
	r := explainOne(b)
	var roots []string
	for _, f := range r.Findings {
		if f.Rule == diagnose.RuleRootFailure {
			roots = append(roots, f.Headline)
		}
	}
	if len(roots) != 2 || !strings.HasPrefix(roots[0], "early failed") || !strings.HasPrefix(roots[1], "late failed") {
		t.Fatalf("root failures = %q", roots)
	}
	if !strings.Contains(evidence(r.Findings[0]), "the first failure") {
		t.Errorf("the first root does not say it came first:\n%s", evidence(r.Findings[0]))
	}
	dep := findRule(t, r, diagnose.RuleDependents)
	if !strings.Contains(evidence(dep), "failed after: handler, waits for early") {
		t.Errorf("the failure that followed is not listed as following:\n%s", evidence(dep))
	}
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Headline, "wf failed") {
			t.Errorf("the DAG that failed because of its tasks is a root: %q", f.Headline)
		}
	}
}

// Past three root failures, the rest are counted in one finding.
func TestManyRootFailuresAreCounted(t *testing.T) {
	b := newWF("Failed")
	for i, n := range []string{"s1", "s2", "s3", "s4", "s5"} {
		b.task(n, "Failed", ran(0, float64(10+i)), exit("1"))
	}
	r := explainOne(b)
	if got := strings.Join(keys(r), " | "); got != "error root-failure exit-code | error root-failure exit-code | error root-failure exit-code | error more-failures" {
		t.Fatalf("findings = %s", got)
	}
	more := findRule(t, r, diagnose.RuleMoreFailures)
	if more.Headline != "2 more nodes failed on their own" || !strings.Contains(evidence(more), "s4, s5") {
		t.Errorf("more = %q\n%s", more.Headline, evidence(more))
	}
}

// Retries: attempts that failed alike say so, attempts that failed in
// different ways say that, a Retry still going is a warning, and one that
// got there in the end is a flaky step.
func TestRetries(t *testing.T) {
	differ := newWF("Failed")
	differ.retry("fetch", "Failed", ran(0, 90),
		attempt("Failed", ran(0, 20), exit("1"), msg("Error (exit code 1)")),
		attempt("Failed", ran(30, 20), exit("137"), msg("Error (exit code 137)")))
	f := findRule(t, explainOne(differ), diagnose.RuleRootFailure)
	if !strings.Contains(f.Headline, "fetch failed 2 attempts in different ways: exit codes 1, 137") ||
		!strings.Contains(f.Next, "transient") {
		t.Errorf("different attempts: %q / %q", f.Headline, f.Next)
	}

	going := newWF("Running")
	going.retry("fetch", "Running", ran(0, -1),
		attempt("Failed", ran(0, 20), exit("1")),
		attempt("Running", ran(30, -1)))
	f = findRule(t, explainOne(going), diagnose.RuleRootFailure)
	if f.Severity != diagnose.Warning || !strings.Contains(f.Headline, "failed 1 attempt so far and is retrying") {
		t.Errorf("retrying: %s %q", f.Severity, f.Headline)
	}

	flaky := newWF("Succeeded")
	flaky.retry("fetch", "Succeeded", ran(0, 60),
		attempt("Failed", ran(0, 20), exit("1"), msg("connection reset")),
		attempt("Succeeded", ran(30, 20), exit("0")))
	r := explainOne(flaky)
	if got := strings.Join(keys(r), " | "); got != "warning retried | info succeeded" {
		t.Fatalf("flaky findings = %s", got)
	}
	f = findRule(t, r, diagnose.RuleRetried)
	if !strings.Contains(f.Headline, "fetch succeeded on attempt 2 of 2 after failing 1 time") ||
		!strings.Contains(evidence(f), "last failure: connection reset") {
		t.Errorf("flaky: %q\n%s", f.Headline, evidence(f))
	}
	if s := findRule(t, r, diagnose.RuleSucceeded); !strings.Contains(s.Headline, "with 1 retried step") {
		t.Errorf("succeeded headline %q", s.Headline)
	}
}

// A failed exit handler is an error of its own; a hook that failed is
// reported, and one that succeeded is not.
func TestExitHandlerAndHooks(t *testing.T) {
	b := newWF("Succeeded")
	b.task("work", "Succeeded", ran(0, 10), exit("0"))
	b.exitHandler("Failed", ran(12, 3), exit("127"), msg("Error (exit code 127)"))
	r := explainOne(b)
	f := findRule(t, r, diagnose.RuleExitHandler)
	if f.Severity != diagnose.Error || !strings.Contains(f.Headline, "The exit handler failed: exit code 127") {
		t.Errorf("exit handler: %s %q", f.Severity, f.Headline)
	}
	if !strings.Contains(evidence(f), "was not found") {
		t.Errorf("exit handler evidence:\n%s", evidence(f))
	}
	if strings.Contains(strings.Join(keys(r), "|"), "root-failure") {
		t.Errorf("the exit handler counted as a root failure: %v", keys(r))
	}

	h := newWF("Succeeded")
	h.task("work", "Succeeded", ran(0, 10), exit("0"))
	h.hook("slack", "Failed", ran(11, 2), exit("1"))
	h.hook("audit", "Succeeded", ran(11, 2), exit("0"))
	r = explainOne(h)
	if got := strings.Join(keys(r), " | "); got != "error hook exit-code | info succeeded" {
		t.Fatalf("hook findings = %s", got)
	}
}

// Nodes that did not run because of a failure are listed: omitted ones,
// skipped ones whose reason is not their own condition, and nodes a finished
// run never started. A node its own condition skipped is not.
func TestDependentsOfAFailure(t *testing.T) {
	b := newWF("Failed")
	b.task("build", "Failed", ran(0, 10), exit("1"))
	b.task("test", "Omitted", notRun(), msg("omitted: depends condition not met"), after("build"))
	b.task("deploy", "Skipped", notRun(), msg("when 'false' evaluated false"))
	b.task("notify", "Pending", notRun(), after("test"))
	r := explainOne(b)
	f := findRule(t, r, diagnose.RuleDependents)
	ev := evidence(f)
	for _, want := range []string{"omitted: test, waits for build (depends condition not met)", "not started: notify, waits for test"} {
		if !strings.Contains(ev, want) {
			t.Errorf("dependents lack %q:\n%s", want, ev)
		}
	}
	if strings.Contains(ev, "deploy") || f.Headline != "2 nodes did not run because of the failure: test, notify" {
		t.Errorf("dependents = %q\n%s", f.Headline, ev)
	}
}

// A workflow its deadline stopped says so, with the spec's deadline, when
// no failing node already carries that cause.
func TestWorkflowDeadline(t *testing.T) {
	b := newWF("Failed")
	b.wf.Summary.Message = "Step exceeded its deadline"
	b.wf.Resource = []byte(`{"spec":{"activeDeadlineSeconds":300}}`)
	b.task("slow", "Failed", ran(0, 300), exit("143"), msg("terminated"))
	r := explainOne(b)
	f := findRule(t, r, diagnose.RuleDeadline)
	if !strings.Contains(evidence(f), "deadline: activeDeadlineSeconds 300") {
		t.Errorf("deadline evidence:\n%s", evidence(f))
	}

	n := newWF("Failed")
	n.wf.Summary.Message = "Step exceeded its deadline"
	n.wf.Resource = []byte(`{"spec":{"activeDeadlineSeconds":300}}`)
	n.task("slow", "Failed", ran(0, 300), msg("Step exceeded its deadline"))
	r = explainOne(n)
	if got := strings.Join(keys(r), " | "); got != "error root-failure deadline" {
		t.Fatalf("a node carrying the deadline gets one finding, got %s", got)
	}
	if !strings.Contains(evidence(r.Findings[0]), "activeDeadlineSeconds 300 on the workflow") {
		t.Errorf("node deadline evidence:\n%s", evidence(r.Findings[0]))
	}
}

// A failed workflow with no failing node, and one with no node at all but
// no message, are still explained.
func TestFailedWithoutAFailingNode(t *testing.T) {
	b := newWF("Failed")
	b.wf.Summary.Message = "workflow shutdown with strategy: Terminate"
	b.task("a", "Succeeded", ran(0, 10), exit("0"))
	f := findRule(t, explainOne(b), diagnose.RuleWorkflowFailed)
	if f.Headline != "The workflow failed: workflow shutdown with strategy: Terminate" {
		t.Errorf("headline %q", f.Headline)
	}

	v := newWF("Error")
	v.wf.Nodes = map[string]core.Node{}
	f = findRule(t, explainOne(v), diagnose.RuleValidation)
	if !strings.Contains(evidence(f), "the server gave no reason") {
		t.Errorf("validation with no message:\n%s", evidence(f))
	}
}

// Pods an unfinished run cannot start are warnings with the reason
// Kubernetes gave; the same pods in a finished run are its failures.
func TestPodPending(t *testing.T) {
	b := newWF("Running")
	b.task("fetch", "Pending", ran(0, -1), msg("ImagePullBackOff: Back-off pulling image \"registry.example/fetch:9\""))
	b.task("big", "Pending", ran(0, -1), msg("Unschedulable: 0/3 nodes are available: 3 Insufficient memory."))
	r := explainOne(b)
	pend := map[string]diagnose.Finding{}
	for _, f := range r.Findings {
		if f.Rule == diagnose.RulePodPending {
			pend[f.NodeID] = f
		}
	}
	if len(pend) != 2 {
		t.Fatalf("pod-pending findings = %v", keys(r))
	}
	if f := pend["fetch"]; f.Cause != diagnose.CauseImagePull || f.Severity != diagnose.Warning ||
		!strings.Contains(f.Headline, "fetch cannot start: ImagePullBackOff") || !strings.Contains(f.Next, "imagePullSecrets") {
		t.Errorf("image pull: %s %q / %q", f.Severity, f.Headline, f.Next)
	}
	if f := pend["big"]; f.Cause != diagnose.CausePodPending ||
		!strings.Contains(f.Headline, "big cannot start: Unschedulable") || !strings.Contains(f.Next, "requests") {
		t.Errorf("unschedulable: %q / %q", f.Headline, f.Next)
	}
}

// Waiting at a gate for an hour or more is a warning; a workflow suspended
// as a whole says so; a running workflow past its estimate is a warning.
func TestWaitingAndRunningLong(t *testing.T) {
	g := newWF("Running")
	g.task("approve", "Running", ran(0, -1), typ("Suspend"))
	g.now = g.start.Add(3 * time.Hour)
	f := findRule(t, explainOne(g), diagnose.RuleSuspended)
	if f.Severity != diagnose.Warning || !strings.Contains(f.Headline, "for 3h00m, a long wait") {
		t.Errorf("long gate: %s %q", f.Severity, f.Headline)
	}

	s := newWF("Running")
	s.wf.Resource = []byte(`{"spec":{"suspend":true}}`)
	s.task("a", "Succeeded", ran(0, 10), exit("0"))
	findRule(t, explainOne(s), diagnose.RuleWorkflowSuspended)

	o := newWF("Running")
	o.wf.Summary.EstimatedDuration = 5 * time.Minute
	o.task("crunch", "Running", ran(0, -1))
	o.now = o.start.Add(12 * time.Minute)
	f = findRule(t, explainOne(o), diagnose.RuleOverdue)
	if f.Severity != diagnose.Warning || f.Headline != "Running for 12m00s, past its estimate of 5m00s" {
		t.Errorf("overdue: %s %q", f.Severity, f.Headline)
	}
}

// A workflow the controller has not started for five minutes is a warning.
func TestNotStartedForLong(t *testing.T) {
	b := newWF("Pending")
	b.wf.Nodes = map[string]core.Node{}
	b.wf.Summary.StartedAt = nil
	b.wf.Summary.Message = "Workflow processing has been postponed due to max parallelism limit"
	b.now = b.start.Add(20 * time.Minute)
	f := findRule(t, explainOne(b), diagnose.RuleNotStarted)
	if f.Severity != diagnose.Warning || !strings.Contains(evidence(f), "max parallelism limit") {
		t.Errorf("not started: %s\n%s", f.Severity, evidence(f))
	}
}

// Failed nodes in a workflow that succeeded anyway are pointed out.
func TestToleratedFailures(t *testing.T) {
	b := newWF("Succeeded")
	b.task("lint", "Failed", ran(0, 5), exit("1"))
	b.task("build", "Succeeded", ran(0, 20), exit("0"))
	r := explainOne(b)
	if got := strings.Join(keys(r), " | "); got != "warning tolerated | info succeeded" {
		t.Fatalf("findings = %s", got)
	}
}

// The node map the server withheld is a finding, not an empty section.
func TestNodesUnavailable(t *testing.T) {
	b := newWF("Running")
	b.wf.NodesAvailable = false
	b.wf.NodesUnavailableReason = "node status offloaded and not hydrated by the server"
	r := explainOne(b)
	f := findRule(t, r, diagnose.RuleNodesUnavailable)
	if !strings.Contains(evidence(f), "offloaded and not hydrated") {
		t.Errorf("evidence:\n%s", evidence(f))
	}
}

// The log evidence follows the read: a line saying it is being read, the
// picked lines once read, and a finding of its own when the log is gone,
// could not be read, or is empty. A pod with no known name is said to be
// unreadable rather than guessed.
func TestLogEvidence(t *testing.T) {
	b := newWF("Failed")
	b.task("job", "Failed", ran(0, 30), exit("1"), pod())
	in := b.input()
	target := diagnose.Explain(in)
	if target.LogPod != "wf-job" {
		t.Fatalf("log pod = %q", target.LogPod)
	}
	logEv := func(r diagnose.Report) string {
		for _, ev := range r.Findings[0].Evidence {
			if ev.Label == "log" {
				return ev.Text
			}
		}
		return ""
	}

	in.Log = &diagnose.Log{State: diagnose.LogLoading, Tail: diagnose.LogTail}
	if got := logEv(diagnose.Explain(in)); got != "reading the last 200 lines of job's log…" {
		t.Errorf("loading: %q", got)
	}

	in.Log = &diagnose.Log{NodeID: target.LogNode, PodName: "wf-job", State: diagnose.LogRead, Tail: 200,
		Lines: []string{"start", "connecting to db", "fatal: password authentication failed", "bye"}}
	r := diagnose.Explain(in)
	if got := logEv(r); !strings.HasPrefix(got, "picked from the last 4 lines of job's log") {
		t.Errorf("read: %q", got)
	}
	if !strings.Contains(evidence(r.Findings[0]), "fatal: password authentication failed") {
		t.Errorf("the fatal line is not quoted:\n%s", evidence(r.Findings[0]))
	}

	in.Log = &diagnose.Log{NodeID: target.LogNode, PodName: "wf-job", State: diagnose.LogFailed, Tail: 200,
		Err: `pods "wf-job" not found`, Gone: true}
	r = diagnose.Explain(in)
	f := findRule(t, r, diagnose.RuleLog)
	if f.Severity != diagnose.Warning || f.Headline != "The log of job is gone" || !strings.Contains(f.Next, "archiveLogs") {
		t.Errorf("gone: %s %q %q", f.Severity, f.Headline, f.Next)
	}
	if got := logEv(r); got != "could not be read (see below)" {
		t.Errorf("gone, card says %q", got)
	}

	in.Log.Gone, in.Log.Err = false, "connection reset"
	if f := findRule(t, diagnose.Explain(in), diagnose.RuleLog); f.Headline != "The log of job could not be read" {
		t.Errorf("failed: %q", f.Headline)
	}

	in.Log = &diagnose.Log{NodeID: target.LogNode, PodName: "wf-job", State: diagnose.LogRead, Tail: 200}
	if f := findRule(t, diagnose.Explain(in), diagnose.RuleLog); f.Severity != diagnose.Info || f.Headline != "The log of job is empty" {
		t.Errorf("empty: %s %q", f.Severity, f.Headline)
	}

	nameless := newWF("Failed")
	nameless.task("job", "Failed", ran(0, 30), exit("1"))
	nin := nameless.input()
	nin.Log = &diagnose.Log{State: diagnose.LogLoading, Tail: diagnose.LogTail}
	r = diagnose.Explain(nin)
	if r.LogPod != "" || !strings.HasPrefix(logEv(r), "not read: the server did not state its pod naming") {
		t.Errorf("nameless pod: pod %q, log %q", r.LogPod, logEv(r))
	}
}

// Parameter values travel apart from the text, so the view decides whether
// to show them.
func TestInputsAreParameters(t *testing.T) {
	b := newWF("Failed")
	b.task("job", "Failed", ran(0, 30), exit("1"), inputs("token", "s3cr3t"))
	f := findRule(t, explainOne(b), diagnose.RuleRootFailure)
	for _, ev := range f.Evidence {
		if strings.Contains(ev.Text, "s3cr3t") {
			t.Fatalf("a parameter value is in the text: %q", ev.Text)
		}
		if ev.Label == "inputs" && (len(ev.Params) != 1 || ev.Params[0].Value != "s3cr3t") {
			t.Fatalf("inputs = %+v", ev.Params)
		}
	}
}
