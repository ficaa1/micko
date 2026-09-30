package diagnose

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// key is a finding as "severity rule cause", the part a test pins exactly.
func key(f Finding) string {
	return strings.TrimSpace(f.Severity.String() + " " + f.Rule + " " + f.Cause)
}

func keys(r Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, key(f))
	}
	return out
}

// evidence is the finding's evidence as "label: text" lines.
func evidence(f Finding) string {
	var b strings.Builder
	for _, ev := range f.Evidence {
		b.WriteString(ev.Label + ": " + ev.Text + "\n")
		for _, l := range ev.Log {
			b.WriteString("  " + l.Text + "\n")
		}
	}
	return b.String()
}

// explainOne is the report for a synthetic workflow.
func explainOne(b *wfBuilder) Report { return Explain(b.input()) }

func findRule(t *testing.T, r Report, rule string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Rule == rule {
			return f
		}
	}
	t.Fatalf("no %s finding in %v", rule, keys(r))
	return Finding{}
}

// Successful runs report their duration and the work that completed or was skipped.
func TestExplainSucceeded(t *testing.T) {
	for _, c := range []struct {
		name    string
		skipped int
		want    string
	}{
		{"one pod", 0, "1 pod succeeded"},
		{"skipped conditions", 2, "1 pod succeeded, 2 nodes skipped by their conditions"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Succeeded")
			b.task("job", "Succeeded", ran(0, 10), exit("0"))
			for i := 0; i < c.skipped; i++ {
				b.task(fmt.Sprintf("skip-%d", i), "Skipped", msg("when 'false' evaluated false"))
			}
			r := explainOne(b)
			if got := strings.Join(keys(r), " | "); got != "info succeeded" {
				t.Fatalf("findings = %q, want info succeeded", got)
			}
			f := findRule(t, r, RuleSucceeded)
			if f.Headline != "Succeeded in 10m00s" || !strings.Contains(evidence(f), "ran: "+c.want) {
				t.Fatalf("success = %q\n%s, want duration 10m00s and %q", f.Headline, evidence(f), c.want)
			}
		})
	}
}

// Rejected specs name unresolved inputs or the missing reason without inventing nodes or a log target.
func TestExplainValidation(t *testing.T) {
	for _, c := range []struct{ name, message, headline, evidence string }{
		{"unresolved input", "invalid spec:\n{{inputs.parameters.bucket}} did not resolve", "failed validation before any node ran: {{inputs.parameters.bucket}} did not resolve", "unresolved: {{inputs.parameters.bucket}}"},
		{"no reason", "", "failed before any node ran: the controller rejected it", "the server gave no reason"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Error")
			b.wf.Nodes = nil
			b.wf.Summary.Message = c.message
			r := explainOne(b)
			f := findRule(t, r, RuleValidation)
			if !strings.Contains(f.Headline, c.headline) || !strings.Contains(evidence(f), c.evidence) || !strings.Contains(f.Next, "argo lint") || r.LogNode != "" || r.LogPod != "" {
				t.Fatalf("validation = %+v, log target %q/%q, want headline containing %q and evidence containing %q", f, r.LogNode, r.LogPod, c.headline, c.evidence)
			}
			if c.message != "" && !strings.Contains(evidence(f), "message: invalid spec: …") {
				t.Fatalf("validation lacks first message line:\n%s", evidence(f))
			}
		})
	}
}

// Findings come most severe first and identical input produces the same complete report.
func TestExplainOrdersBySeverityAndIsDeterministic(t *testing.T) {
	b := newWF("Failed")
	b.task("a", "Succeeded", ran(0, 10), exit("0"))
	b.task("b", "Failed", ran(10, 5), exit("1"), after("a"))
	b.task("omitted", "Omitted", notRun(), after("b"))
	b.exitHandler("Failed", ran(40, 10), exit("127"))
	b.retry("c", "Succeeded", ran(0, 40), attempt("Failed", ran(0, 10), exit("137")), attempt("Succeeded", ran(15, 20), exit("0")))
	in := b.input()
	first := Explain(in)
	for i := 0; i < 20; i++ {
		again := Explain(b.input())
		if !reflect.DeepEqual(again, first) {
			t.Fatalf("run %d report = %+v, want %+v", i, again, first)
		}
	}
	if got := strings.Join(keys(first), " | "); got != "error root-failure exit-code | error exit-handler exit-code | warning retried | info dependents" {
		t.Fatalf("findings = %q, want root error, handler error, retry warning, dependent info", got)
	}
}

