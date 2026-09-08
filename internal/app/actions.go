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
	if !m.actionOpts.AllowActions || m.actionOpts.ReadOnly || m.actionOpts.Demo || m.deps.actioner == nil {
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
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
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
		ref := req.Ref
		if result.Affected != nil {
			ref = *result.Affected
		}
		wf, readErr := readBack(ctx, m.deps.reader, ref, req.Action)
		if readErr != nil {
			result.Outcome = core.ActionUnknown
			return actionResultMsg{genStamp: g, Result: result, Err: readErr}
		}
		result.Workflow = &wf
		result.Outcome = core.ActionConfirmed
		return actionResultMsg{genStamp: g, Result: result}
	}
}

func unknownAction(req core.ActionRequest) core.ActionResult {
	return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}
}

func readBack(ctx context.Context, reader core.Reader, ref core.Ref, action core.Action) (core.Workflow, error) {
	wf, err := reader.Get(ctx, ref)
	if err != nil {
		return core.Workflow{}, err
	}
	if action != core.ActionStop && action != core.ActionTerminate {
		return wf, nil
	}
	// Status may be delayed after a successful PUT. Poll briefly, bounded and
	// cancelable; timeout becomes unknown rather than false success.
	for i := 0; i < 10 && !terminalPhase(wf.Summary.Phase); i++ {
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return core.Workflow{}, ctx.Err()
		case <-timer.C:
		}
		wf, err = reader.Get(ctx, ref)
		if err != nil {
			return core.Workflow{}, err
		}
	}
	if !terminalPhase(wf.Summary.Phase) {
		return core.Workflow{}, errors.New("action status did not reach a terminal phase before observation deadline")
	}
	return wf, nil
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
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil
	}
	m.clearInflight("action")
	if m.actionView == nil {
		return nil
	}
	if msg.Err != nil && msg.Result.Outcome != core.ActionUnknown {
		msg.Result.Outcome = core.ActionUnknown
	}
	m.actionView.SetOutcome(msg.Result)
	return nil
}
