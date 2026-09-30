package actions

import (
	tea "charm.land/bubbletea/v2"
	"github.com/ficaa1/micko/internal/core"
	"strings"
	"testing"
)

// Safety modes refuse single and bulk actions before confirmation can emit a request.
func TestSafetyModesBlockActions(t *testing.T) {
	for _, c := range []struct {
		name    string
		options Options
		reason  string
	}{
		{"disabled", Options{}, "actions disabled"}, {"read-only", Options{AllowActions: true, ReadOnly: true}, "read-only"}, {"demo", Options{AllowActions: true, Demo: true}, "demo mode"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, bulk := range []bool{false, true} {
				for _, menu := range []bool{false, true} {
					m := NewWithOptions(summary("wf", "Running", false).Ref, c.options)
					if bulk {
						m.SetTargets(mixed())
					}
					if menu {
						m.OpenMenu()
					} else {
						m.Open(core.ActionTerminate)
					}
					if m.State() != StateUnavailable || !strings.Contains(m.View().Content, c.reason) {
						t.Fatalf("bulk=%v menu=%v state=%v view=%q", bulk, menu, m.State(), m.View().Content)
					}
					if bulk && !strings.Contains(m.View().Content, "targets: 3 marked workflows") {
						t.Fatal(m.View().Content)
					}
					for _, key := range []string{"s", "y", "D", "enter"} {
						if cmd := press(m, key); cmd != nil {
							t.Fatalf("%s emitted request", key)
						}
					}
				}
			}
		})
	}
}

// Unconfirmed keys cancel without sending the named workflow's action.
func TestConfirmationCancelsByDefault(t *testing.T) {
	for _, key := range []string{"n", "enter", "esc"} {
		t.Run(key, func(t *testing.T) {
			m := NewWithOptions(core.Ref{Namespace: "workflows", Name: "train", UID: "uid-1"}, Options{AllowActions: true})
			m.Open(core.ActionStop)
			for _, want := range []string{"workflows/train (UID: uid-1)", "Cancel is the default"} {
				if !strings.Contains(m.View().Content, want) {
					t.Fatalf("confirmation lacks %q: %s", want, m.View().Content)
				}
			}
			if cmd := press(m, key); cmd != nil || m.State() != StateIdle {
				t.Fatalf("state=%v command=%v", m.State(), cmd != nil)
			}
		})
	}
}

// An explicit yes emits the selected request once and leaves the pane waiting.
func TestSingleIntentOnce(t *testing.T) {
	m := NewWithOptions(core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}, Options{AllowActions: true})
	m.Open(core.ActionRetry)
	cmd := press(m, "y")
	if cmd == nil || m.State() != StateSubmitting {
		t.Fatalf("state=%v command=%v", m.State(), cmd != nil)
	}
	msg, ok := cmd().(ActionIntentMsg)
	want := core.ActionRequest{Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}}
	if !ok || msg.Request.Ref != want.Ref || msg.Request.Action != want.Action || msg.Request.Confirmation != want.Confirmation {
		t.Fatalf("intent=%+v", msg)
	}
	for _, key := range []string{"y", "enter", "esc"} {
		if cmd := press(m, key); cmd != nil || m.State() != StateSubmitting {
			t.Fatalf("%s: state=%v command=%v", key, m.State(), cmd != nil)
		}
	}
}

// Terminate requires the exact workflow name, entered as literal text.
func TestTerminateGate(t *testing.T) {
	m := NewWithOptions(core.Ref{Namespace: "ns", Name: "Wf-01", UID: "uid"}, Options{AllowActions: true})
	m.Open(core.ActionStop)
	if strings.Contains(strings.ToLower(m.View().Content), "irreversible") {
		t.Fatal(m.View().Content)
	}
	m.Open(core.ActionTerminate)
	if m.State() != StateTypedName || !strings.Contains(m.View().Content, "Type Wf-01") {
		t.Fatal(m.View().Content)
	}
	press(m, "wf-01")
	if cmd := press(m, "enter"); cmd != nil || m.State() != StateTypedName {
		t.Fatal("wrong case passed name gate")
	}
	for range 5 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	press(m, "Wf-01")
	cmd := press(m, "enter")
	if cmd == nil || m.State() != StateSubmitting {
		t.Fatalf("state=%v command=%v", m.State(), cmd != nil)
	}
	req := cmd().(ActionIntentMsg).Request
	if req.Ref != m.Ref() || req.Action != core.ActionTerminate || req.Confirmation != (core.Confirmation{Confirmed: true, TypedName: "Wf-01"}) {
		t.Fatalf("request=%+v", req)
	}
	if press(m, "enter") != nil {
		t.Fatal("second enter emitted a request")
	}
}