// Failure causes prefer the recorded message and explain the exit code when it is the only cause.
func TestExplainFailureCauses(t *testing.T) {
	for _, c := range []struct {
		name, exit, message, cause, headline, evidence, next string
	}{
		{"general error", "1", "", CauseExitCode, "exit code 1", "exit code: 1: the program reported a general error", "Fix the error"},
		{"usage error", "2", "", CauseExitCode, "exit code 2", "usage error or a failed check", "arguments and inputs"},
		{"not executable", "126", "", CauseExitCode, "exit code 126", "could not be executed", "execute permission"},
		{"not found", "127", "", CauseExitCode, "exit code 127", "was not found", "PATH"},
		{"bare kill", "137", "", CauseExitCode, "exit code 137", "SIGKILL", "eviction or preemption"},
		{"segfault", "139", "", CauseExitCode, "exit code 139", "segmentation fault", "native libraries"},
		{"terminated", "143", "", CauseExitCode, "exit code 143", "SIGTERM", "stopped the workflow"},
		{"other signal", "134", "", CauseExitCode, "exit code 134", "killed by signal 6", "what sent the signal"},
		{"program status", "42", "", CauseExitCode, "exit code 42", "the program's own exit status", "documentation"},
		{"OOM outranks code", "137", "OOMKilled (exit code 137)", CauseOOM, "out of memory", "the container used more memory than its limit", "memory limit of template job"},
		{"pull failure", "", "ErrImagePull: rpc error: pull access denied", CauseImagePull, "could not pull its image (ErrImagePull)", "the kubelet could not pull", "imagePullSecrets"},
		{"pull backoff", "", "ImagePullBackOff: Back-off pulling image", CauseImagePull, "(ImagePullBackOff)", "ImagePullBackOff", "imagePullSecrets"},
		{"pod deadline", "", "Pod was active on the node longer than the specified deadline", CauseDeadline, "deadline exceeded", "activeDeadlineSeconds", "raise activeDeadlineSeconds"},
		{"deadline outranks code", "143", "Step exceeded its deadline", CauseDeadline, "deadline exceeded", "activeDeadlineSeconds", "raise activeDeadlineSeconds"},
		{"unschedulable", "", "Unschedulable: 0/3 nodes are available: 3 Insufficient cpu.", CausePodPending, "its pod never started (Unschedulable)", "no node could take the pod", "requests"},
		{"other message", "", "failed to save outputs: key not found", CauseMessage, "failed to save outputs", "message: failed to save outputs", "node's message"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Failed")
			b.task("job", "Error", ran(0, 30), exit(c.exit), msg(c.message))
			f := findRule(t, explainOne(b), RuleRootFailure)
			if f.Cause != c.cause || !strings.Contains(f.Headline, c.headline) || !strings.Contains(evidence(f), c.evidence) || !strings.Contains(f.Next, c.next) {
				t.Fatalf("failure = %+v, want cause %q, headline containing %q, evidence containing %q and next containing %q", f, c.cause, c.headline, c.evidence, c.next)
			}
		})
	}
}

