package actions

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func consequence(action core.Action) string {
	switch action {
	case core.ActionRetry:
		return "restart the existing workflow from its failed nodes, keeping its name"
	case core.ActionResume:
		return "resume the workflow's manual approval gate"
	case core.ActionSuspend:
		return "suspend the workflow: running steps finish, no new step starts until it is resumed"
	case core.ActionResubmit:
		return "create a new workflow from the same spec, under a new name"
	case core.ActionStop:
		return "stop the workflow while allowing exit handlers"
	case core.ActionTerminate:
		return "irreversibly terminate the workflow without exit handlers"
	case core.ActionDelete:
		return "permanently delete the workflow and its node status from the cluster"
	default:
		return "perform an action"
	}
}

// confirmListMax bounds the targets a bulk confirmation names one by one.
// Past it the rest are counted, so the confirmation always fits the pane
// and the prompt line under the list is never pushed off the screen.
const confirmListMax = 8

// outcomeListMin is the fewest target lines a bulk outcome shows, whatever
// the terminal height.
const outcomeListMin = 3

// render draws the pane. The theme styles its parts without changing a
// character, so the plain rendering reads the same.
func render(m *Model) string {
	t := m.theme
	var b strings.Builder
	line := func(label, value string, style lipgloss.Style) {
		b.WriteString(t.Muted.Render(label+":") + " " + style.Render(value) + "\n")
	}
	plain := lipgloss.NewStyle()
	if m.bulk {
		b.WriteString(t.Title.Render("bulk workflow action") + "\n")
		line("targets", plural(len(m.targets), "marked workflow"), t.Text)
		line("server", shared.Sanitize(m.server), t.Text)
		line("profile", shared.Sanitize(m.profile), t.Text)
	} else {
		b.WriteString(t.Title.Render("workflow action") + "\n")
		line("target", target(m.ref), t.Text)
		line("server", shared.Sanitize(m.server), t.Text)
		line("profile", shared.Sanitize(m.profile), t.Text)
		line("phase", shared.Sanitize(m.phase), t.PhaseStyle(m.phase))
	}
	switch m.state {
	case StateUnavailable:
		line("status", "unavailable", t.Warning)
		line("reason", shared.Sanitize(m.reason), plain)
	case StateMenu:
		renderMenu(&b, m)
	case StateConfirm:
		line("action", actionLabel(m.action), t.Accent)
		if m.bulk {
			line("consequence", consequence(m.action)+", on each of "+plural(len(m.applies), "workflow")+":", plain)
			renderTargetList(&b, m.applies, confirmListMax)
			renderLeftAlone(&b, m)
			b.WriteString("Requests are sent one at a time; none is ever resent.\n")
		} else {
			line("consequence", consequence(m.action)+" on "+target(m.ref), plain)
		}
		if m.action == core.ActionDelete {
			b.WriteString(t.Warning.Render("Delete has a second, final confirmation after this one.") + "\n")
		}
		b.WriteString("Confirm? " + keys(t, "[y] Yes  [n/esc/enter] Cancel (Cancel is the default)") + "\n")
	case StateTypedName:
		if m.bulk {
			line("action", "TERMINATE on "+plural(len(m.applies), "workflow"), t.ErrorText)
			b.WriteString(t.ErrorText.Render("WARNING: terminate is irreversible and permanently stops these workflows.") + "\n")
			renderTargetList(&b, m.applies, confirmListMax)
			renderLeftAlone(&b, m)
			if len(m.applies) == 1 {
				b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.typedGate()), shared.Sanitize(m.typedName)))
			} else {
				b.WriteString(fmt.Sprintf("Type %s (the number of workflows) to confirm: %s\n", m.typedGate(), shared.Sanitize(m.typedName)))
			}
		} else {
			line("action", "TERMINATE", t.ErrorText)
			b.WriteString(t.ErrorText.Render("WARNING: terminate is irreversible and permanently stops this workflow.") + "\n")
			b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.ref.Name), shared.Sanitize(m.typedName)))
		}
		b.WriteString(keys(t, "[enter] Submit  [esc] Cancel") + "\n")
	case StateFinal:
		line("action", "DELETE (final confirmation)", t.ErrorText)
		if m.bulk {
			b.WriteString(t.ErrorText.Render("WARNING: delete cannot be undone. "+plural(len(m.applies), "workflow")+" will be removed from the cluster:") + "\n")
			renderTargetList(&b, m.applies, confirmListMax)
		} else {
			b.WriteString(t.ErrorText.Render("WARNING: delete cannot be undone. "+target(m.ref)+" will be removed from the cluster.") + "\n")
		}
		b.WriteString("Press " + t.HintKey.Render("D") + " (shift+d) to delete. Any other key cancels.\n")
	case StateSubmitting:
		if m.bulk {
			line("action", actionLabel(m.action), t.Accent)
			line("status", fmt.Sprintf("%d of %d finished; one request at a time, waiting for the server", m.sent, len(m.applies)), t.Accent)
			if m.stopRequested {
				line("stopping", "nothing more is sent after the request in flight", t.Warning)
			}
		} else {
			line("status", "sent one request; waiting for the server", t.Accent)
		}
	case StateOutcome:
		if m.bulk {
			renderBulkOutcome(&b, m)
			break
		}
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

