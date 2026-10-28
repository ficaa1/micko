package testkit

import (
	"context"
	"fmt"
	"strings"

	"github.com/ficaa1/micko/internal/core"
)

func (f *FakeReader) GetResource(ctx context.Context, kind core.ResourceKind, ref core.Ref) (core.ResourceState, error) {
	if err := ctx.Err(); err != nil {
		return core.ResourceState{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if kind == core.KindCronWorkflow {
		if f.CronErr != nil {
			return core.ResourceState{}, f.CronErr
		}
		for _, c := range f.CronWorkflows {
			if c.Namespace == ref.Namespace && c.Name == ref.Name {
				return core.ResourceState{Kind: kind, Ref: core.Ref{Namespace: c.Namespace, Name: c.Name, UID: c.UID}, Arguments: c.Arguments, Suspended: c.Suspend}, nil
			}
		}
	} else {
		items := f.WorkflowTemplates
		err := f.TemplateErr
		if kind == core.KindClusterWorkflowTemplate {
			items = f.ClusterWorkflowTemplates
			err = f.ClusterTemplateErr
		}
		if err != nil {
			return core.ResourceState{}, err
		}
		for _, c := range items {
			if c.Namespace == ref.Namespace && c.Name == ref.Name {
				return core.ResourceState{Kind: kind, Ref: core.Ref{Namespace: c.Namespace, Name: c.Name, UID: c.UID}, Arguments: c.Arguments}, nil
			}
		}
	}
	return core.ResourceState{}, core.NewAPIError(core.ErrNotFound, 404, "resource not found")
}

func (f *FakeReader) ExecuteResourceAction(ctx context.Context, req core.ResourceActionRequest) (core.ActionResult, error) {
	result := core.ActionResult{Action: req.Action, Target: req.Resource.Ref, Outcome: core.ActionRefused}
	if err := req.Validate(); err != nil {
		return result, err
	}
	actual, err := f.GetResource(ctx, req.Resource.Kind, req.Resource.Ref)
	if err != nil {
		return result, err
	}
	if actual.Ref != req.Resource.Ref {
		return result, &core.UIDMismatchError{Expected: req.Resource.Ref, Actual: actual.Ref}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Action == core.ActionSubmit {
		name := fmt.Sprintf("%s-%d", req.Resource.Ref.Name, len(f.Workflows)+1)
		wf := SyntheticWorkflow(req.Namespace, name, "Pending", FixtureEpoch)
		wf.Summary.Arguments = append([]core.Argument(nil), actual.Arguments...)
		for _, p := range req.Parameters {
			name, value, _ := strings.Cut(p, "=")
			for i := range wf.Summary.Arguments {
				if wf.Summary.Arguments[i].Name == name {
					wf.Summary.Arguments[i].Value = value
					wf.Summary.Arguments[i].HasValue = true
				}
			}
		}
		if f.Workflows == nil {
			f.Workflows = map[core.Ref]core.Workflow{}
		}
		f.Workflows[wf.Summary.Ref] = wf
		ref := wf.Summary.Ref
		result.Affected = &ref
		result.Workflow = &wf
	} else {
		for i, c := range f.CronWorkflows {
			if c.UID == actual.Ref.UID && c.Namespace == actual.Ref.Namespace && c.Name == actual.Ref.Name {
				f.CronWorkflows[i].Suspend = req.Action == core.ActionSuspend
			}
		}
	}
	result.Outcome = core.ActionAccepted
	return result, nil
}