// Root failures exclude parents and fallout, order by finish time and cap individual cards at three.
func TestExplainRootFailures(t *testing.T) {
	for _, c := range []struct {
		name            string
		build           func() *wfBuilder
		roots           []string
		more, dependent string
	}{
		{
			name: "independent failures before fallout",
			build: func() *wfBuilder {
				b := newWF("Failed")
				b.task("late", "Failed", ran(0, 40), exit("2"))
				b.task("early", "Failed", ran(0, 10), exit("1"))
				b.task("handler", "Failed", ran(15, 5), exit("3"), after("early"))
				return b
			},
			roots: []string{"early", "late"}, dependent: "failed after: handler, waits for early",
		},
		{
			name: "fanout card limit",
			build: func() *wfBuilder {
				b := newWF("Failed")
				for i, n := range []string{"s1", "s2", "s3", "s4", "s5"} {
					b.task(n, "Failed", ran(0, float64(10+i)), exit("1"))
				}
				return b
			},
			roots: []string{"s1", "s2", "s3"}, more: "2 more nodes failed on their own",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := explainOne(c.build())
			var roots []string
			for _, f := range r.Findings {
				if f.Rule == RuleRootFailure {
					roots = append(roots, f.NodeID)
				}
			}
			if !reflect.DeepEqual(roots, c.roots) {
				t.Fatalf("root node IDs = %v, want %v", roots, c.roots)
			}
			if !strings.Contains(evidence(r.Findings[0]), "the first failure") {
				t.Fatalf("first root lacks first-failure evidence:\n%s", evidence(r.Findings[0]))
			}
			if c.more != "" {
				f := findRule(t, r, RuleMoreFailures)
				if f.Headline != c.more || !strings.Contains(evidence(f), "s4, s5") {
					t.Fatalf("more failures = %+v, want %q and s4, s5", f, c.more)
				}
			}
			if c.dependent != "" {
				if f := findRule(t, r, RuleDependents); !strings.Contains(evidence(f), c.dependent) {
					t.Fatalf("dependents lack %q:\n%s", c.dependent, evidence(f))
				}
			}
		})
	}
}

// A retry reports its attempts together and reads the last failure's pod, while recovered retries are warnings.
func TestExplainRetries(t *testing.T) {
	for _, c := range []struct {
		name, workflowPhase, retryPhase                 string
		attempts                                        []attemptSpec
		keys, headline, evidence, next, logNode, logPod string
	}{
		{
			name: "same cause", workflowPhase: "Failed", retryPhase: "Failed",
			attempts: []attemptSpec{attempt("Failed", ran(0, 20), exit("1"), msg("Error (exit code 1)"), pod()), attempt("Failed", ran(30, 20), exit("1"), msg("Error (exit code 1)"), pod())},
			keys:     "error root-failure exit-code", headline: "fetch failed all 2 attempts: exit code 1 each time",
			evidence: "attempts: fetch(0) failed exit 1 after 20s · fetch(1) failed exit 1 after 20s", next: "Retrying did not help", logNode: "fetch(1)", logPod: "wf-fetch(1)",
		},
		{
			name: "different causes", workflowPhase: "Failed", retryPhase: "Failed",
			attempts: []attemptSpec{attempt("Failed", ran(0, 20), exit("1")), attempt("Failed", ran(30, 20), exit("137"))},
			keys:     "error root-failure exit-code", headline: "fetch failed 2 attempts in different ways: exit codes 1, 137", evidence: "fetch(1) failed exit 137 after 20s", next: "transient", logNode: "fetch(1)",
		},
		{
			name: "retrying", workflowPhase: "Running", retryPhase: "Running",
			attempts: []attemptSpec{attempt("Failed", ran(0, 20), exit("1")), attempt("Running", ran(30, -1))},
			keys:     "warning root-failure exit-code | info running", headline: "fetch failed 1 attempt so far and is retrying", evidence: "fetch(1) running after 59m30s", next: "Fix the error", logNode: "fetch(0)",
		},
		{
			name: "recovered", workflowPhase: "Succeeded", retryPhase: "Succeeded",
			attempts: []attemptSpec{attempt("Failed", ran(0, 20), exit("1"), msg("connection reset")), attempt("Succeeded", ran(30, 20), exit("0"))},
			keys:     "warning retried | info succeeded", headline: "fetch succeeded on attempt 2 of 2 after failing 1 time", evidence: "last failure: connection reset", next: "flaky",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF(c.workflowPhase)
			b.retry("fetch", c.retryPhase, ran(0, 90), c.attempts...)
			r := explainOne(b)
			if got := strings.Join(keys(r), " | "); got != c.keys {
				t.Fatalf("findings = %q, want %q", got, c.keys)
			}
			f := r.Findings[0]
			if !strings.Contains(f.Headline, c.headline) || !strings.Contains(evidence(f), c.evidence) || !strings.Contains(f.Next, c.next) {
				t.Fatalf("retry = %+v, want headline %q, evidence %q and next %q", f, c.headline, c.evidence, c.next)
			}
			if r.LogNode != c.logNode || r.LogPod != c.logPod {
				t.Fatalf("log target = %q/%q, want %q/%q", r.LogNode, r.LogPod, c.logNode, c.logPod)
			}
			if c.retryPhase == "Succeeded" {
				if got := findRule(t, r, RuleSucceeded).Headline; !strings.Contains(got, "with 1 retried step") {
					t.Fatalf("success headline = %q, want 1 retried step", got)
				}
			}
		})
	}
}

