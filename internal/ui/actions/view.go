package actions

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

func consequence(action core.Action) string {
	switch action {
	case core.ActionRetry:
		return "restart the existing workflow from its failed nodes, keeping its name"
	case core.ActionResume:
		return "resume the workflow's manual approval gate"
	case core.ActionResubmit:
		return "create a new workflow from the same spec, under a new name"
	case core.ActionStop:
		return "stop the workflow while allowing exit handlers"
	case core.ActionTerminate:
		return "irreversibly terminate the workflow without exit handlers"
	default:
		return "perform an action"
	}
}

// render draws the pane. Every line is "label: value" or a row of bracketed
// keys, and the theme styles those parts without changing a character, so
// the plain rendering is the text a test or a mono terminal reads.
func render(m *Model) string {
	t := m.theme
	var b strings.Builder
	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(t.Muted.Render(label+":") + " " + style.Render(value) + "\n")
	}
	plain := lipgloss.NewStyle()
	b.WriteString(t.Title.Render("workflow action") + "\n")
	line("target", target(m.ref), t.Text)
	line("server", shared.Sanitize(m.server), t.Text)
	line("profile", shared.Sanitize(m.profile), t.Text)
	line("phase", shared.Sanitize(m.phase), t.PhaseStyle(m.phase))
	switch m.state {
	case StateUnavailable:
		line("status", "unavailable", t.Warning)
		line("reason", shared.Sanitize(m.reason), plain)
	case StateMenu:
		b.WriteString("choose action (all require confirmation):\n")
		b.WriteString(keys(t, "[u] resume  [r] retry  [b] resubmit  [s] stop  [esc] cancel") + "\n")
	case StateConfirm:
		line("action", actionLabel(m.action), t.Accent)
		line("consequence", consequence(m.action)+" on "+target(m.ref), plain)
		b.WriteString("Confirm? " + keys(t, "[y] Yes  [n/esc/enter] Cancel (Cancel is the default)") + "\n")
	case StateTypedName:
		line("action", "TERMINATE", t.ErrorText)
		b.WriteString(t.ErrorText.Render("WARNING: terminate is irreversible and permanently stops this workflow.") + "\n")
		b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.ref.Name), shared.Sanitize(m.typedName)))
		b.WriteString(keys(t, "[enter] Submit  [esc] Cancel") + "\n")
	case StateSubmitting:
		line("status", "sent one request; waiting for the server", t.Accent)
	case StateOutcome:
		if m.outcome == nil {
			line("outcome", "unknown", t.Warning)
		} else {
			switch m.outcome.Outcome {
			case core.ActionConfirmed:
				line("outcome", "confirmed", t.PhaseSucceeded)
			case core.ActionAccepted:
				line("outcome", "ACCEPTED — the server applied the action; it has not finished yet.", t.PhaseRunning)
				if m.outcome.Workflow != nil {
					line("phase", shared.Sanitize(m.outcome.Workflow.Summary.Phase)+" (watch the workflow for the final state)", plain)
				}
			case core.ActionRefused:
				line("outcome", "REFUSED — the request was not sent; nothing changed.", t.Warning)
			case core.ActionUnknown:
				line("outcome", "UNKNOWN — it is not known whether the server applied the action.", t.ErrorText)
				if m.outcome.Workflow != nil {
					line("phase now", shared.Sanitize(m.outcome.Workflow.Summary.Phase), plain)
				}
			default:
				line("outcome", "unknown", t.Warning)
			}
		}
	default:
		line("status", "idle", plain)
	}
	return b.String()
}

// keys styles each bracketed key in a row of choices, such as "[y] Yes",
// as a key and leaves the rest of the text as it is.
func keys(t shared.Theme, row string) string {
	var b strings.Builder
	for {
		open := strings.Index(row, "[")
		if open < 0 {
			break
		}
		end := strings.Index(row[open:], "]")
		if end < 0 {
			break
		}
		end += open + 1
		b.WriteString(row[:open])
		b.WriteString(t.HintKey.Render(row[open:end]))
		row = row[end:]
	}
	b.WriteString(row)
	return b.String()
}
