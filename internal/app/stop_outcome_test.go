package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/actions"
)

// stopReader accepts one Stop and then reports the phase sequence given, so a
// test can imitate a workflow that runs an exit handler after the request was
// accepted.
type stopReader struct {
	*testkit.FakeReader
	ref       core.Ref
	phases    []string
	execCalls int
}

func (s *stopReader) Get(_ context.Context, ref core.Ref) (core.Workflow, error) {
	wf := s.FakeReader.Workflows[s.ref]
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

func newStopRoot(t *testing.T, phases ...string) (*Root, *stopReader) {
	t.Helper()
	wf := workflowFixture("wf")
	r := &stopReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}, ref: wf.Summary.Ref, phases: phases}
	m := NewRootWithOptions(r, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	old, oldN := stopObservationInterval, stopObservationAttempts
	stopObservationInterval, stopObservationAttempts = time.Millisecond, 3
	t.Cleanup(func() { stopObservationInterval, stopObservationAttempts = old, oldN })
	return m, r
}

func stopRequest(m *Root) core.ActionRequest {
	wf := workflowFixture("wf")
	_ = m
	return core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionStop, Confirmation: core.Confirmation{Confirmed: true}}
}

// A Stop the server accepted is certain, even while the exit handler still
// runs. Reporting it as UNKNOWN would invite the operator to send it again.
func TestGracefulStopStillRunningReportsAcceptedNotUnknown(t *testing.T) {
	m, r := newStopRoot(t, "Running")
	msgs := runCmd(m.startAction(stopRequest(m)))
	if len(msgs) != 1 {
		t.Fatalf("messages=%d", len(msgs))
	}
	got := msgs[0].(actionResultMsg).Result
	if got.Outcome != core.ActionAccepted {
		t.Fatalf("outcome = %q, want accepted while the exit handler runs", got.Outcome)
	}
	if r.execCalls != 1 {
		t.Fatalf("execute calls = %d, want exactly one", r.execCalls)
	}
	if got.Workflow == nil || got.Workflow.Summary.Phase != "Running" {
		t.Fatalf("accepted result did not carry the live phase: %+v", got.Workflow)
	}
}

// Once the workflow reaches a terminal phase the outcome is confirmed.
func TestGracefulStopReachingTerminalPhaseIsConfirmed(t *testing.T) {
	m, r := newStopRoot(t, "Running", "Succeeded")
	msgs := runCmd(m.startAction(stopRequest(m)))
	got := msgs[0].(actionResultMsg).Result
	if got.Outcome != core.ActionConfirmed {
		t.Fatalf("outcome = %q, want confirmed after a terminal phase", got.Outcome)
	}
	if r.execCalls != 1 {
		t.Fatalf("execute calls = %d, want exactly one", r.execCalls)
	}
}

// An accepted result must not be rewritten to unknown by a later observation
// error, and it must not be resent.
func TestAcceptedOutcomeIsNotDowngradedToUnknown(t *testing.T) {
	m, _ := newStopRoot(t, "Running")
	m.actionView = actions.NewWithOptions(workflowFixture("wf").Summary.Ref, actions.Options{AllowActions: true})
	msg := actionResultMsg{genStamp: genStamp{Attempt: m.actionAttempt}, Result: core.ActionResult{Action: core.ActionStop, Outcome: core.ActionAccepted}, Err: context.DeadlineExceeded}
	m.handleActionResult(msg)
	if msg.Result.Outcome != core.ActionAccepted {
		t.Fatalf("outcome = %q, want accepted to survive an observation error", msg.Result.Outcome)
	}
}

// Stale data must block mutations whatever made it stale. A server-side
// failure leaves the snapshot just as old as a dead port-forward does.
func TestFailedListBlocksActionsUntilFreshData(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.handleListLoaded(listLoadedMsg{genStamp: genStamp{}, Err: &core.APIError{Kind: core.ErrUnavailable, Message: "connection refused"}})
	if m.connectionFresh {
		t.Fatal("a failed list left actions enabled over stale data")
	}
	req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionResume, Confirmation: core.Confirmation{Confirmed: true}}
	if cmd := m.startAction(req); cmd != nil {
		t.Fatal("action started over stale data")
	}
	m.handleListLoaded(listLoadedMsg{genStamp: genStamp{}, Page: core.Page{Items: []core.Summary{wf.Summary}}})
	if !m.connectionFresh {
		t.Fatal("a fresh list did not re-enable actions")
	}
	if cmd := m.startAction(req); cmd == nil {
		t.Fatal("action still blocked after fresh data")
	}
}

// A blocked action key must say why. A key that does nothing at all reads as
// a broken key rather than a safety gate.
func TestBlockedActionKeyExplainsItself(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.route = RouteDetail
	m.selection = wf.Summary.Ref
	m.SetConnectionState(false, "")
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.actionView == nil || m.actionView.State() != actions.StateUnavailable {
		t.Fatalf("blocked action key did not open an explanation: %v", m.actionView)
	}
	if !strings.Contains(m.actionView.View().Content, "connection lost") {
		t.Fatalf("reason not shown: %q", m.actionView.View().Content)
	}
	// Still no mutation may start.
	if cmd := m.startAction(core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionResume, Confirmation: core.Confirmation{Confirmed: true}}); cmd != nil {
		t.Fatal("action started while disconnected")
	}
}