// Failed handlers and hooks are separate errors, while successful hooks add no finding.
func TestExplainHandlers(t *testing.T) {
	for _, c := range []struct {
		name                           string
		build                          func(*wfBuilder)
		keys, rule, headline, evidence string
	}{
		{
			name:  "failed exit handler",
			build: func(b *wfBuilder) { b.exitHandler("Failed", ran(12, 3), exit("127"), msg("Error (exit code 127)")) },
			keys:  "error exit-handler exit-code | info succeeded", rule: RuleExitHandler,
			headline: "The exit handler failed: exit code 127", evidence: "was not found",
		},
		{
			name: "failed and successful hooks",
			build: func(b *wfBuilder) {
				b.hook("slack", "Failed", ran(11, 2), exit("1"))
				b.hook("audit", "Succeeded", ran(11, 2), exit("0"))
			},
			keys: "error hook exit-code | info succeeded", rule: RuleHook,
			headline: "The slack hook failed: exit code 1", evidence: "exit code: 1: the program reported a general error",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Succeeded")
			b.task("work", "Succeeded", ran(0, 10), exit("0"))
			c.build(b)
			r := explainOne(b)
			if got := strings.Join(keys(r), " | "); got != c.keys {
				t.Fatalf("findings = %q, want %q", got, c.keys)
			}
			f := findRule(t, r, c.rule)
			if f.Headline != c.headline {
				t.Errorf("headline = %q, want %q", f.Headline, c.headline)
			}
			if got := evidence(f); !strings.Contains(got, c.evidence) {
				t.Errorf("evidence = %q, want it to contain %q", got, c.evidence)
			}
		})
	}
}

// Failure fallout lists omitted and unstarted dependents without counting nodes skipped by their own conditions.
func TestExplainDependents(t *testing.T) {
	b := newWF("Failed")
	b.task("build", "Failed", ran(0, 10), exit("1"))
	b.task("test", "Omitted", notRun(), msg("omitted: depends condition not met"), after("build"))
	b.task("deploy", "Skipped", notRun(), msg("when 'false' evaluated false"))
	b.task("notify", "Pending", notRun(), after("test"))
	r := explainOne(b)
	f := findRule(t, r, RuleDependents)
	ev := evidence(f)
	for _, want := range []string{"omitted: test, waits for build (depends condition not met)", "not started: notify, waits for test"} {
		if !strings.Contains(ev, want) {
			t.Errorf("dependents lack %q:\n%s", want, ev)
		}
	}
	if strings.Contains(ev, "deploy") || f.Headline != "2 nodes did not run because of the failure: test, notify" {
		t.Errorf("dependents headline = %q and evidence = %q, want headline %q without deploy evidence", f.Headline, ev, "2 nodes did not run because of the failure: test, notify")
	}
}

// Deadline findings keep the workflow limit and are not duplicated when a failing node names the cause.
func TestExplainDeadline(t *testing.T) {
	for _, c := range []struct {
		name, exit, message, keys, rule, evidence string
	}{
		{"workflow deadline", "143", "terminated", "error root-failure exit-code | error deadline deadline", RuleDeadline, "deadline: activeDeadlineSeconds 300"},
		{"node deadline", "", "Step exceeded its deadline", "error root-failure deadline", RuleRootFailure, "activeDeadlineSeconds 300 on the workflow"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Failed")
			b.wf.Summary.Message = "Step exceeded its deadline"
			b.wf.Resource = []byte(`{"spec":{"activeDeadlineSeconds":300}}`)
			b.task("slow", "Failed", ran(0, 300), exit(c.exit), msg(c.message))
			r := explainOne(b)
			if got := strings.Join(keys(r), " | "); got != c.keys {
				t.Fatalf("findings = %q, want %q", got, c.keys)
			}
			f := findRule(t, r, c.rule)
			if got := evidence(f); !strings.Contains(got, c.evidence) {
				t.Errorf("evidence = %q, want it to contain %q", got, c.evidence)
			}
		})
	}
}

