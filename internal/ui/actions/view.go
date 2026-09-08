package actions

import (
	"fmt"
	"strings"

	"argo-tui/internal/core"
)

func render(m *Model) string {
	var b strings.Builder
	b.WriteString("workflow action\n")
	b.WriteString("target: " + target(m.ref) + "\n")
	switch m.state {
	case StateUnavailable:
		b.WriteString("status: unavailable\nreason: " + m.reason + "\n")
	case StateConfirm:
		b.WriteString("action: " + actionLabel(m.action) + "\n")
		if m.action == core.ActionStop {
			b.WriteString("STOP pauses/cancels running work; it may be possible to resume or inspect the result.\n")
		} else {
			b.WriteString("This requests a new server-side operation; review the target before continuing.\n")
		}
		b.WriteString("Confirm? [y/enter] Yes  [n/esc] Cancel (Cancel is the default)\n")
	case StateTypedName:
		b.WriteString("action: TERMINATE\n")
		b.WriteString("WARNING: terminate is irreversible and permanently stops this workflow.\n")
		b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", m.ref.Name, m.typedName))
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
