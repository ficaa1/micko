package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
)

// betaReader applies actions like a server; execErr fails every send.
type betaReader struct {
	*testkit.FakeReader
	execCalls int
	execErr   error
}

func (b *betaReader) Execute(_ context.Context, req core.ActionRequest) (core.ActionResult, error) {
	b.execCalls++
	if b.execErr != nil {
		return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}, b.execErr
	}
	// The resume takes effect at once, so the read-back sees it.
	if wf, ok := b.Workflows[req.Ref]; ok && req.Action == core.ActionResume {
		wf.Summary.Suspended = false
		b.Workflows[req.Ref] = wf
	}
	ref := req.Ref
	return core.ActionResult{Action: req.Action, Target: req.Ref, Affected: &ref, Outcome: core.ActionConfirmed}, nil
}

func (b *betaReader) execs() int { return b.execCalls }

// stopReader accepts every action, then reports phases in turn, the last repeating.
type stopReader struct {
	*testkit.FakeReader
	phases    []string
	execCalls int
}

func (s *stopReader) Get(_ context.Context, ref core.Ref) (core.Workflow, error) {
	wf := s.Workflows[ref]
	if len(s.phases) > 0 {
		wf.Summary.Phase = s.phases[0]
		if len(s.phases) > 1 {
			s.phases = s.phases[1:]
		}
	}
	return wf, nil
}

func (s *stopReader) Execute(_ context.Context, req core.ActionRequest) (core.ActionResult, error) {
	s.execCalls++
	ref := req.Ref
	return core.ActionResult{Action: req.Action, Target: req.Ref, Affected: &ref, Outcome: core.ActionConfirmed}, nil
}

func (s *stopReader) execs() int { return s.execCalls }

// refusingReader fails the preflight read, so no action is ever sent.
type refusingReader struct{ *testkit.FakeReader }

func (refusingReader) Get(context.Context, core.Ref) (core.Workflow, error) {
	return core.Workflow{}, errors.New("preflight read failed")
}

func (refusingReader) Execute(context.Context, core.ActionRequest) (core.ActionResult, error) {
	panic("the action was sent even though the preflight failed")
}

func (refusingReader) execs() int { return 0 }

// shortObservation cuts the wait for an accepted action's end state to milliseconds.
func shortObservation(t *testing.T) {
	t.Helper()
	old, oldN := stopObservationInterval, stopObservationAttempts
	stopObservationInterval, stopObservationAttempts = time.Millisecond, 3
	t.Cleanup(func() { stopObservationInterval, stopObservationAttempts = old, oldN })
}

// An action is sent once and its outcome is what was read back afterwards.
func TestActionOutcome(t *testing.T) {
	wf := workflowFixture("wf")
	failed := wf
	failed.Summary.Phase = "Failed"
	type reader interface {
		core.Reader
		execs() int
	}
	for _, c := range []struct {
		name   string
		reader func() reader
		action core.Action
		want   core.ActionOutcome
		execs  int
	}{
		{"a resubmit read back", func() reader { return &betaReader{FakeReader: fixtureReader(failed)} }, core.ActionResubmit, core.ActionConfirmed, 1},
		{"a send that failed after leaving", func() reader {
			return &betaReader{FakeReader: fixtureReader(wf), execErr: errors.New("connection reset after send")}
		}, core.ActionTerminate, core.ActionUnknown, 1},
		{"a failed preflight", func() reader { return refusingReader{fixtureReader(wf)} }, core.ActionRetry, core.ActionRefused, 0},
		{"a stop still running its exit handler", func() reader { return &stopReader{FakeReader: fixtureReader(wf), phases: []string{"Running"}} }, core.ActionStop, core.ActionAccepted, 1},
		{"a stop that reaches a terminal phase", func() reader {
			return &stopReader{FakeReader: fixtureReader(wf), phases: []string{"Running", "Succeeded"}}
		}, core.ActionStop, core.ActionConfirmed, 1},
		{"a terminate that reaches a terminal phase", func() reader {
			return &stopReader{FakeReader: fixtureReader(wf), phases: []string{"Running", "Running", "Failed"}}
		}, core.ActionTerminate, core.ActionConfirmed, 1},
		{"a terminate still running", func() reader { return &stopReader{FakeReader: fixtureReader(wf), phases: []string{"Running"}} }, core.ActionTerminate, core.ActionAccepted, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			shortObservation(t)
			r := c.reader()
			m := armedRoot(r, actions.Options{})
			req := core.ActionRequest{Ref: wf.Summary.Ref, Action: c.action, Confirmation: core.Confirmation{Confirmed: true, TypedName: "wf"}}
			msgs := runCmd(m.startAction(req))
			if len(msgs) != 1 {
				t.Fatalf("messages = %d", len(msgs))
			}
			got := msgs[0].(actionResultMsg).Result
			if got.Outcome != c.want || r.execs() != c.execs {
				t.Fatalf("outcome %s after %d sends, want %s after %d", got.Outcome, r.execs(), c.want, c.execs)
			}
			if got.Outcome == core.ActionAccepted && (got.Workflow == nil || got.Workflow.Summary.Phase != "Running") {
				t.Fatalf("an accepted result does not carry the live phase: %+v", got.Workflow)
			}
		})
	}
}

