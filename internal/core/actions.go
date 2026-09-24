package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Action is one whole-workflow mutation. Each request addresses exactly one
// workflow; a bulk action is a sequence of these, one per target, so every
// rule below holds per workflow. Node-specific, parameter-editing and
// workflow-submission operations are deliberately not part of this contract.
type Action string

const (
	ActionResume    Action = "resume"
	ActionSuspend   Action = "suspend"
	ActionRetry     Action = "retry"
	ActionResubmit  Action = "resubmit"
	ActionStop      Action = "stop"
	ActionTerminate Action = "terminate"
	ActionDelete    Action = "delete"
)

// Actions lists every supported action in menu order.
var Actions = []Action{ActionResume, ActionSuspend, ActionRetry, ActionResubmit, ActionStop, ActionTerminate, ActionDelete}

// Supported reports whether a is one of the actions above. Validation and
// the transport reject anything else before a request is built.
func (a Action) Supported() bool {
	for _, known := range Actions {
		if a == known {
			return true
		}
	}
	return false
}

// AppliesTo reports whether a can act on a workflow in the state s reports.
// The menu offers only the actions that apply, and the preflight checks the
// rule again against a fresh read: Argo answers an action that does not
// apply with an error, and an error after a send can only be reported as
// UNKNOWN, so a request that cannot succeed is better never sent.
//
//   - resume: a suspended workflow that has not finished.
//   - suspend: a workflow that is still running and not already suspended.
//   - stop, terminate: a workflow that is still running.
//   - retry: a workflow that failed or errored.
//   - resubmit: a finished workflow, whatever its result.
//   - delete: any workflow.
//
// "Still running" is every phase that is not finished, including Pending,
// an empty phase and a phase this build does not know.
func (a Action) AppliesTo(s Summary) bool {
	finished := FinishedPhase(s.Phase)
	switch a {
	case ActionResume:
		return s.Suspended && !finished
	case ActionSuspend:
		return !finished && !s.Suspended
	case ActionStop, ActionTerminate:
		return !finished
	case ActionRetry:
		return s.Phase == "Failed" || s.Phase == "Error"
	case ActionResubmit:
		return finished
	case ActionDelete:
		return true
	default:
		return false
	}
}

// FinishedPhase reports whether phase is one a workflow never leaves by
// itself: Succeeded, Failed or Error.
func FinishedPhase(phase string) bool {
	switch phase {
	case "Succeeded", "Failed", "Error":
		return true
	default:
		return false
	}
}

// Confirmation is UI-originated and is never serialized into an Argo body.
// A zero value is Cancel, so callers must opt in explicitly.
type Confirmation struct {
	Confirmed bool
	// TypedName is the workflow name the operator typed for a single
	// terminate.
	TypedName string
	// BulkSize and TypedCount stand in for TypedName when one terminate
	// covers several workflows: the operator types the number of targets,
	// and BulkSize is the number the confirmation showed. Typing the count
	// proves the operator read how many workflows the request reaches,
	// which is the mistake a bulk terminate invites.
	BulkSize   int
	TypedCount string
	// Final is the second, separate confirmation a delete needs. Only the
	// final delete screen sets it, so a confirmation that stopped at the
	// first step can never delete.
	Final bool
}

var (
	ErrActionNotConfirmed       = errors.New("action not confirmed")
	ErrConfirmationNameMismatch = errors.New("confirmation name does not match workflow name")
	ErrDeleteNotFinal           = errors.New("delete needs its final confirmation")
	ErrActionDisabled           = errors.New("actions are disabled")
)

// NotApplicableError is returned by the preflight when a fresh read shows a
// workflow the action does not apply to.
type NotApplicableError struct {
	Action Action
	Phase  string
}

func (e *NotApplicableError) Error() string {
	phase := e.Phase
	if phase == "" {
		phase = "(no phase)"
	}
	return fmt.Sprintf("%s does not apply to this workflow now (phase %s)", e.Action, phase)
}

// UIDMismatchError is returned when the preflight identity differs from the
// selected identity. Argo action bodies have no UID precondition; this check
// reduces, but cannot eliminate, a same-name replacement race.
type UIDMismatchError struct{ Expected, Actual Ref }

func (e *UIDMismatchError) Error() string {
	return fmt.Sprintf("workflow UID mismatch: expected %q, got %q", e.Expected.UID, e.Actual.UID)
}

// ActionRequest combines the selected identity, safe action options, and
// confirmation. Ref.UID is for preflight validation only and is never sent:
// Argo's action endpoints address a workflow by name, and its delete
// endpoint builds its own delete options, so a UID precondition sent with
// the request would never reach Kubernetes.
type ActionRequest struct {
	Ref               Ref
	Action            Action
	NodeFieldSelector string
	RestartSuccessful bool
	Memoized          bool
	Parameters        []string
	Message           string
	Confirmation      Confirmation
}

