package actions

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
)

func summary(name, phase string, suspended bool) core.Summary {
	return core.Summary{Ref: core.Ref{Namespace: "ns", Name: name, UID: "uid-" + name}, Phase: phase, Suspended: suspended}
}

func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
			if k == "D" {
				msg = tea.KeyPressMsg{Code: 'd', ShiftedCode: 'D', Mod: tea.ModShift, Text: "D"}
			}
		}
		_, cmd = m.Update(msg)
	}
	return cmd
}

// mixed is three marked workflows in three different states.
func mixed() []core.Summary {
	return []core.Summary{
		summary("gate", "Running", true),
		summary("broken", "Failed", false),
		summary("busy", "Running", false),
	}
}

func bulkModel(targets []core.Summary) *Model {
	m := NewWithOptions(targets[0].Ref, Options{AllowActions: true, Server: "https://argo.test", Profile: "dev"})
	m.SetTargets(targets)
	return m
}

// The bulk menu offers every verb that applies to at least one target and
// says how many each reaches; verbs that apply to none are named apart.
func TestBulkMenuOffersVerbsThatApplyToAnyTarget(t *testing.T) {
	m := bulkModel(mixed())
	m.OpenMenu()
	want := map[core.Action]int{
		core.ActionResume:    1, // gate
		core.ActionSuspend:   1, // busy
		core.ActionRetry:     1, // broken
		core.ActionResubmit:  1, // broken
		core.ActionStop:      2, // gate, busy
		core.ActionTerminate: 2,
		core.ActionDelete:    3,
	}
	got := map[core.Action]int{}
	for _, a := range m.Available() {
		got[a.Action] = a.Applies
	}
	for a, n := range want {
		if got[a] != n {
			t.Errorf("%s applies to %d, want %d", a, got[a], n)
		}
	}
	view := m.View().Content
	for _, line := range []string{"[t] terminate  applies to 2 of 3", "[d] delete     applies to 3 of 3", "[u] resume     applies to 1 of 3"} {
		if !strings.Contains(view, line) {
			t.Errorf("menu lacks %q:\n%s", line, view)
		}
	}

	finished := bulkModel([]core.Summary{summary("a", "Succeeded", false), summary("b", "Succeeded", false)})
	finished.OpenMenu()
	view = finished.View().Content
	if strings.Contains(view, "[s] stop") || !strings.Contains(view, "not applicable to any marked workflow: resume, suspend, retry, stop, terminate") {
		t.Fatalf("finished targets offered running verbs:\n%s", view)
	}
	press(finished, "s")
	if finished.State() != StateMenu {
		t.Fatalf("an unoffered verb's key left the menu: state=%v", finished.State())
	}
}

// A single workflow's menu hides the verbs its phase rules out.
func TestSingleMenuHidesVerbsThePhaseRulesOut(t *testing.T) {
	m := NewWithOptions(core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Options{AllowActions: true})
	m.SetTargets([]core.Summary{summary("wf", "Succeeded", false)})
	m.OpenMenu()
	view := m.View().Content
	if !strings.Contains(view, "[b] resubmit") || !strings.Contains(view, "[d] delete") || strings.Contains(view, "[u] resume") || strings.Contains(view, "[z] suspend") {
		t.Fatalf("menu = %s", view)
	}
	press(m, "u")
	if m.State() != StateMenu {
		t.Fatalf("resume opened on a finished workflow: state=%v", m.State())
	}
}

// The confirmation names the count, lists the targets it reaches (clipped
// with a count of the rest), and says which marked workflows it leaves
// alone.
func TestBulkConfirmationListsTheTargets(t *testing.T) {
	var targets []core.Summary
	for i := 0; i < confirmListMax+3; i++ {
		targets = append(targets, summary(fmt.Sprintf("run-%02d", i), "Failed", false))
	}
	targets = append(targets, summary("fine", "Succeeded", false))
	m := bulkModel(targets)
	m.OpenMenu()
	press(m, "r")
	view := m.View().Content
	for _, want := range []string{
		"on each of 11 workflows",
		"ns/run-00",
		"ns/run-07",
		"… and 3 more",
		"1 marked workflow left alone: retry does not apply to it.",
		"[y] Yes",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "ns/run-08") || strings.Contains(view, "ns/fine") {
		t.Fatalf("confirmation listed past the clip or a target it skips:\n%s", view)
	}
}

// y emits one request per applicable target, in target order, and nothing
// for the targets the verb does not apply to.
func TestBulkConfirmEmitsOneRequestPerApplicableTarget(t *testing.T) {
	m := bulkModel(mixed())
	m.OpenMenu()
	press(m, "s")
	cmd := press(m, "y")
	if cmd == nil || m.State() != StateSubmitting {
		t.Fatalf("no intent: state=%v", m.State())
	}
	intent, ok := cmd().(BulkIntentMsg)
	if !ok {
		t.Fatalf("intent type = %T", cmd())
	}
	if len(intent.Requests) != 2 || intent.Requests[0].Ref.Name != "gate" || intent.Requests[1].Ref.Name != "busy" {
		t.Fatalf("requests = %+v", intent.Requests)
	}
	for _, r := range intent.Requests {
		if r.Action != core.ActionStop || !r.Confirmation.Confirmed {
			t.Fatalf("request = %+v", r)
		}
	}
	if again := press(m, "y"); again != nil {
		t.Fatal("a second y emitted a second intent")
	}
}