// The read-back settles an action only on the end state it asked for.
func TestReadBackSettlesOnTheEndState(t *testing.T) {
	shortObservation(t)
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	for _, c := range []struct {
		name    string
		action  core.Action
		wf      *core.Summary
		settled bool
	}{
		{"resume, gate still open", core.ActionResume, &core.Summary{Phase: "Running", Suspended: true}, false},
		{"resume, gate closed", core.ActionResume, &core.Summary{Phase: "Running"}, true},
		{"retry, still failed", core.ActionRetry, &core.Summary{Phase: "Failed"}, false},
		{"retry, running", core.ActionRetry, &core.Summary{Phase: "Running"}, true},
		{"suspend, suspended", core.ActionSuspend, &core.Summary{Phase: "Running", Suspended: true}, true},
		{"suspend, not suspended", core.ActionSuspend, &core.Summary{Phase: "Running"}, false},
		{"delete, gone", core.ActionDelete, nil, true},
		// An archived copy or a finalizer keeps a deleted workflow readable.
		{"delete, still readable", core.ActionDelete, &core.Summary{Phase: "Succeeded"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := fixtureReader()
			if c.wf != nil {
				s := *c.wf
				s.Ref = ref
				f.Workflows[ref] = core.Workflow{Summary: s}
			}
			_, settled, err := readBack(context.Background(), f, ref, c.action)
			if err != nil || settled != c.settled {
				t.Fatalf("settled = %v (err %v), want %v", settled, err, c.settled)
			}
		})
	}
}

// Only an UNKNOWN outcome keeps the action pane open.
func TestOnlyAnUnknownOutcomeKeepsThePaneOpen(t *testing.T) {
	for _, c := range []struct {
		outcome core.ActionOutcome
		err     error
		open    bool
		flash   string
	}{
		{core.ActionUnknown, nil, true, ""},
		{core.ActionConfirmed, errors.New("read back failed"), true, ""},
		{core.ActionAccepted, context.DeadlineExceeded, false, "stop accepted"},
		{core.ActionRefused, errors.New("uid mismatch"), false, "stop refused"},
		{core.ActionConfirmed, nil, false, "stop confirmed"},
	} {
		t.Run(string(c.outcome), func(t *testing.T) {
			wf := workflowFixture("wf")
			m := armedRoot(&betaReader{FakeReader: fixtureReader(wf)}, actions.Options{})
			m.route = RouteDetail
			m.selection = wf.Summary.Ref
			m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
			m.actionView.Open(core.ActionStop)
			m.actionView.Confirm()
			m.handleActionResult(actionResultMsg{
				genStamp: genStamp{Attempt: m.actionAttempt},
				Result:   core.ActionResult{Action: core.ActionStop, Target: wf.Summary.Ref, Outcome: c.outcome},
				Err:      c.err,
			})
			if open := m.actionView.State() == actions.StateOutcome; open != c.open {
				t.Fatalf("pane open = %v, want %v", open, c.open)
			}
			if !strings.Contains(m.flash, c.flash) {
				t.Fatalf("flash = %q, want %q", m.flash, c.flash)
			}
		})
	}
}

