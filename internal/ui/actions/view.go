package actions

import (
	"fmt"
	"strings"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/shared"
)

func consequence(action core.Action) string {
	switch action {
	case core.ActionRetry:
		return "retry the existing workflow"
	case core.ActionResume:
		return "resume the workflow's manual approval gate"
	case core.ActionResubmit:
		return "create a new workflow"
	case core.ActionStop:
		return "stop the workflow while allowing exit handlers"
	case core.ActionTerminate:
		return "irreversibly terminate the workflow without exit handlers"
	default:
		return "perform an action"
	}
}

func render(m *Model) string {
	var b strings.Builder
	b.WriteString("workflow action\n")
	b.WriteString("target: " + target(m.ref) + "\n")
	b.WriteString("server: " + shared.Sanitize(m.server) + "\n")
	b.WriteString("profile: " + shared.Sanitize(m.profile) + "\n")
	b.WriteString("phase: " + shared.Sanitize(m.phase) + "\n")
	switch m.state {
	case StateUnavailable:
		b.WriteString("status: unavailable\nreason: " + shared.Sanitize(m.reason) + "\n")
	case StateMenu:
		b.WriteString("choose action (all require confirmation):\n")
		b.WriteString("[u] resume  [s] stop  [esc] cancel\n")
	case StateConfirm:
		b.WriteString("action: " + actionLabel(m.action) + "\n")
		b.WriteString("consequence: " + consequence(m.action) + " on " + target(m.ref) + "\n")
		b.WriteString("Confirm? [y] Yes  [n/esc/enter] Cancel (Cancel is the default)\n")
	case StateTypedName:
		b.WriteString("action: TERMINATE\n")
		b.WriteString("WARNING: terminate is irreversible and permanently stops this workflow.\n")
		b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.ref.Name), shared.Sanitize(m.typedName)))
		b.WriteString("[enter] Submit  [esc] Cancel\n")
	case StateSubmitting:
		b.WriteString("status: submitting one request; do not repeat\n")
	case StateOutcome:
		if m.outcome == nil {
			b.WriteString("outcome: unknown\n")
		} else {
			switch m.outcome.Outcome {
			case core.ActionConfirmed:
				b.WriteString("outcome: confirmed\n")
			case core.ActionUnknown:
				b.WriteString("outcome: UNKNOWN — the server may have applied the action; inspect before retrying\n")
			default:
				b.WriteString("outcome: unknown\n")
			}
		}
	default:
		b.WriteString("status: idle\n")
	}
	return b.String()
}
