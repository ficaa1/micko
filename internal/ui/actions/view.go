package actions

import (
	"fmt"
	"strings"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
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

func render(m *Model) string {
	var b strings.Builder
	if m.bulk {
		b.WriteString("bulk workflow action\n")
		b.WriteString("targets: " + plural(len(m.targets), "marked workflow") + "\n")
		b.WriteString("server: " + shared.Sanitize(m.server) + "\n")
		b.WriteString("profile: " + shared.Sanitize(m.profile) + "\n")
	} else {
		b.WriteString("workflow action\n")
		b.WriteString("target: " + target(m.ref) + "\n")
		b.WriteString("server: " + shared.Sanitize(m.server) + "\n")
		b.WriteString("profile: " + shared.Sanitize(m.profile) + "\n")
		b.WriteString("phase: " + shared.Sanitize(m.phase) + "\n")
	}
	switch m.state {
	case StateUnavailable:
		b.WriteString("status: unavailable\nreason: " + shared.Sanitize(m.reason) + "\n")
	case StateMenu:
		renderMenu(&b, m)
	case StateConfirm:
		b.WriteString("action: " + actionLabel(m.action) + "\n")
		if m.bulk {
			b.WriteString("consequence: " + consequence(m.action) + ", on each of " + plural(len(m.applies), "workflow") + ":\n")
			renderTargetList(&b, m.applies, confirmListMax)
			renderLeftAlone(&b, m)
			b.WriteString("Requests are sent one at a time; none is ever resent.\n")
		} else {
			b.WriteString("consequence: " + consequence(m.action) + " on " + target(m.ref) + "\n")
		}
		if m.action == core.ActionDelete {
			b.WriteString("Delete has a second, final confirmation after this one.\n")
		}
		b.WriteString("Confirm? [y] Yes  [n/esc/enter] Cancel (Cancel is the default)\n")
	case StateTypedName:
		if m.bulk {
			b.WriteString("action: TERMINATE on " + plural(len(m.applies), "workflow") + "\n")
			b.WriteString("WARNING: terminate is irreversible and permanently stops these workflows.\n")
			renderTargetList(&b, m.applies, confirmListMax)
			renderLeftAlone(&b, m)
			if len(m.applies) == 1 {
				b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.typedGate()), shared.Sanitize(m.typedName)))
			} else {
				b.WriteString(fmt.Sprintf("Type %s (the number of workflows) to confirm: %s\n", m.typedGate(), shared.Sanitize(m.typedName)))
			}
		} else {
			b.WriteString("action: TERMINATE\n")
			b.WriteString("WARNING: terminate is irreversible and permanently stops this workflow.\n")
			b.WriteString(fmt.Sprintf("Type %s to confirm: %s\n", shared.Sanitize(m.ref.Name), shared.Sanitize(m.typedName)))
		}
		b.WriteString("[enter] Submit  [esc] Cancel\n")
	case StateFinal:
		b.WriteString("action: DELETE (final confirmation)\n")
		if m.bulk {
			b.WriteString("WARNING: delete cannot be undone. " + plural(len(m.applies), "workflow") + " will be removed from the cluster:\n")
			renderTargetList(&b, m.applies, confirmListMax)
		} else {
			b.WriteString("WARNING: delete cannot be undone. " + target(m.ref) + " will be removed from the cluster.\n")
		}
		b.WriteString("Press D (shift+d) to delete. Any other key cancels.\n")
	case StateSubmitting:
		if m.bulk {
			b.WriteString("action: " + actionLabel(m.action) + "\n")
			b.WriteString(fmt.Sprintf("status: %d of %d finished; one request at a time, waiting for the server\n", m.sent, len(m.applies)))
			if m.stopRequested {
				b.WriteString("stopping: nothing more is sent after the request in flight\n")
			}
		} else {
			b.WriteString("status: sent one request; waiting for the server\n")
		}
	case StateOutcome:
		if m.bulk {
			renderBulkOutcome(&b, m)
			break
		}
		if m.outcome == nil {
			b.WriteString("outcome: unknown\n")
		} else {
			switch m.outcome.Outcome {
			case core.ActionConfirmed:
				b.WriteString("outcome: confirmed\n")
			case core.ActionAccepted:
				b.WriteString("outcome: ACCEPTED — the server applied the action; it has not finished yet.\n")
				if m.outcome.Workflow != nil {
					b.WriteString("phase: " + shared.Sanitize(m.outcome.Workflow.Summary.Phase) + " (watch the workflow for the final state)\n")
				}
			case core.ActionRefused:
				b.WriteString("outcome: REFUSED — the request was not sent; nothing changed.\n")
			case core.ActionUnknown:
				b.WriteString("outcome: UNKNOWN — it is not known whether the server applied the action.\n")
				if m.outcome.Workflow != nil {
					b.WriteString("phase now: " + shared.Sanitize(m.outcome.Workflow.Summary.Phase) + "\n")
				}
			default:
				b.WriteString("outcome: unknown\n")
			}
		}
	default:
		b.WriteString("status: idle\n")
	}
	return b.String()
}

// renderMenu lists the actions that apply to at least one target. A bulk
// menu says how many targets each one reaches, because an action offered
// for three marked workflows may apply to only one of them.
func renderMenu(b *strings.Builder, m *Model) {
	avail := m.Available()
	if m.bulk {
		b.WriteString("choose an action (each is confirmed before anything is sent):\n")
		for _, a := range avail {
			b.WriteString(fmt.Sprintf("  [%s] %-10s applies to %d of %d\n", menuKey(a.Action), a.Action, a.Applies, len(m.targets)))
		}
		if un := m.Unavailable(); len(un) > 0 {
			b.WriteString("not applicable to any marked workflow: " + joinActions(un) + "\n")
		}
		b.WriteString("  [esc] cancel\n")
		return
	}
	b.WriteString("choose action (all require confirmation):\n")
	var keys []string
	for _, a := range avail {
		keys = append(keys, "["+menuKey(a.Action)+"] "+string(a.Action))
	}
	b.WriteString(strings.Join(append(keys, "[esc] cancel"), "  ") + "\n")
	if un := m.Unavailable(); len(un) > 0 {
		b.WriteString("not offered in this phase: " + joinActions(un) + "\n")
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
		b.WriteString(fmt.Sprintf("%s left alone: %s does not apply to %s.\n",
			plural(skipped, "marked workflow"), m.action, pronoun(skipped)))
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

func renderBulkOutcome(b *strings.Builder, m *Model) {
	b.WriteString("action: " + actionLabel(m.action) + "\n")
	b.WriteString("total: " + m.bulkTotals() + "\n")
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
			b.WriteString(fmt.Sprintf("  %-9s  %s", strings.ToUpper(string(o)), shortTarget(it.Result.Target)))
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
