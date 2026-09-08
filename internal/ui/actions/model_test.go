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

func TestTerminateKeyboardInputPreservesTextBackspacesAndBoundsPaste(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "Wf-01", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionTerminate)
	for _, text := range []string{"q", "N", "y", "r", "Wf"} {
		m.Update(tea.KeyPressMsg{Text: text})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Text: "f-01"})
	if got := m.TypedName(); got != "qNyrWf-01" {
		t.Fatalf("typed name = %q", got)
	}
	tooLong := strings.Repeat("x", maxTypedNameLength+1)
	m.SetTypedName("")
	m.Update(tea.KeyPressMsg{Text: tooLong})
	if len(m.TypedName()) != maxTypedNameLength {
		t.Fatalf("paste bound length = %d", len(m.TypedName()))
	}
}

func TestTerminateInputIsolatedAndExactEnterOnly(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionTerminate)
	for _, text := range []string{"q", "n", "y", "r"} {
		m.Update(tea.KeyPressMsg{Text: text})
	}
	if m.State() != StateTypedName || m.TypedName() != "qnyr" {
		t.Fatalf("input shortcuts leaked: state=%v text=%q", m.State(), m.TypedName())
	}
	m.SetTypedName("wf")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.State() != StateSubmitting || m.IntentCount() != 1 {
		t.Fatalf("exact name should submit: state=%v intents=%d", m.State(), m.IntentCount())
	}
}

func TestBareEnterCancelsConfirmationAndOutcomeIsDismissible(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := New(ref, true, false, false)
	m.Open(core.ActionRetry)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.State() != StateIdle || m.IntentCount() != 0 {
		t.Fatalf("bare enter must cancel: state=%v intents=%d", m.State(), m.IntentCount())
	}
	m.Open(core.ActionRetry)
	m.Update(tea.KeyPressMsg{Text: "y"})
	m.SetOutcome(core.ActionResult{Action: core.ActionRetry, Target: ref, Outcome: core.ActionConfirmed})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.State() != StateIdle {
		t.Fatalf("outcome should dismiss: state=%v", m.State())
	}
}

func TestRenderedActionTextSanitizesTargetAndStatesConsequences(t *testing.T) {
	ref := core.Ref{Namespace: "ns\x1b[31m", Name: "wf\x1b]8;;evil", UID: "uid"}
	m := New(ref, true, false, false)
	for _, action := range []core.Action{core.ActionRetry, core.ActionResubmit, core.ActionStop, core.ActionTerminate} {
		m.Open(action)
		view := m.View().Content
		if strings.Contains(view, "\x1b") || !strings.Contains(view, "ns") || !strings.Contains(view, "uid") {
			t.Fatalf("unsafe target rendering for %s: %q", action, view)
		}
		if !strings.Contains(view, "workflow") {
			t.Fatalf("missing full-target consequence copy for %s: %q", action, view)
		}
	}
}
