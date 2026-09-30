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

// A request passes only for the selected workflow with the confirmation its
// action needs, and the transport's gate agrees whenever the identity matches.
func TestActionRequestValidation(t *testing.T) {
	selected := Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	replaced := Ref{Namespace: "ns", Name: "wf", UID: "new"}
	cases := []struct {
		name    string
		action  Action
		confirm Confirmation
		actual  *Ref
		wantErr string
		is      error
	}{
		{"resume confirmed", ActionResume, Confirmation{Confirmed: true}, nil, "", nil},
		{"resume cancelled", ActionResume, Confirmation{}, nil, "action not confirmed", ErrActionNotConfirmed},
		{"replaced workflow", ActionResume, Confirmation{Confirmed: true}, &replaced,
			`workflow UID mismatch: expected "uid", got "new"`, nil},
		{"unknown action", Action("explode"), Confirmation{Confirmed: true, Final: true}, nil, `unsupported action "explode"`, nil},
		{"terminate with the typed name", ActionTerminate, Confirmation{Confirmed: true, TypedName: "wf"}, nil, "", nil},
		{"terminate with another name", ActionTerminate, Confirmation{Confirmed: true, TypedName: "other"}, nil,
			"confirmation name does not match workflow name", ErrConfirmationNameMismatch},
		{"bulk terminate with the count", ActionTerminate, Confirmation{Confirmed: true, BulkSize: 3, TypedCount: "3"}, nil, "", nil},
		{"bulk terminate with another count", ActionTerminate, Confirmation{Confirmed: true, BulkSize: 3, TypedCount: "2"}, nil,
			"confirmation name does not match workflow name", ErrConfirmationNameMismatch},
		{"bulk terminate with no count", ActionTerminate, Confirmation{Confirmed: true, BulkSize: 3}, nil,
			"confirmation name does not match workflow name", ErrConfirmationNameMismatch},
		{"single terminate with a count", ActionTerminate, Confirmation{Confirmed: true, BulkSize: 1, TypedCount: "1"}, nil,
			"confirmation name does not match workflow name", ErrConfirmationNameMismatch},
		{"delete after the first step", ActionDelete, Confirmation{Confirmed: true}, nil,
			"delete needs its final confirmation", ErrDeleteNotFinal},
		{"delete after the final step", ActionDelete, Confirmation{Confirmed: true, Final: true}, nil, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := ActionRequest{Ref: selected, Action: c.action, Confirmation: c.confirm}
			actual := selected
			if c.actual != nil {
				actual = *c.actual
			}
			err := req.Validate(actual)
			if got := errText(err); got != c.wantErr {
				t.Fatalf("Validate = %q, want %q", got, c.wantErr)
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("Validate = %v, want it to match %v", err, c.is)
			}
			if c.actual == nil {
				if got := errText(req.CheckConfirmation()); got != c.wantErr {
					t.Errorf("CheckConfirmation = %q, want %q", got, c.wantErr)
				}
			}
		})
	}
}

// errText returns err's message, or "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Each action's body carries only the fields Argo's schema defines for it,
// never the UID, and delete sends none.
func TestActionWireBodies(t *testing.T) {
	cases := []struct {
		action Action
		want   string
	}{
		{ActionResume, `{"name":"wf","namespace":"ns","nodeFieldSelector":"phase=Failed"}`},
		{ActionSuspend, `{"name":"wf","namespace":"ns"}`},
		{ActionRetry, `{"name":"wf","namespace":"ns","nodeFieldSelector":"phase=Failed","parameters":["x=y"],"restartSuccessful":true}`},
		{ActionResubmit, `{"memoized":true,"name":"wf","namespace":"ns","parameters":["x=y"]}`},
		{ActionStop, `{"message":"operator stop","name":"wf","namespace":"ns","nodeFieldSelector":"phase=Failed"}`},
		{ActionTerminate, `{"name":"wf","namespace":"ns"}`},
		{ActionDelete, `null`},
	}
	for _, c := range cases {
		t.Run(string(c.action), func(t *testing.T) {
			req := ActionRequest{
				Ref:    Ref{Namespace: "ns", Name: "wf", UID: "secret-uid"},
				Action: c.action, NodeFieldSelector: "phase=Failed", RestartSuccessful: true, Memoized: true,
				Parameters: []string{"x=y"}, Message: "operator stop",
				Confirmation: Confirmation{Confirmed: true, TypedName: "wf", Final: true},
			}
			body, err := json.Marshal(req.WireBody())
			if err != nil || string(body) != c.want {
				t.Fatalf("body = %s, err = %v; want %s", body, err, c.want)
			}
		})
	}
}