// A workflow failed from above keeps the controller's message even when every node succeeded.
func TestExplainWorkflowFailed(t *testing.T) {
	b := newWF("Failed")
	b.wf.Summary.Message = "workflow shutdown with strategy: Terminate"
	b.task("a", "Succeeded", ran(0, 10), exit("0"))
	f := findRule(t, explainOne(b), RuleWorkflowFailed)
	if f.Headline != "The workflow failed: workflow shutdown with strategy: Terminate" {
		t.Fatalf("headline = %q, want workflow shutdown with strategy: Terminate", f.Headline)
	}
}

// Pods an unfinished run cannot start are warnings with Kubernetes reasons and concrete next steps.
func TestExplainPendingPods(t *testing.T) {
	b := newWF("Running")
	b.task("fetch", "Pending", ran(0, -1), msg("ImagePullBackOff: Back-off pulling image \"registry.example/fetch:9\""))
	b.task("big", "Pending", ran(0, -1), msg("Unschedulable: 0/3 nodes are available: 3 Insufficient memory."))
	r := explainOne(b)
	pend := map[string]Finding{}
	for _, f := range r.Findings {
		if f.Rule == RulePodPending {
			pend[f.NodeID] = f
		}
	}
	if len(pend) != 2 {
		t.Fatalf("pending pod findings = %+v, want exactly fetch and big; all findings = %v", pend, keys(r))
	}
	if f := pend["fetch"]; f.Cause != CauseImagePull || f.Severity != Warning ||
		!strings.Contains(f.Headline, "fetch cannot start: ImagePullBackOff") || !strings.Contains(f.Next, "imagePullSecrets") {
		t.Errorf("image pull finding = %+v, want cause %q, severity warning, headline containing %q and advice containing %q", f, CauseImagePull, "fetch cannot start: ImagePullBackOff", "imagePullSecrets")
	}
	if f := pend["big"]; f.Cause != CausePodPending ||
		!strings.Contains(f.Headline, "big cannot start: Unschedulable") || !strings.Contains(f.Next, "requests") {
		t.Errorf("scheduling finding = %+v, want cause %q, headline containing %q and advice containing %q", f, CausePodPending, "big cannot start: Unschedulable", "requests")
	}
}

// Approval gates become warnings at an hour and keep the time they began waiting.
func TestExplainGateWait(t *testing.T) {
	for _, c := range []struct {
		name     string
		wait     time.Duration
		severity Severity
		headline string
	}{
		{"short", 4*time.Minute + 50*time.Second, Info, "Waiting for a person at approve for 4m50s"},
		{"threshold", time.Hour, Warning, "Waiting for a person at approve for 1h00m, a long wait"},
		{"long", 3 * time.Hour, Warning, "Waiting for a person at approve for 3h00m, a long wait"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Running")
			b.task("approve", "Running", ran(430, -1), typ("Suspend"))
			b.now = b.start.Add(430*time.Second + c.wait)
			f := findRule(t, explainOne(b), RuleSuspended)
			if f.Severity != c.severity || f.Headline != c.headline || !strings.Contains(evidence(f), "since: 7m10s into the run") || !strings.Contains(f.Next, "Resume it (a, u)") {
				t.Fatalf("gate = %+v, want %s %q with start offset and resume advice", f, c.severity, c.headline)
			}
		})
	}
}

