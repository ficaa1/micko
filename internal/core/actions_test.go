package core

import (
	"encoding/json"
	"errors"
	"testing"
)

// The availability table is what the menu offers and what the preflight
// re-checks. Each row is one action against one workflow state.
func TestActionAvailabilityByPhase(t *testing.T) {
	running := Summary{Phase: "Running"}
	pending := Summary{Phase: "Pending"}
	suspended := Summary{Phase: "Running", Suspended: true}
	succeeded := Summary{Phase: "Succeeded"}
	failed := Summary{Phase: "Failed"}
	errored := Summary{Phase: "Error"}
	unknown := Summary{Phase: "FuturePhase"}

	cases := []struct {
		action Action
		s      Summary
		want   bool
	}{
		{ActionResume, suspended, true},
		{ActionResume, running, false},
		{ActionResume, Summary{Phase: "Failed", Suspended: true}, false},
		{ActionSuspend, running, true},
		{ActionSuspend, pending, true},
		{ActionSuspend, suspended, false},
		{ActionSuspend, succeeded, false},
		{ActionStop, running, true},
		{ActionStop, suspended, true},
		{ActionStop, unknown, true},
		{ActionStop, failed, false},
		{ActionTerminate, running, true},
		{ActionTerminate, succeeded, false},
		{ActionRetry, failed, true},
		{ActionRetry, errored, true},
		{ActionRetry, succeeded, false},
		{ActionRetry, running, false},
		{ActionResubmit, succeeded, true},
		{ActionResubmit, failed, true},
		{ActionResubmit, running, false},
		{ActionDelete, running, true},
		{ActionDelete, succeeded, true},
		{ActionDelete, Summary{}, true},
		{Action("explode"), running, false},
	}
	for _, tc := range cases {
		if got := tc.action.AppliesTo(tc.s); got != tc.want {
			t.Errorf("%s on phase=%q suspended=%v: got %v, want %v", tc.action, tc.s.Phase, tc.s.Suspended, got, tc.want)
		}
	}
}

// Delete needs its final confirmation; a first yes alone must not pass.
func TestDeleteRequiresTheFinalConfirmation(t *testing.T) {
	ref := Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	req := ActionRequest{Ref: ref, Action: ActionDelete, Confirmation: Confirmation{Confirmed: true}}
	if err := req.Validate(ref); !errors.Is(err, ErrDeleteNotFinal) {
		t.Fatalf("delete without the final step: err = %v", err)
	}
	req.Confirmation.Final = true
	if err := req.Validate(ref); err != nil {
		t.Fatalf("delete with the final step: err = %v", err)
	}
}

// A bulk terminate is confirmed by typing the number of targets. The count
// must match the size the confirmation showed, and a single-target request
// cannot use the count at all.
func TestBulkTerminateIsConfirmedByTheTypedCount(t *testing.T) {
	ref := Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	ok := ActionRequest{Ref: ref, Action: ActionTerminate, Confirmation: Confirmation{Confirmed: true, BulkSize: 3, TypedCount: "3"}}
	if err := ok.Validate(ref); err != nil {
		t.Fatalf("matching count: %v", err)
	}
	for _, c := range []Confirmation{
		{Confirmed: true, BulkSize: 3, TypedCount: "2"},
		{Confirmed: true, BulkSize: 1, TypedCount: "1"},
		{Confirmed: true, BulkSize: 3},
	} {
		req := ActionRequest{Ref: ref, Action: ActionTerminate, Confirmation: c}
		if err := req.Validate(ref); !errors.Is(err, ErrConfirmationNameMismatch) {
			t.Errorf("confirmation %+v: err = %v, want a mismatch", c, err)
		}
	}
}

func TestSuspendBodyMatchesTheSchemaAndDeleteHasNone(t *testing.T) {
	ref := Ref{Namespace: "ns", Name: "wf", UID: "secret-uid"}
	body, err := json.Marshal(ActionRequest{Ref: ref, Action: ActionSuspend}.WireBody())
	if err != nil || string(body) != `{"name":"wf","namespace":"ns"}` {
		t.Fatalf("suspend body = %s, err = %v", body, err)
	}
	if b := (ActionRequest{Ref: ref, Action: ActionDelete}).WireBody(); b != nil {
		t.Fatalf("delete body = %#v, want none", b)
	}
}

func TestUnknownActionIsRejected(t *testing.T) {
	ref := Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	req := ActionRequest{Ref: ref, Action: "explode", Confirmation: Confirmation{Confirmed: true, Final: true}}
	if err := req.Validate(ref); err == nil {
		t.Fatal("an unknown action validated")
	}
	if err := req.CheckConfirmation(); err == nil {
		t.Fatal("an unknown action passed the confirmation check")
	}
}
