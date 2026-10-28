package app

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/journal"
	"github.com/ficaa1/micko/internal/ui/actions"
)

func (m *Root) openResourceActions() tea.Cmd {
	var state core.ResourceState
	hidden := m.redact
	switch m.route {
	case RouteCron:
		row, ok := m.cronView.Selected()
		if !ok {
			m.flash = "no cron workflow selected"
			return nil
		}
		state = core.ResourceState{Kind: core.KindCronWorkflow, Ref: core.Ref{Namespace: row.Namespace, Name: row.Name, UID: row.UID}, Arguments: row.Arguments, Suspended: row.Suspend}
		hidden = !m.cronView.ValuesRevealed()
	case RouteTemplates, RouteClusterTemplates:
		view := m.tmplView
		kind := core.KindWorkflowTemplate
		if m.route == RouteClusterTemplates {
			view = m.ctmplView
			kind = core.KindClusterWorkflowTemplate
		}
		row, ok := view.Selected()
		if !ok {
			m.flash = "no template selected"
			return nil
		}
		state = core.ResourceState{Kind: kind, Ref: core.Ref{Namespace: row.Namespace, Name: row.Name, UID: row.UID}, Arguments: row.Arguments}
		hidden = !view.ValuesRevealed()
	default:
		m.flash = "actions: select a workflow, template or cron workflow"
		return nil
	}
	ns := state.Ref.Namespace
	if state.Kind == core.KindClusterWorkflowTemplate {
		ns = m.deps.namespace
	}
	m.actionView = m.newActionView(state.Ref)
	m.actionView.SetResource(state, ns, hidden)
	m.actionView.SetContext(m.actionOpts.Server, m.actionOpts.Profile, "")
	m.actionView.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	_, canRead := m.deps.reader.(core.ResourceReader)
	_, canWrite := m.deps.reader.(core.ResourceActioner)
	switch {
	case !m.connectionReady || !m.connectionFresh:
		m.actionView.OpenUnavailable(staleActionReason(m.connectionReady))
	case m.kindStates[m.route].lastErr != nil:
		m.actionView.OpenUnavailable("resource list is stale; refresh before acting")
	case !canRead || !canWrite:
		m.actionView.OpenUnavailable("this connection does not support resource actions")
	case ns == "":
		m.actionView.OpenUnavailable("select a destination namespace with :ns NAME")
	default:
		m.actionView.OpenMenu()
	}
	return nil
}

func (m *Root) startResourceAction(req core.ResourceActionRequest) tea.Cmd {
	if m.hasInflight("action") {
		return nil
	}
	reader, canRead := m.deps.reader.(core.ResourceReader)
	writer, canWrite := m.deps.reader.(core.ResourceActioner)
	refusal := ""
	switch {
	case !m.actionsEnabled():
		refusal = "actions are disabled; nothing sent"
	case !m.connectionReady || !m.connectionFresh:
		refusal = "connection or snapshot is stale; nothing sent"
	case !canRead || !canWrite:
		refusal = "resource actions are unavailable; nothing sent"
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.actionAttempt++
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.actionAttempt}
	m.setInflight("action", uint64(g.Attempt), cancel)
	x := m.actionExec()
	return func() tea.Msg {
		var result core.ActionResult
		var err error
		if refusal != "" {
			result = core.ActionResult{Action: req.Action, Target: req.Resource.Ref, Outcome: core.ActionRefused}
			err = errors.New(refusal)
		} else {
			result, err = runResourceAction(ctx, x.reader, reader, writer, req)
		}
		entry := journal.Entry{DestinationNamespace: req.Namespace, Time: x.clock.Now().UTC(), Profile: x.profile, Server: x.server, Kind: string(req.Resource.Kind), Namespace: req.Resource.Ref.Namespace, Name: req.Resource.Ref.Name, UID: req.Resource.Ref.UID, Verb: string(req.Action), Outcome: string(result.Outcome)}
		// Server errors can echo submitted values; the journal records only the outcome.
		if err != nil {
			entry.Error = "resource action " + string(result.Outcome)
		}
		return actionResultMsg{Resource: true, genStamp: g, Result: result, Err: err, JournalErr: x.journal.Record(entry)}
	}
}

func runResourceAction(ctx context.Context, workflows core.Reader, reader core.ResourceReader, writer core.ResourceActioner, req core.ResourceActionRequest) (core.ActionResult, error) {
	result := core.ActionResult{Action: req.Action, Target: req.Resource.Ref, Outcome: core.ActionRefused}
	if err := req.Validate(); err != nil {
		return result, err
	}
	actual, err := reader.GetResource(ctx, req.Resource.Kind, req.Resource.Ref)
	if err != nil {
		return result, errors.New("cannot read originating resource; nothing sent")
	}
	if actual.Kind != req.Resource.Kind || actual.Ref != req.Resource.Ref {
		return result, errors.New("originating resource identity changed; nothing sent")
	}
	if req.Action == core.ActionSuspend && actual.Suspended || req.Action == core.ActionResume && !actual.Suspended {
		return result, errors.New("cron suspend state changed; reopen the action")
	}
	if err := req.ValidateParameters(actual); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result, err = writer.ExecuteResourceAction(ctx, req)
	result.Action, result.Target = req.Action, req.Resource.Ref
	if err != nil || result.Outcome == core.ActionUnknown {
		if result.Outcome != core.ActionRefused {
			result.Outcome = core.ActionUnknown
		}
		return result, errors.New("resource action " + strings.ToUpper(string(result.Outcome)) + "; inspect before acting again")
	}
	result.Outcome = core.ActionAccepted
	if req.Action == core.ActionSubmit {
		if ref := result.Affected; ref != nil {
			wf, err := workflows.Get(ctx, *ref)
			if err == nil && wf.Summary.Ref == *ref {
				result.Workflow = &wf
				result.Outcome = core.ActionConfirmed
			}
		}
	} else {
		after, err := reader.GetResource(ctx, req.Resource.Kind, req.Resource.Ref)
		if err == nil && after.Ref == req.Resource.Ref && after.Suspended == (req.Action == core.ActionSuspend) {
			result.Outcome = core.ActionConfirmed
		}
	}
	return result, nil
}

func (m *Root) resourceIntentMatches(req core.ResourceActionRequest) bool {
	return m.actionView != nil && m.actionView.State() == actions.StateSubmitting && m.actionView.Ref() == req.Resource.Ref && m.actionView.Action() == req.Action
}