// Name entry preserves shortcut letters and rune editing while dropping controls and bounding paste.
func TestTerminateInput(t *testing.T) {
	for _, c := range []struct {
		name, input     string
		backspace, keys bool
		want            string
	}{
		{"literal shortcuts", "qnNyrWf", true, true, "qnNyrW"}, {"unicode", "é猫", true, false, "é"}, {"controls", "a\x00\x1bb\n", false, false, "ab"}, {"paste", strings.Repeat("猫", 257), false, false, strings.Repeat("猫", 256)},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := NewWithOptions(core.Ref{Name: "wf"}, Options{AllowActions: true})
			m.Open(core.ActionTerminate)
			if c.keys {
				for _, r := range c.input {
					m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				}
			} else {
				m.Update(tea.KeyPressMsg{Text: c.input})
			}
			if c.backspace {
				m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			if m.state != StateTypedName || m.typedName != c.want {
				t.Fatalf("state=%v text=%q want=%q", m.state, m.typedName, c.want)
			}
			if cmd := press(m, "esc"); cmd != nil || m.State() != StateIdle || m.typedName != "" {
				t.Fatalf("cancel state=%v text=%q", m.state, m.typedName)
			}
		})
	}
}

// Outcome reports distinguish uncertainty from success and can be dismissed without sending again.
func TestSingleOutcome(t *testing.T) {
	for _, c := range []struct {
		name         string
		outcome      core.ActionOutcome
		want, footer string
	}{
		{"unknown", core.ActionUnknown, "not known whether", "retry outcome unknown"}, {"confirmed", core.ActionConfirmed, "outcome: confirmed", "retry confirmed"}, {"accepted", core.ActionAccepted, "has not finished yet", "retry accepted — the server applied it, it has not finished"}, {"refused", core.ActionRefused, "nothing changed", "retry refused — nothing was sent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, key := range []string{"enter", "esc"} {
				m := NewWithOptions(core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}, Options{AllowActions: true})
				m.Open(core.ActionRetry)
				press(m, "y")
				m.SetOutcome(core.ActionResult{Action: core.ActionRetry, Target: m.Ref(), Outcome: c.outcome})
				view := m.View().Content
				if m.State() != StateOutcome || !strings.Contains(view, c.want) || m.OutcomeLine() != c.footer || (c.outcome == core.ActionUnknown && strings.Contains(strings.ToLower(view), "success")) {
					t.Fatalf("state=%v view=%q footer=%q", m.state, view, m.OutcomeLine())
				}
				if cmd := press(m, key); cmd != nil || m.State() != StateIdle {
					t.Fatalf("dismiss state=%v command=%v", m.state, cmd != nil)
				}
			}
		})
	}
}

// Confirmation sanitizes terminal text and names the target, context and action consequences.
func TestRenderedActionTextSanitizesTargetAndStatesConsequences(t *testing.T) {
	for _, c := range []struct {
		action      core.Action
		consequence string
	}{
		{core.ActionRetry, "restart the existing workflow"}, {core.ActionResubmit, "create a new workflow"}, {core.ActionStop, "allowing exit handlers"}, {core.ActionTerminate, "irreversible"},
	} {
		t.Run(string(c.action), func(t *testing.T) {
			m := NewWithOptions(core.Ref{Namespace: "ns\x1b[31m", Name: "wf\x1b]8;;evil", UID: "uid"}, Options{AllowActions: true, Server: "https://argo.test", Profile: "dev", Phase: "Running"})
			m.Open(c.action)
			view := m.View().Content
			if strings.Contains(view, "\x1b") {
				t.Fatalf("unsafe view=%q", view)
			}
			for _, want := range []string{"ns", "wf", "uid", c.consequence, "server: https://argo.test", "profile: dev", "phase: Running"} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q: %s", want, view)
				}
			}
		})
	}
}
