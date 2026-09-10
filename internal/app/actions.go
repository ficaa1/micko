package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
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
	m.setInflight("action", cancel)
	m.actionAttempt++
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.actionAttempt}
	return func() tea.Msg {
		actual, err := m.deps.reader.Get(ctx, req.Ref)
		if err != nil {
			return actionResultMsg{genStamp: g, Result: unknownAction(req), Err: err}
		}
		if err := req.Validate(actual.Summary.Ref); err != nil {
			return actionResultMsg{genStamp: g, Result: unknownAction(req), Err: err}
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

// stopObservation bounds how long an accepted Stop is watched for a terminal
// phase. A graceful Stop runs the workflow's exit handler first, so the phase
// can lag the accepted request by many seconds.
// They are variables, not constants, so a test can shorten the budget without
// waiting out a real exit handler.
var (
	stopObservationInterval = time.Second
	stopObservationAttempts = 30
)

// readBack observes the state that follows an accepted mutation. settled
// reports whether the expected end state was seen inside the budget; false
// means the mutation is applied but still in progress, never that it failed.
func readBack(ctx context.Context, reader core.Reader, ref core.Ref, action core.Action) (wf core.Workflow, settled bool, err error) {
	wf, err = reader.Get(ctx, ref)
	if err != nil {
		return core.Workflow{}, false, err
	}
	if action != core.ActionStop && action != core.ActionTerminate {
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
	m.clearInflight("action")
	if m.actionView == nil {
		return nil
	}
	// An error alongside an accepted result describes a failed observation,
	// not a failed mutation, so the accepted outcome stands. Only a result
	// that is neither confirmed nor accepted degrades to unknown.
	if msg.Err != nil && msg.Result.Outcome != core.ActionAccepted && msg.Result.Outcome != core.ActionUnknown {
		msg.Result.Outcome = core.ActionUnknown
	}
	m.actionView.SetOutcome(msg.Result)
	return nil
}