// renderMenu lists the actions that apply to at least one target. A bulk
// menu says how many targets each one reaches, because an action offered
// for three marked workflows may apply to only one of them.
func renderMenu(b *strings.Builder, m *Model) {
	t := m.theme
	avail := m.Available()
	if m.bulk {
		b.WriteString("choose an action (each is confirmed before anything is sent):\n")
		for _, a := range avail {
			b.WriteString(keys(t, fmt.Sprintf("  [%s] %-10s applies to %d of %d", menuKey(a.Action), a.Action, a.Applies, len(m.targets))) + "\n")
		}
		if un := m.Unavailable(); len(un) > 0 {
			b.WriteString(t.Muted.Render("not applicable to any marked workflow: "+joinActions(un)) + "\n")
		}
		b.WriteString(keys(t, "  [esc] cancel") + "\n")
		return
	}
	b.WriteString("choose action (all require confirmation):\n")
	var ks []string
	for _, a := range avail {
		ks = append(ks, "["+menuKey(a.Action)+"] "+string(a.Action))
	}
	b.WriteString(keys(t, strings.Join(append(ks, "[esc] cancel"), "  ")) + "\n")
	if un := m.Unavailable(); len(un) > 0 {
		b.WriteString(t.Muted.Render("not offered in this phase: "+joinActions(un)) + "\n")
	}
}

func joinActions(as []core.Action) string {
	s := make([]string, len(as))
	for i, a := range as {
		s[i] = string(a)
	}
	return strings.Join(s, ", ")
}

// renderTargetList names targets one per line, clipped to max with a count
// of the rest.
func renderTargetList(b *strings.Builder, targets []core.Summary, max int) {
	for i, t := range targets {
		if i == max {
			b.WriteString(fmt.Sprintf("  … and %d more\n", len(targets)-max))
			break
		}
		b.WriteString("  " + shortTarget(t.Ref) + "\n")
	}
}

// renderLeftAlone says how many marked workflows the action skips. They are
// part of the selection the reader built, so leaving them out silently
// would read as the action reaching them.
func renderLeftAlone(b *strings.Builder, m *Model) {
	if skipped := len(m.targets) - len(m.applies); skipped > 0 {
		b.WriteString(m.theme.Warning.Render(fmt.Sprintf("%s left alone: %s does not apply to %s.",
			plural(skipped, "marked workflow"), m.action, pronoun(skipped))) + "\n")
	}
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// outcomeOrder puts the outcomes that need the reader first: an UNKNOWN
// must be inspected, a REFUSED was not sent, and both are what a clipped
// list must not hide.
var outcomeOrder = []core.ActionOutcome{core.ActionUnknown, core.ActionRefused, core.ActionAccepted, core.ActionConfirmed}

// outcomeStyle colours an outcome word the way the single-workflow outcome
// line colours it.
func outcomeStyle(t shared.Theme, o core.ActionOutcome) lipgloss.Style {
	switch o {
	case core.ActionConfirmed:
		return t.PhaseSucceeded
	case core.ActionAccepted:
		return t.PhaseRunning
	case core.ActionRefused:
		return t.Warning
	default:
		return t.ErrorText
	}
}

func renderBulkOutcome(b *strings.Builder, m *Model) {
	t := m.theme
	b.WriteString(t.Muted.Render("action:") + " " + t.Accent.Render(actionLabel(m.action)) + "\n")
	b.WriteString(t.Muted.Render("total:") + " " + m.bulkTotals() + "\n")
	limit := len(m.items)
	if m.height > 0 {
		// The frame, the header above and the total line take about
		// sixteen rows of a terminal; the rest is for the targets.
		limit = m.height - 16
		if limit < outcomeListMin {
			limit = outcomeListMin
		}
	}
	shown := 0
	for _, o := range outcomeOrder {
		for _, it := range m.items {
			if it.Result.Outcome != o {
				continue
			}
			if shown == limit {
				b.WriteString(fmt.Sprintf("  … and %d more\n", len(m.items)-limit))
				return
			}
			shown++
			word := outcomeStyle(t, o).Render(fmt.Sprintf("%-9s", strings.ToUpper(string(o))))
			b.WriteString("  " + word + "  " + shortTarget(it.Result.Target))
			if detail := itemDetail(it); detail != "" {
				b.WriteString(" — " + detail)
			}
			b.WriteString("\n")
		}
	}
}

// itemDetail is the short explanation after one target's outcome.
func itemDetail(it BulkItem) string {
	switch it.Result.Outcome {
	case core.ActionConfirmed, core.ActionAccepted:
		if ref := it.Result.Affected; ref != nil && ref.Name != "" && ref.Name != it.Result.Target.Name {
			return "new workflow " + shared.Sanitize(ref.Name)
		}
		if wf := it.Result.Workflow; wf != nil && wf.Summary.Phase != "" {
			return "phase " + shared.Sanitize(wf.Summary.Phase)
		}
		if it.Result.Action == core.ActionDelete && it.Result.Outcome == core.ActionConfirmed {
			return "gone"
		}
		return ""
	case core.ActionRefused:
		return "not sent: " + shared.Sanitize(it.Reason)
	default:
		return "may have been applied; inspect before retrying"
	}
}

// bulkTotals is the count of each outcome, in the order the list uses.
func (m *Model) bulkTotals() string {
	counts := map[core.ActionOutcome]int{}
	for _, it := range m.items {
		counts[it.Result.Outcome]++
	}
	parts := make([]string, 0, len(outcomeOrder))
	for i := len(outcomeOrder) - 1; i >= 0; i-- {
		o := outcomeOrder[i]
		parts = append(parts, fmt.Sprintf("%d %s", counts[o], o))
	}
	return strings.Join(parts, ", ") + " (" + plural(len(m.items), "workflow") + ")"
}

// shortTarget names a workflow by namespace and name. A bulk list has no
// room for the UID on every line; the identity preflight checks it anyway.
func shortTarget(ref core.Ref) string {
	return shared.Sanitize(ref.Namespace) + "/" + shared.Sanitize(ref.Name)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
