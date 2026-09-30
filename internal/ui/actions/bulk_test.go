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

// Menus offer only applicable verbs and ignore keys for unavailable actions.
func TestMenuAvailability(t *testing.T) {
	for _, c := range []struct {
		name         string
		targets      []core.Summary
		want, absent []string
		ignored      string
	}{
		{"mixed", mixed(), []string{"[u] resume     applies to 1 of 3", "[z] suspend    applies to 1 of 3", "[r] retry      applies to 1 of 3", "[b] resubmit   applies to 1 of 3", "[s] stop       applies to 2 of 3", "[t] terminate  applies to 2 of 3", "[d] delete     applies to 3 of 3"}, nil, ""},
		{"finished bulk", []core.Summary{summary("a", "Succeeded", false), summary("b", "Succeeded", false)}, []string{"[b] resubmit", "[d] delete", "not applicable to any marked workflow: resume, suspend, retry, stop, terminate"}, []string{"[s] stop"}, "s"},
		{"finished single", []core.Summary{summary("wf", "Succeeded", false)}, []string{"[b] resubmit", "[d] delete"}, []string{"[u] resume", "[z] suspend"}, "u"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := bulkModel(c.targets)
			m.OpenMenu()
			view := m.View().Content
			for _, want := range c.want {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q: %s", want, view)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(view, absent) {
					t.Fatalf("offered %q: %s", absent, view)
				}
			}
			if c.ignored != "" {
				if cmd := press(m, c.ignored); cmd != nil || m.State() != StateMenu {
					t.Fatalf("ignored key: state=%v command=%v", m.state, cmd != nil)
				}
			}
		})
	}
}

// Confirmation lists applicable targets up to eight and counts omitted and skipped workflows.
func TestBulkConfirmationListsTheTargets(t *testing.T) {
	var targets []core.Summary
	for i := 0; i < 11; i++ {
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

// Confirmation emits one ordered request per applicable target without repeating it.
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

// Bulk terminate requires the applicable count, or the name when only one target applies.
func TestBulkTerminateGate(t *testing.T) {
	for _, c := range []struct {
		name                 string
		targets              []core.Summary
		prompt, wrong, right string
		refs                 []string
		confirmation         core.Confirmation
	}{
		{"count", mixed(), "Type 2 (the number of workflows)", "gate", "2", []string{"gate", "busy"}, core.Confirmation{Confirmed: true, BulkSize: 2, TypedCount: "2"}},
		{"one applicable", []core.Summary{summary("gate", "Running", true), summary("finished", "Succeeded", false)}, "Type gate to confirm", "1", "gate", []string{"gate"}, core.Confirmation{Confirmed: true, TypedName: "gate"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := bulkModel(c.targets)
			m.OpenMenu()
			press(m, "t")
			if m.State() != StateTypedName || !strings.Contains(m.View().Content, c.prompt) {
				t.Fatalf("state=%v view=%s", m.state, m.View().Content)
			}
			press(m, c.wrong)
			if cmd := press(m, "enter"); cmd != nil || m.State() != StateTypedName {
				t.Fatal("wrong text passed gate")
			}
			for range len(c.wrong) {
				m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			press(m, c.right)
			cmd := press(m, "enter")
			if cmd == nil {
				t.Fatal("correct text produced no intent")
			}
			intent := cmd().(BulkIntentMsg)
			if len(intent.Requests) != len(c.refs) {
				t.Fatalf("requests=%+v", intent.Requests)
			}
			for i, r := range intent.Requests {
				if r.Ref.Name != c.refs[i] || r.Action != core.ActionTerminate || r.Confirmation != c.confirmation {
					t.Fatalf("request[%d]=%+v", i, r)
				}
			}
		})
	}
}

// Delete requires a separate capital D confirmation and cancels on every other key.
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
		if cmd := press(m, repeat); cmd != nil || m.State() != StateIdle {
			t.Fatalf("%q on the final screen: state=%v, want cancelled", repeat, m.State())
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

// Escape stops future bulk sends while the current request remains in flight.
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

// Bulk results report each outcome and total with uncertain results first.
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
