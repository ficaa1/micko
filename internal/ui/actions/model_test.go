package actions

import (
	"strings"
	"testing"

	"argo-tui/internal/core"
	tea "charm.land/bubbletea/v2"
)

func TestDisabledReadOnlyAndDemoCannotOpenAction(t *testing.T) {
	ref := core.Ref{Namespace: "workflows", Name: "train", UID: "uid-1"}
	for _, tc := range []struct {
		name string
		m    *Model
		want string
	}{
		{"disabled", New(ref, false, false, false), "disabled"},
		{"read-only", New(ref, true, true, false), "read-only"},
		{"demo", New(ref, true, false, true), "demo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.m.Open(core.ActionTerminate)
			if tc.m.State() != StateUnavailable || !strings.Contains(strings.ToLower(tc.m.View().Content), tc.want) {
				t.Fatalf("state=%v view=%q", tc.m.State(), tc.m.View().Content)
			}
		})
	}
}

func TestCancelIsDefaultAndShowsFullTarget(t *testing.T) {
	ref := core.Ref{Namespace: "workflows", Name: "train", UID: "uid-1"}
	m := New(ref, true, false, false)
	m.Open(core.ActionStop)
	if m.State() != StateConfirm || !strings.Contains(m.View().Content, "workflows/train") || !strings.Contains(m.View().Content, "Cancel") {
		t.Fatalf("confirmation view = %q", m.View().Content)
	}
	m.Cancel()
	if m.State() != StateIdle || m.IntentCount() != 0 {
		t.Fatalf("cancel state=%v intents=%d", m.State(), m.IntentCount())
	}
}

func TestTerminateRequiresTypedNameAndStopWordingDiffers(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionStop)
	if strings.Contains(strings.ToLower(m.View().Content), "irreversible") {
		t.Fatal("stop must not be described as irreversible")
	}
	m.Open(core.ActionTerminate)
	if m.State() != StateTypedName || !strings.Contains(strings.ToLower(m.View().Content), "type wf") {
		t.Fatalf("typed gate view = %q", m.View().Content)
	}
	m.SetTypedName("wrong")
	if m.Submit() || m.State() != StateTypedName {
		t.Fatal("wrong typed name must not submit")
	}
	m.SetTypedName("wf")
	if !m.Submit() || m.State() != StateSubmitting {
		t.Fatal("matching typed name must submit")
	}
	if m.Submit() || m.IntentCount() != 1 {
		t.Fatal("double submit must be suppressed")
	}
	intent := m.LastIntent()
	if intent.Ref != ref || intent.Action != core.ActionTerminate || !intent.Confirmation.Confirmed {
		t.Fatalf("intent=%#v", intent)
	}
}

func TestUnknownOutcomeIsExplicit(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionRetry)
	m.Confirm()
	if m.State() != StateSubmitting {
		t.Fatalf("state=%v", m.State())
	}
	m.SetOutcome(core.ActionResult{Action: core.ActionRetry, Target: ref, Outcome: core.ActionUnknown})
	if m.State() != StateOutcome || !strings.Contains(strings.ToLower(m.View().Content), "unknown") || strings.Contains(strings.ToLower(m.View().Content), "success") {
		t.Fatalf("unknown view = %q", m.View().Content)
	}
}

func TestTeaUpdateEmitsIntentOnlyAfterConfirmation(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionRetry)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil || m.State() != StateSubmitting {
		t.Fatalf("confirm update state=%v cmd=%v", m.State(), cmd == nil)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("expected intent message")
	}
}