func (r ActionRequest) Validate(actual Ref) error {
	if !r.Action.Supported() {
		return fmt.Errorf("unsupported action %q", r.Action)
	}
	if r.Ref.Namespace != actual.Namespace || r.Ref.Name != actual.Name || r.Ref.UID != actual.UID {
		return &UIDMismatchError{Expected: r.Ref, Actual: actual}
	}
	return r.CheckConfirmation()
}

// CheckConfirmation applies the confirmation rules alone. The transport
// calls it as a last gate: it has no fresh identity to compare against, but
// it must still refuse an unconfirmed request.
func (r ActionRequest) CheckConfirmation() error {
	if !r.Action.Supported() {
		return fmt.Errorf("unsupported action %q", r.Action)
	}
	c := r.Confirmation
	if !c.Confirmed {
		return ErrActionNotConfirmed
	}
	if r.Action == ActionTerminate && c.TypedName != r.Ref.Name &&
		(c.BulkSize < 2 || c.TypedCount != strconv.Itoa(c.BulkSize)) {
		return ErrConfirmationNameMismatch
	}
	if r.Action == ActionDelete && !c.Final {
		return ErrDeleteNotFinal
	}
	return nil
}

// These bodies mirror the Argo OpenAPI schemas exactly. UID and UI
// confirmation fields are intentionally absent. Delete sends no body.
type ResumeBody struct {
	Name              string `json:"name,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	NodeFieldSelector string `json:"nodeFieldSelector,omitempty"`
}

type RetryBody struct {
	Name              string   `json:"name,omitempty"`
	Namespace         string   `json:"namespace,omitempty"`
	NodeFieldSelector string   `json:"nodeFieldSelector,omitempty"`
	Parameters        []string `json:"parameters,omitempty"`
	RestartSuccessful bool     `json:"restartSuccessful,omitempty"`
}

type ResubmitBody struct {
	Memoized   bool     `json:"memoized,omitempty"`
	Name       string   `json:"name,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
}

type SuspendBody struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type StopBody struct {
	Message           string `json:"message,omitempty"`
	Name              string `json:"name,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	NodeFieldSelector string `json:"nodeFieldSelector,omitempty"`
}

type TerminateBody struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

func (r ActionRequest) WireBody() any {
	switch r.Action {
	case ActionResume:
		return ResumeBody{r.Ref.Name, r.Ref.Namespace, r.NodeFieldSelector}
	case ActionSuspend:
		return SuspendBody{r.Ref.Name, r.Ref.Namespace}
	case ActionRetry:
		return RetryBody{r.Ref.Name, r.Ref.Namespace, r.NodeFieldSelector, r.Parameters, r.RestartSuccessful}
	case ActionResubmit:
		return ResubmitBody{r.Memoized, r.Ref.Name, r.Ref.Namespace, r.Parameters}
	case ActionStop:
		return StopBody{r.Message, r.Ref.Name, r.Ref.Namespace, r.NodeFieldSelector}
	case ActionTerminate:
		return TerminateBody{r.Ref.Name, r.Ref.Namespace}
	default:
		return nil
	}
}

type ActionOutcome string

const (
	// ActionConfirmed means the server accepted the request and a read-back
	// observed the expected resulting state.
	ActionConfirmed ActionOutcome = "confirmed"
	// ActionAccepted means the server returned success, so the mutation was
	// definitely applied, but the expected end state was not observed within
	// the observation budget. A graceful Stop that is still running exit
	// handlers lands here. It is certain, unlike ActionUnknown, and must
	// never be resent either: the request has already taken effect.
	ActionAccepted ActionOutcome = "accepted"
	// ActionUnknown means it is not known whether the request was applied.
	ActionUnknown ActionOutcome = "unknown"
	// ActionRefused means no request was ever sent: it failed a check on
	// this side. Nothing changed on the server, so there is nothing to
	// inspect and the action can be corrected and repeated.
	ActionRefused ActionOutcome = "refused"
)

// ActionResult contains the exact target and any affected/new identity read
// from the server. Unknown means the request may have been applied; callers
// must inspect before retrying and must never auto-resubmit. Accepted means
// it was applied but has not finished; callers must not resend it either.
type ActionResult struct {
	Action   Action
	Target   Ref
	Affected *Ref
	Outcome  ActionOutcome
	Workflow *Workflow
	Response json.RawMessage
}

// Actioner sends one action request and never retries it. The transport owns
// exact HTTP details; orchestration owns read-back and ambiguity handling.
type Actioner interface {
	Execute(context.Context, ActionRequest) (ActionResult, error)
}
