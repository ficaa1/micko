package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Action is the intentionally small v0.2 mutation surface. Bulk, delete,
// suspend/resume, node-specific, parameter-editing, and workflow-submission
// operations are deliberately not part of this contract.
type Action string

const (
	ActionResume    Action = "resume"
	ActionRetry     Action = "retry"
	ActionResubmit  Action = "resubmit"
	ActionStop      Action = "stop"
	ActionTerminate Action = "terminate"
)

// Confirmation is UI-originated and is never serialized into an Argo body.
// A zero value is Cancel, so callers must opt in explicitly.
type Confirmation struct {
	Confirmed bool
	TypedName string
}

var (
	ErrActionNotConfirmed       = errors.New("action not confirmed")
	ErrConfirmationNameMismatch = errors.New("confirmation name does not match workflow name")
	ErrActionDisabled           = errors.New("actions are disabled")
)

// UIDMismatchError is returned when the preflight identity differs from the
// selected identity. Argo action bodies have no UID precondition; this check
// reduces, but cannot eliminate, a same-name replacement race.
type UIDMismatchError struct{ Expected, Actual Ref }

func (e *UIDMismatchError) Error() string {
	return fmt.Sprintf("workflow UID mismatch: expected %q, got %q", e.Expected.UID, e.Actual.UID)
}

// ActionRequest combines the selected identity, safe action options, and
// confirmation. Ref.UID is for preflight validation only and is never sent.
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
	if r.Action != ActionRetry && r.Action != ActionResubmit && r.Action != ActionStop && r.Action != ActionTerminate {
		return fmt.Errorf("unsupported action %q", r.Action)
	}
	if r.Ref.Namespace != actual.Namespace || r.Ref.Name != actual.Name || r.Ref.UID != actual.UID {
		return &UIDMismatchError{Expected: r.Ref, Actual: actual}
	}
	if !r.Confirmation.Confirmed {
		return ErrActionNotConfirmed
	}
	if r.Action == ActionTerminate && r.Confirmation.TypedName != r.Ref.Name {
		return ErrConfirmationNameMismatch
	}
	return nil
}

// These four bodies mirror the v4.1.2 OpenAPI schemas exactly. UID and UI
// confirmation fields are intentionally absent.
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
	ActionConfirmed ActionOutcome = "confirmed"
	ActionUnknown   ActionOutcome = "unknown"
)

// ActionResult contains the exact target and any affected/new identity read
// from the server. Unknown means the request may have been applied; callers
// must inspect before retrying and must never auto-resubmit.
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
