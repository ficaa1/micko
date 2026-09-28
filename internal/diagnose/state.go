package diagnose

import (
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// state.go holds the rules about a run that has not failed: waiting at a
// gate, running (within its estimate or past it), not started, or done.

// longWait is how long a wait at a gate, or for the controller to start a
// workflow, may last before the finding becomes a warning.
const (
	longGateWait  = time.Hour
	longStartWait = 5 * time.Minute
)

// suspended reports every approval gate the run is waiting at, and for how
// long.
func (x *index) suspended() []Finding {
	if finishedPhase(x.in.Workflow.Summary.Phase) {
		return nil
	}
	var out []Finding
	for _, id := range x.order {
		n := x.nodes[id]
		if n.Type != "Suspend" || n.Phase != "Running" {
			continue
		}
		f := Finding{Rule: RuleSuspended, Severity: Info, NodeID: id,
			Headline: "Waiting for a person at " + x.name(id),
			Next:     "Resume it (a, u) when the run may go on, or stop it (a, s). Nothing after the gate runs until then.",
		}
		ev := []Evidence{{Label: "node", Text: x.nodeLine(id)}}
		if start, _, ok := x.ran(id); ok {
			wait := x.in.Now.Sub(start)
			f.Headline += " for " + shared.ShortDuration(wait)
			ev = append(ev, Evidence{Label: "since", Text: x.offset(start) + " into the run"})
			if wait >= longGateWait {
				f.Severity = Warning
				f.Headline += ", a long wait"
			}
		}
		if p := x.path(id); len(p) > 1 {
			ev = append(ev, Evidence{Label: "path", Text: strings.Join(p, " › ")})
		}
		if len(n.Inputs.Parameters) > 0 {
			ev = append(ev, Evidence{Label: "inputs", Params: n.Inputs.Parameters})
		}
		f.Evidence = ev
		out = append(out, f)
	}
	return out
}

// workflowSuspended reports a workflow suspended as a whole (argo suspend),
// which holds no gate node of its own.
func (x *index) workflowSuspended() (Finding, bool) {
	s := x.in.Workflow.Summary
	if finishedPhase(s.Phase) || x.spec.Suspend == nil || !*x.spec.Suspend {
		return Finding{}, false
	}
	for _, n := range x.nodes {
		if n.Type == "Suspend" && n.Phase == "Running" {
			return Finding{}, false
		}
	}
	return Finding{Rule: RuleWorkflowSuspended, Severity: Info,
		Headline: "The workflow is suspended: no new step starts until it is resumed",
		Evidence: []Evidence{{Label: "spec", Text: "suspend: true"}},
		Next:     "Resume it (a, u) to let it continue; steps already running finish either way.",
	}, true
}

// running reports a run in progress: how long it has run against the
// controller's estimate, what is running now, what waits, and which nodes
// have run past their own estimates. Past the workflow's estimate it is a
// warning.
func (x *index) running() (Finding, bool) {
	s := x.in.Workflow.Summary
	if finishedPhase(s.Phase) || s.StartedAt == nil {
		return Finding{}, false
	}
	elapsed := x.in.Now.Sub(*s.StartedAt)
	estimate := x.estimate()
	f := Finding{Rule: RuleRunning, Severity: Info,
		Headline: "Running for " + shared.ShortDuration(elapsed),
		Next:     "Nothing to do yet. The Timeline (T) shows where the time goes.",
	}
	if estimate > 0 {
		f.Headline += " of an estimated " + shared.ShortDuration(estimate)
		if elapsed > estimate {
			f.Rule, f.Severity = RuleOverdue, Warning
			f.Headline = "Running for " + shared.ShortDuration(elapsed) + ", past its estimate of " + shared.ShortDuration(estimate)
			f.Next = "Check the running nodes' logs for progress, and the Timeline (T) for what is taking longer than in earlier runs."
		}
	}
	var running, waiting, over []string
	for _, id := range x.order {
		n := x.nodes[id]
		// Gates have findings of their own; the lists are the work.
		if n.Type != "Pod" && n.Type != "ContainerSet" {
			continue
		}
		start, end, ok := x.ran(id)
		switch {
		case ok && !finishedPhase(n.Phase):
			running = append(running, x.name(id)+" "+shared.ShortDuration(end.Sub(start)))
			if n.EstimatedDuration > 0 && end.Sub(start) > n.EstimatedDuration {
				over = append(over, x.name(id)+" "+shared.ShortDuration(end.Sub(start))+" of ~"+shared.ShortDuration(n.EstimatedDuration))
			}
		case !ok && !finishedPhase(n.Phase):
			waiting = append(waiting, x.name(id))
		}
	}
	if len(running) > 0 {
		f.Evidence = append(f.Evidence, Evidence{Label: "running", Text: listNames(running, 5)})
	}
	if len(waiting) > 0 {
		f.Evidence = append(f.Evidence, Evidence{Label: "waiting", Text: listNames(waiting, 5)})
	}
	if len(over) > 0 {
		f.Evidence = append(f.Evidence, Evidence{Label: "past estimate", Text: listNames(over, 5)})
	}
	if s.Progress != "" {
		f.Evidence = append(f.Evidence, Evidence{Label: "progress", Text: s.Progress + " pods done"})
	}
	return f, true
}

// estimate is the controller's estimate for the whole run: the workflow's
// own, or its root node's, which the controller fills from the same history.
func (x *index) estimate() time.Duration {
	wf := x.in.Workflow
	if wf.Summary.EstimatedDuration > 0 {
		return wf.Summary.EstimatedDuration
	}
	for _, n := range wf.Nodes {
		if n.Name == wf.Summary.Ref.Name && n.EstimatedDuration > 0 {
			return n.EstimatedDuration
		}
	}
	return 0
}

// notStarted reports a workflow the controller has not started: no nodes
// and no final phase. After a few minutes that is a warning, since a healthy
// controller picks a workflow up within seconds.
func notStarted(x *index) (Finding, bool) {
	s := x.in.Workflow.Summary
	if finishedPhase(s.Phase) || s.Phase == "Running" {
		return Finding{}, false
	}
	f := Finding{Rule: RuleNotStarted, Severity: Info,
		Headline: "Not started: the controller has not run any node yet",
		Next:     "Check that the workflow controller is running and manages this namespace (its namespace and instanceID settings), and whether a parallelism limit or a semaphore is holding the workflow.",
	}
	if !s.CreatedAt.IsZero() {
		wait := x.in.Now.Sub(s.CreatedAt)
		f.Headline = "Not started: submitted " + shared.ShortDuration(wait) + " ago and the controller has not run any node"
		if wait >= longStartWait {
			f.Severity = Warning
		}
		f.Evidence = append(f.Evidence, Evidence{Label: "submitted", Text: s.CreatedAt.UTC().Format(time.RFC3339)})
	}
	f.Evidence = append(f.Evidence, Evidence{Label: "phase", Text: orWord(s.Phase, "none yet")})
	if s.Message != "" {
		f.Evidence = append(f.Evidence, Evidence{Label: "message", Text: firstLine(s.Message)})
	}
	return f, true
}

// succeeded reports a finished, successful run plainly: how long it took and
// what ran. Anything that went wrong on the way has its own finding.
func (x *index) succeeded() (Finding, bool) {
	s := x.in.Workflow.Summary
	if s.Phase != "Succeeded" {
		return Finding{}, false
	}
	f := Finding{Rule: RuleSucceeded, Severity: Info,
		Headline: "Succeeded",
		Next:     "Nothing to do.",
	}
	if s.StartedAt != nil && s.FinishedAt != nil {
		f.Headline += " in " + shared.ShortDuration(s.FinishedAt.Sub(*s.StartedAt))
	}
	pods, skipped, retried := 0, 0, 0
	for _, id := range x.order {
		n := x.nodes[id]
		switch {
		case n.Type == "Pod" && n.Phase == "Succeeded":
			pods++
		case n.Phase == "Skipped":
			skipped++
		}
		if n.Type == "Retry" && x.failedBelow[id] {
			retried++
		}
	}
	ran := plural(pods, "pod") + " succeeded"
	if skipped > 0 {
		ran += ", " + plural(skipped, "node") + " skipped by their conditions"
	}
	f.Evidence = append(f.Evidence, Evidence{Label: "ran", Text: ran})
	if retried > 0 {
		f.Headline += ", with " + plural(retried, "retried step")
		f.Next = "Nothing failed in the end; the warnings above say what needed another attempt."
	}
	return f, true
}
