package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
)

type actionResultMsg struct {
	genStamp
	Result core.ActionResult
	Err    error
}

// startAction performs the safety-critical sequence in one cancelable command:
// preflight identity, exactly one mutation, then authoritative read-back.
func (m *Root) startAction(req core.ActionRequest) tea.Cmd {
	if !m.actionOpts.AllowActions || m.actionOpts.ReadOnly || m.actionOpts.Demo || m.deps.actioner == nil || !m.connectionReady || !m.connectionFresh {
		return nil
	}
	if req.Ref.UID == "" || req.Ref.Namespace == "" || req.Ref.Name == "" {
		return nil
	}
	m.mu.Lock()
	_, alreadyRunning := m.inflight["action"]
	m.mu.Unlock()
	if alreadyRunning {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.actionAttempt++
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.actionAttempt}
	// The entry is tagged with the attempt the reply will carry, so a late
	// reply from an earlier attempt cannot retire this one.
	m.setInflight("action", uint64(m.actionAttempt), cancel)
	return func() tea.Msg {
		// Both checks run before anything is sent. A failure here changed
		// nothing on the server, so the outcome is refused rather than
		// unknown: there is nothing to go and inspect.
		actual, err := m.deps.reader.Get(ctx, req.Ref)
		if err != nil {
			return actionResultMsg{genStamp: g, Result: refusedAction(req), Err: err}
		}
		if err := req.Validate(actual.Summary.Ref); err != nil {
			return actionResultMsg{genStamp: g, Result: refusedAction(req), Err: err}
		}
		result, err := m.deps.actioner.Execute(ctx, req) // exactly one call
		if err != nil || result.Outcome == core.ActionUnknown {
			// An ambiguous send is never repeated. Best-effort inspection is
			// allowed, but the UI remains UNKNOWN regardless of what it finds.
			if inspected, inspectErr := m.deps.reader.Get(ctx, req.Ref); inspectErr == nil {
				result.Workflow = &inspected
			}
			result.Outcome = core.ActionUnknown
			if err == nil {
				err = errors.New("action outcome unknown; inspect before retrying")
			}
			return actionResultMsg{genStamp: g, Result: result, Err: err}
		}
		// The server returned success, so the mutation is applied. From here
		// on the outcome is at worst ACCEPTED; it never degrades to UNKNOWN,
		// because a failed observation says nothing about a request the
		// server already acknowledged.
		ref := req.Ref
		if result.Affected != nil {
			ref = *result.Affected
		}
		wf, settled, readErr := readBack(ctx, m.deps.reader, ref, req.Action)
		if readErr != nil {
			result.Outcome = core.ActionAccepted
			return actionResultMsg{genStamp: g, Result: result, Err: readErr}
		}
		result.Workflow = &wf
		if settled {
			result.Outcome = core.ActionConfirmed
		} else {
			result.Outcome = core.ActionAccepted
		}
		return actionResultMsg{genStamp: g, Result: result}
	}
}

func unknownAction(req core.ActionRequest) core.ActionResult {
	return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}
}

// refusedAction reports an action that never left this process.
func refusedAction(req core.ActionRequest) core.ActionResult {
	return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionRefused}
}

// stopObservation bounds how long an accepted Stop is watched for a terminal
// phase. A graceful Stop runs the workflow's exit handler first, so the phase
// can lag the accepted request by many seconds.
//
// The budget is short on purpose. The action pane is modal, so every second
// spent here is a second the reader stares at "waiting for the server" with
// no way forward. Five seconds separates a quick stop from a slow one; past
// that the outcome is reported as ACCEPTED and the detail pane, which
// refetches on the same poll, shows the phase settle in place.
//
// They are variables, not constants, so a test can shorten the budget without
// waiting out a real exit handler.
var (
	stopObservationInterval = time.Second
	stopObservationAttempts = 5
)

// readBack observes the state that follows an accepted mutation. settled
// reports whether the expected end state was seen inside the budget; false
// means the mutation is applied but still in progress, never that it failed.
func readBack(ctx context.Context, reader core.Reader, ref core.Ref, action core.Action) (wf core.Workflow, settled bool, err error) {
	wf, err = reader.Get(ctx, ref)
	if err != nil {
		return core.Workflow{}, false, err
	}
	switch action {
	case core.ActionStop, core.ActionTerminate:
		// Handled by the observation loop below.
	case core.ActionResume:
		// A resumed workflow is one that no longer holds an open gate.
		return wf, !wf.Summary.Suspended, nil
	case core.ActionRetry:
		// A retried workflow has left its terminal phase.
		return wf, !terminalPhase(wf.Summary.Phase), nil
	default:
		// Resubmit is read back under the new name, so the workflow
		// existing is the observation.
		return wf, true, nil
	}
	for i := 0; i < stopObservationAttempts && !terminalPhase(wf.Summary.Phase); i++ {
		timer := time.NewTimer(stopObservationInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			// Cancellation is not evidence about the workflow. Report the
			// last observed state as still in progress.
			return wf, false, nil
		case <-timer.C:
		}
		next, getErr := reader.Get(ctx, ref)
		if getErr != nil {
			return wf, false, getErr
		}
		wf = next
	}
	return wf, terminalPhase(wf.Summary.Phase), nil
}

func terminalPhase(phase string) bool {
	switch strings.ToLower(phase) {
	case "succeeded", "failed", "error", "killed", "stopped", "terminated":
		return true
	default:
		return false
	}
}

func (m *Root) handleActionResult(msg actionResultMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.actionAttempt {
		return nil
	}
	m.clearInflight("action", uint64(msg.Attempt))
	if m.actionView == nil {
		return nil
	}
	// An error alongside an accepted result describes a failed observation,
	// not a failed mutation, so the accepted outcome stands. Only a result
	// that is neither confirmed nor accepted degrades to unknown.
	if msg.Err != nil && msg.Result.Outcome != core.ActionAccepted &&
		msg.Result.Outcome != core.ActionUnknown && msg.Result.Outcome != core.ActionRefused {
		msg.Result.Outcome = core.ActionUnknown
	}
	m.actionView.SetOutcome(msg.Result)
	// A finished action hands the screen back by itself. Waiting for an Esc
	// left the reader one press away from the workflow they had just changed,
	// which is what "confirm action doesn't return" meant. The one-line
	// report moves to the footer of the route behind it.
	//
	// UNKNOWN is the exception: nobody knows whether the server applied it,
	// and that is precisely the state a reader must see rather than dismiss.
	if msg.Result.Outcome == core.ActionUnknown {
		return nil
	}
	m.flash = m.actionView.OutcomeLine()
	m.actionView.Close()
	if m.route == RouteDetail && m.selection.UID != "" {
		return m.startDetailFetch()
	}
	return nil
}
