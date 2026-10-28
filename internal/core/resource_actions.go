package core

import (
	"context"
	"errors"
	"slices"
	"strings"
)

type ResourceKind string

const (
	KindWorkflowTemplate        ResourceKind = "WorkflowTemplate"
	KindClusterWorkflowTemplate ResourceKind = "ClusterWorkflowTemplate"
	KindCronWorkflow            ResourceKind = "CronWorkflow"
	ActionSubmit                Action       = "submit"
)

type ResourceState struct {
	Kind      ResourceKind
	Ref       Ref
	Arguments []Argument
	Suspended bool
}

type ResourceActionRequest struct {
	Resource ResourceState
	Action   Action
	// Namespace is the workflow destination, including for cluster templates.
	Namespace    string
	Parameters   []string
	Confirmation Confirmation
}

func (r ResourceActionRequest) Validate() error {
	if !r.Confirmation.Confirmed {
		return ErrActionNotConfirmed
	}
	if r.Resource.Ref.Name == "" || r.Resource.Ref.UID == "" || r.Namespace == "" {
		return errors.New("resource identity or destination namespace is missing")
	}
	switch r.Resource.Kind {
	case KindClusterWorkflowTemplate:
		if r.Resource.Ref.Namespace != "" {
			return errors.New("cluster template must have no namespace")
		}
	case KindWorkflowTemplate, KindCronWorkflow:
		if r.Resource.Ref.Namespace != r.Namespace {
			return errors.New("resource and destination namespace differ")
		}
	default:
		return errors.New("unsupported resource kind")
	}
	if r.Action == ActionSubmit {
		return nil
	}
	if r.Resource.Kind == KindCronWorkflow && (r.Action == ActionSuspend || r.Action == ActionResume) {
		return nil
	}
	return errors.New("unsupported resource action")
}

// ResourceReader reads an originating resource for an identity preflight.
type ResourceReader interface {
	GetResource(context.Context, ResourceKind, Ref) (ResourceState, error)
}

// ResourceActioner sends one template or cron mutation, without retries.
type ResourceActioner interface {
	ExecuteResourceAction(context.Context, ResourceActionRequest) (ActionResult, error)
}

func (r ResourceActionRequest) ValidateParameters(actual ResourceState) error {
	if r.Action != ActionSubmit {
		return nil
	}
	edits := map[string]string{}
	for _, pair := range r.Parameters {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			return errors.New("invalid parameter override")
		}
		edits[name] = value
	}
	for _, p := range actual.Arguments {
		value, edited := edits[p.Name]
		if !edited && p.Required() {
			return errors.New("supply every required parameter before submitting")
		}
		if edited && len(p.Enum) > 0 && !slices.Contains(p.Enum, value) {
			return errors.New("a parameter override is outside its allowed values")
		}
		delete(edits, p.Name)
	}
	if len(edits) > 0 {
		return errors.New("parameter definitions changed; reopen the action")
	}
	return nil
}
