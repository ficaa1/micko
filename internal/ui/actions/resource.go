package actions

import (
	"strings"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

type ResourceIntentMsg struct{ Request core.ResourceActionRequest }

func (m *Model) SetResource(state core.ResourceState, namespace string, hidden bool) {
	m.resource = &state
	m.ref = state.Ref
	m.destination = namespace
	m.SetParameters(state.Arguments, hidden)
}

func (m *Model) menuActions() []core.Action {
	if m.resource == nil {
		return core.Actions
	}
	if m.resource.Kind == core.KindCronWorkflow {
		return []core.Action{core.ActionSubmit, core.ActionSuspend, core.ActionResume}
	}
	return []core.Action{core.ActionSubmit}
}

func renderResource(m *Model) string {
	var b strings.Builder
	b.WriteString(string(m.resource.Kind) + " action\n")
	b.WriteString("target: " + target(m.ref) + "\n")
	b.WriteString("server: " + shared.Sanitize(m.server) + "\n")
	b.WriteString("profile: " + shared.Sanitize(m.profile) + "  namespace: " + shared.Sanitize(m.destination) + "\n")
	switch m.state {
	case StateUnavailable:
		b.WriteString("unavailable: " + shared.Sanitize(m.reason) + "\n")
	case StateMenu:
		label := "submit"
		if m.resource.Kind == core.KindCronWorkflow {
			label = "run now"
		}
		b.WriteString("[g] " + label)
		for _, a := range m.Available() {
			if a.Action != core.ActionSubmit {
				b.WriteString("  [" + menuKey(a.Action) + "] " + string(a.Action))
			}
		}
		b.WriteString("  [esc] cancel\n")
	case StateConfirm, StateEditParam:
		if m.action == core.ActionSubmit {
			b.WriteString("Create one new workflow in " + shared.Sanitize(m.destination) + ".\n")
			renderParams(&b, m)
		} else {
			b.WriteString(strings.ToUpper(string(m.action)) + " scheduled runs; active runs are unchanged.\n")
		}
		if m.reason != "" {
			b.WriteString(shared.Sanitize(m.reason) + "\n")
		}
		if m.state == StateEditParam {
			b.WriteString("enter keep  ctrl+backspace clear  esc discard\n")
		} else {
			b.WriteString("[y] Confirm  [enter/esc] Cancel\n")
		}
	case StateSubmitting:
		b.WriteString("Checking identity and sending once; waiting for the server.\n")
	case StateOutcome:
		if m.outcome != nil {
			b.WriteString(strings.ToUpper(string(m.outcome.Outcome)) + "\n")
			if m.outcome.Outcome == core.ActionUnknown {
				b.WriteString("It may have been applied. Inspect resources before retrying.\n")
			}
			if m.outcome.Outcome == core.ActionRefused {
				b.WriteString("Nothing was sent.\n")
			}
			if m.outcome.Outcome == core.ActionAccepted {
				b.WriteString("Applied; resulting state could not be observed. Do not resend.\n")
			}
		}
		if m.reason != "" {
			b.WriteString(shared.Sanitize(m.reason) + "\n")
		}
	}
	return b.String()
}

func (m *Model) SetReason(reason string) { m.reason = reason }