// Workflow-wide suspension is reported separately from approval gates and ignored once the run finishes.
func TestExplainWorkflowSuspended(t *testing.T) {
	for _, c := range []struct {
		name, phase string
		gate, want  bool
	}{
		{"suspended", "Running", false, true},
		{"gate owns suspension", "Running", true, false},
		{"finished", "Succeeded", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF(c.phase)
			b.wf.Resource = []byte(`{"spec":{"suspend":true}}`)
			if c.gate {
				b.task("approve", "Running", ran(0, -1), typ("Suspend"))
			}
			found := false
			for _, f := range explainOne(b).Findings {
				found = found || f.Rule == RuleWorkflowSuspended
			}
			if found != c.want {
				t.Fatalf("workflow suspended finding = %v, want %v", found, c.want)
			}
		})
	}
}

// Running reports show active and waiting work, with warnings only past the workflow estimate.
func TestExplainRunning(t *testing.T) {
	for _, c := range []struct {
		name     string
		elapsed  time.Duration
		rule     string
		severity Severity
		headline string
	}{
		{"within estimate", 4 * time.Minute, RuleRunning, Info, "Running for 4m00s of an estimated 5m00s"},
		{"at estimate", 5 * time.Minute, RuleRunning, Info, "Running for 5m00s of an estimated 5m00s"},
		{"overdue", 12 * time.Minute, RuleOverdue, Warning, "Running for 12m00s, past its estimate of 5m00s"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Running")
			b.wf.Summary.EstimatedDuration = 5 * time.Minute
			b.wf.Summary.Progress = "1/3"
			b.task("crunch", "Running", ran(80, -1), func(n *core.Node, _ *wfBuilder) { n.EstimatedDuration = 2 * time.Minute })
			b.task("waiting", "Pending", notRun(), after("crunch"))
			b.now = b.start.Add(c.elapsed)
			f := findRule(t, explainOne(b), c.rule)
			if f.Severity != c.severity || f.Headline != c.headline {
				t.Fatalf("running = %s %q, want %s %q", f.Severity, f.Headline, c.severity, c.headline)
			}
			for _, want := range []string{"running: crunch", "waiting: waiting", "past estimate: crunch", "progress: 1/3 pods done"} {
				if !strings.Contains(evidence(f), want) {
					t.Errorf("evidence lacks %q:\n%s", want, evidence(f))
				}
			}
		})
	}
}

// A controller wait becomes a warning at five minutes while keeping its postponement message.
func TestExplainNotStarted(t *testing.T) {
	for _, c := range []struct {
		wait     time.Duration
		severity Severity
	}{
		{time.Minute, Info},
		{5 * time.Minute, Warning},
		{20 * time.Minute, Warning},
	} {
		t.Run(c.wait.String(), func(t *testing.T) {
			b := newWF("Pending")
			b.wf.Nodes = nil
			b.wf.Summary.StartedAt = nil
			b.wf.Summary.Message = "Workflow processing has been postponed due to max parallelism limit"
			b.now = b.start.Add(c.wait)
			f := findRule(t, explainOne(b), RuleNotStarted)
			if f.Severity != c.severity || !strings.Contains(evidence(f), "max parallelism limit") || !strings.Contains(f.Headline, "submitted") {
				t.Fatalf("not started = %+v, want %s with submission time and postponement message", f, c.severity)
			}
		})
	}
}

// Failed nodes in a workflow that succeeded anyway are pointed out.
func TestExplainToleratedFailures(t *testing.T) {
	b := newWF("Succeeded")
	b.task("lint", "Failed", ran(0, 5), exit("1"))
	b.task("build", "Succeeded", ran(0, 20), exit("0"))
	r := explainOne(b)
	if got := strings.Join(keys(r), " | "); got != "warning tolerated | info succeeded" {
		t.Fatalf("findings = %q, want %q", got, "warning tolerated | info succeeded")
	}
}

// The node map the server withheld is a finding, not an empty section.
func TestExplainNodesUnavailable(t *testing.T) {
	b := newWF("Running")
	b.wf.NodesAvailable = false
	b.wf.NodesUnavailableReason = "node status offloaded and not hydrated by the server"
	r := explainOne(b)
	f := findRule(t, r, RuleNodesUnavailable)
	if !strings.Contains(evidence(f), "offloaded and not hydrated") {
		t.Errorf("unavailable evidence = %q, want it to contain %q", evidence(f), "offloaded and not hydrated")
	}
}