func TestRootKeyboardActionReachesExecutorAndRendersOutcome(t *testing.T) {
	wf := workflowFixture("wf")
	wf.Summary.Suspended = true
	b := &betaReader{FakeReader: fixtureReader(wf)}
	m := armedRoot(b, actions.Options{})
	m.route = RouteDetail
	m.selection = wf.Summary.Ref
	m.detailState = detailState{ref: wf.Summary.Ref, workflow: wf}

	keys(m, "a", "u") // resume
	cmd := keys(m, "y")
	if cmd == nil {
		t.Fatal("the confirmation emitted nothing")
	}
	intent := cmd()
	if _, ok := intent.(actions.ActionIntentMsg); !ok {
		t.Fatalf("intent = %T", intent)
	}
	_, cmd = m.Update(intent)
	if cmd == nil {
		t.Fatal("the root did not start the action")
	}
	m.Update(cmd())
	if b.execCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", b.execCalls)
	}
	if m.actionView.State() != actions.StateIdle || m.route != RouteDetail {
		t.Fatalf("pane %v on route %v, want the detail back", m.actionView.State(), m.route)
	}
	if !strings.Contains(screen(m), "resume confirmed") {
		t.Fatalf("the outcome is not on screen:\n%s", screen(m))
	}
}

// Stale data blocks actions and says why until a fresh snapshot arrives.
func TestStaleDataBlocksActions(t *testing.T) {
	for _, c := range []struct {
		name   string
		stale  func(m *Root)
		reason string
		fresh  func(m *Root)
	}{
		{"connection lost", func(m *Root) { m.SetConnectionState(false, "") }, "connection lost",
			func(m *Root) { m.SetConnectionState(true, "") }},
		{"list failed", func(m *Root) {
			m.handleListLoaded(listLoadedMsg{Err: &core.APIError{Kind: core.ErrUnavailable, Message: "connection refused"}})
		}, "data is stale", func(*Root) {}},
	} {
		t.Run(c.name, func(t *testing.T) {
			wf := workflowFixture("wf")
			m := armedRoot(&betaReader{FakeReader: fixtureReader(wf)}, actions.Options{})
			m.route = RouteDetail
			m.selection = wf.Summary.Ref
			req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionResume, Confirmation: core.Confirmation{Confirmed: true}}

			c.stale(m)
			if cmd := m.startAction(req); cmd != nil {
				t.Fatal("an action started over stale data")
			}
			keys(m, "a")
			if m.actionView.State() != actions.StateUnavailable || !strings.Contains(m.actionView.View().Content, c.reason) {
				t.Fatalf("a: state %v\n%s", m.actionView.State(), m.actionView.View().Content)
			}
			keys(m, "esc")

			c.fresh(m)
			if cmd := m.startAction(req); cmd != nil {
				t.Fatal("an action started before a fresh snapshot")
			}
			m.handleListLoaded(listLoadedMsg{Page: core.Page{Items: []core.Summary{wf.Summary}}})
			if cmd := m.startAction(req); cmd == nil {
				t.Fatal("a fresh snapshot did not open the gate")
			}
		})
	}
}

// The result of an earlier attempt never changes the pane of a later one.
func TestActionLateResultCannotOverwriteLaterAttempt(t *testing.T) {
	wf := workflowFixture("wf")
	m := armedRoot(&betaReader{FakeReader: fixtureReader(wf)}, actions.Options{})
	m.selection = wf.Summary.Ref
	req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}}
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
	first := m.startAction(req)().(actionResultMsg)
	m.clearInflight("action", uint64(first.Attempt))
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
	second := m.startAction(req)().(actionResultMsg)
	if first.Attempt == 0 || first.Attempt == second.Attempt {
		t.Fatalf("attempt identities = %d and %d", first.Attempt, second.Attempt)
	}
	m.actionView.Open(core.ActionRetry)
	want := m.actionView.State()
	m.Update(first)
	if m.actionView.State() != want {
		t.Fatalf("a late result moved the pane to %v", m.actionView.State())
	}
}