// A bulk terminate is confirmed by typing the number of workflows; the
// name of one of them does not pass.
func TestBulkTerminateTypesTheCount(t *testing.T) {
	m := bulkModel(mixed())
	m.OpenMenu()
	press(m, "t")
	if m.State() != StateTypedName || !strings.Contains(m.View().Content, "Type 2 (the number of workflows)") {
		t.Fatalf("typed gate view = %s", m.View().Content)
	}
	press(m, "g", "a", "t", "e", "enter")
	if m.State() != StateTypedName {
		t.Fatal("a name passed the count gate")
	}
	m.SetTypedName("2")
	cmd := press(m, "enter")
	intent := cmd().(BulkIntentMsg)
	for _, r := range intent.Requests {
		if err := r.Validate(r.Ref); err != nil {
			t.Fatalf("request %s does not validate: %v", r.Ref.Name, err)
		}
	}
}

// Delete takes two steps: y, then a capital D on a separate screen. The d
// that opened it and the y that passed the first step, pressed again,
// cancel instead of deleting.
func TestDeleteNeedsASecondDistinctConfirmation(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "u"}
	for _, repeat := range []string{"d", "y", "enter", "esc"} {
		m := NewWithOptions(ref, Options{AllowActions: true})
		m.OpenMenu()
		press(m, "d", "y")
		if m.State() != StateFinal {
			t.Fatalf("y did not lead to the final screen: state=%v", m.State())
		}
		if !strings.Contains(m.View().Content, "Press D (shift+d) to delete") {
			t.Fatalf("final screen = %s", m.View().Content)
		}
		if cmd := press(m, repeat); cmd != nil || m.State() != StateIdle || m.IntentCount() != 0 {
			t.Fatalf("%q on the final screen: state=%v intents=%d, want cancelled", repeat, m.State(), m.IntentCount())
		}
	}
	m := NewWithOptions(ref, Options{AllowActions: true})
	m.OpenMenu()
	press(m, "d", "y")
	cmd := press(m, "D")
	if cmd == nil {
		t.Fatal("D did not confirm the delete")
	}
	req := cmd().(ActionIntentMsg).Request
	if req.Action != core.ActionDelete || !req.Confirmation.Confirmed || !req.Confirmation.Final {
		t.Fatalf("request = %+v", req)
	}
}

// A bulk run can be stopped between requests; the request in flight is
// not affected.
func TestEscDuringABulkRunAsksToStop(t *testing.T) {
	m := bulkModel(mixed())
	m.OpenMenu()
	press(m, "s", "y")
	press(m, "esc")
	if !m.StopRequested() || m.State() != StateSubmitting {
		t.Fatalf("stop=%v state=%v", m.StopRequested(), m.State())
	}
	if !strings.Contains(m.View().Content, "nothing more is sent") {
		t.Fatalf("view = %s", m.View().Content)
	}
}

// The bulk outcome lists every target with its outcome, the ones that need
// the reader first, and a total line.
func TestBulkOutcomeListsEachTargetAndTheTotal(t *testing.T) {
	targets := []core.Summary{summary("a", "Running", false), summary("b", "Running", false), summary("c", "Running", false), summary("d", "Running", false)}
	m := bulkModel(targets)
	m.OpenMenu()
	press(m, "s", "y")
	m.SetBulkOutcome([]BulkItem{
		{Result: core.ActionResult{Action: core.ActionStop, Target: targets[0].Ref, Outcome: core.ActionConfirmed, Workflow: &core.Workflow{Summary: core.Summary{Phase: "Failed"}}}},
		{Result: core.ActionResult{Action: core.ActionStop, Target: targets[1].Ref, Outcome: core.ActionRefused}, Reason: "workflow UID mismatch"},
		{Result: core.ActionResult{Action: core.ActionStop, Target: targets[2].Ref, Outcome: core.ActionUnknown}},
		{Result: core.ActionResult{Action: core.ActionStop, Target: targets[3].Ref, Outcome: core.ActionAccepted}},
	})
	view := m.View().Content
	for _, want := range []string{
		"total: 1 confirmed, 1 accepted, 1 refused, 1 unknown (4 workflows)",
		"UNKNOWN    ns/c — may have been applied; inspect before retrying",
		"REFUSED    ns/b — not sent: workflow UID mismatch",
		"ACCEPTED   ns/d",
		"CONFIRMED  ns/a — phase Failed",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("outcome lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "UNKNOWN") > strings.Index(view, "CONFIRMED") {
		t.Fatalf("unknown is listed after confirmed:\n%s", view)
	}
	if got := m.OutcomeLine(); got != "stop: 1 confirmed, 1 accepted, 1 refused, 1 unknown (4 workflows)" {
		t.Fatalf("outcome line = %q", got)
	}
}

// The demo, a read-only session and a session without --allow-actions all
// show the bulk menu in its unavailable state and can emit nothing.
func TestBulkIsUnavailableWithoutActions(t *testing.T) {
	for name, opts := range map[string]Options{
		"demo":      {AllowActions: true, Demo: true},
		"read-only": {AllowActions: true, ReadOnly: true},
		"disabled":  {},
	} {
		m := NewWithOptions(mixed()[0].Ref, opts)
		m.SetTargets(mixed())
		m.OpenMenu()
		if m.State() != StateUnavailable || !strings.Contains(m.View().Content, "targets: 3 marked workflows") {
			t.Fatalf("%s: state=%v view=%s", name, m.State(), m.View().Content)
		}
		if cmd := press(m, "s", "y", "D"); cmd != nil {
			t.Fatalf("%s: keys emitted an intent", name)
		}
	}
}