// Log evidence distinguishes loading, read, missing, failed and empty logs without guessing a nameless pod.
func TestExplainLogStates(t *testing.T) {
	for _, c := range []struct {
		name              string
		pod               bool
		log               *Log
		text, logHeadline string
		severity          Severity
	}{
		{"static", true, nil, "", "", Info},
		{"loading", true, &Log{State: LogLoading, Tail: 200}, "reading the last 200 lines of job's log…", "", Info},
		{"read", true, &Log{NodeID: "job", PodName: "wf-job", State: LogRead, Tail: 200, Lines: []string{"start", "fatal: password authentication failed", "bye"}}, "picked from the last 3 lines of job's log (container main)", "", Info},
		{"gone", true, &Log{NodeID: "job", PodName: "wf-job", State: LogFailed, Err: `pods "wf-job" not found`, Gone: true}, "could not be read (see below)", "The log of job is gone", Warning},
		{"failed", true, &Log{NodeID: "job", PodName: "wf-job", State: LogFailed, Err: "connection reset"}, "could not be read (see below)", "The log of job could not be read", Warning},
		{"empty", true, &Log{NodeID: "job", PodName: "wf-job", State: LogRead}, "empty (see below)", "The log of job is empty", Info},
		{"other pod", true, &Log{NodeID: "other", PodName: "other-pod", State: LogRead, Lines: []string{"ERROR unrelated"}, Tail: 200}, "reading the last 200 lines of job's log…", "", Info},
		{"nameless", false, &Log{State: LogLoading, Tail: 200}, "not read: the server did not state its pod naming, so the pod cannot be named safely", "", Info},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Failed")
			opts := []opt{ran(0, 30), exit("1")}
			if c.pod {
				opts = append(opts, pod())
			}
			b.task("job", "Failed", opts...)
			in := b.input()
			in.Log = c.log
			r := Explain(in)
			wantPod := ""
			if c.pod {
				wantPod = "wf-job"
			}
			if r.LogNode != "job" || r.LogPod != wantPod {
				t.Fatalf("log target = %q/%q, want job/%q", r.LogNode, r.LogPod, wantPod)
			}
			f := findRule(t, r, RuleRootFailure)
			text := ""
			for _, ev := range f.Evidence {
				if ev.Label == "log" {
					text = ev.Text
				}
			}
			if text != c.text {
				t.Fatalf("log evidence = %q, want %q", text, c.text)
			}
			var logFinding *Finding
			for i := range r.Findings {
				if r.Findings[i].Rule == RuleLog {
					logFinding = &r.Findings[i]
				}
			}
			if c.logHeadline == "" {
				if logFinding != nil {
					t.Fatalf("unexpected log finding: %+v", logFinding)
				}
			} else if logFinding == nil || logFinding.Headline != c.logHeadline || logFinding.Severity != c.severity {
				t.Fatalf("log finding = %+v, want %s %q", logFinding, c.severity, c.logHeadline)
			}
			if c.log != nil && c.log.Gone && !strings.Contains(logFinding.Next, "archiveLogs") {
				t.Fatalf("gone log advice = %q, want archiveLogs", logFinding.Next)
			}
		})
	}
}

// Parameter values travel separately from text so the view controls their visibility.
func TestExplainParameters(t *testing.T) {
	b := newWF("Failed")
	b.task("job", "Failed", ran(0, 30), exit("1"), inputs("token", "s3cr3t"))
	f := findRule(t, explainOne(b), RuleRootFailure)
	found := false
	for _, ev := range f.Evidence {
		if strings.Contains(ev.Text, "s3cr3t") {
			t.Fatalf("a parameter value is in the text: %q", ev.Text)
		}
		if ev.Label == "inputs" {
			if found || !reflect.DeepEqual(ev.Params, []core.Parameter{{Name: "token", Value: "s3cr3t"}}) {
				t.Fatalf("inputs = %+v, want one token parameter", ev.Params)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("root failure omitted inputs evidence")
	}
}
