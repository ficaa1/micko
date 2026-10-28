package argo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/ficaa1/micko/internal/core"
)

var _ core.ResourceReader = (*Client)(nil)
var _ core.ResourceActioner = (*Client)(nil)

func resourcePath(kind core.ResourceKind, ref core.Ref) (string, error) {
	switch kind {
	case core.KindWorkflowTemplate:
		return templatePathPrefix + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.Name), nil
	case core.KindClusterWorkflowTemplate:
		return clusterTemplatePath + "/" + url.PathEscape(ref.Name), nil
	case core.KindCronWorkflow:
		return cronPathPrefix + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.Name), nil
	default:
		return "", errors.New("unsupported resource kind")
	}
}

func (c *Client) GetResource(ctx context.Context, kind core.ResourceKind, ref core.Ref) (core.ResourceState, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	path, err := resourcePath(kind, ref)
	if err != nil {
		return core.ResourceState{}, err
	}
	body, err := c.getBody(ctx, endpointAction, path, nil)
	if err != nil {
		return core.ResourceState{}, err
	}
	return decodeResourceState(kind, body)
}

func decodeResourceState(kind core.ResourceKind, body []byte) (core.ResourceState, error) {
	state := core.ResourceState{Kind: kind}
	if kind == core.KindCronWorkflow {
		cw, err := decodeCronWorkflow(body)
		if err != nil {
			return state, err
		}
		state.Ref = core.Ref{Namespace: cw.Namespace, Name: cw.Name, UID: cw.UID}
		state.Arguments = cw.Arguments
		state.Suspended = cw.Suspend
	} else {
		tmpl, err := decodeTemplate(body)
		if err != nil {
			return state, err
		}
		state.Ref = core.Ref{Namespace: tmpl.Namespace, Name: tmpl.Name, UID: tmpl.UID}
		state.Arguments = tmpl.Arguments
	}
	return state, nil
}

func (c *Client) ExecuteResourceAction(ctx context.Context, req core.ResourceActionRequest) (core.ActionResult, error) {
	result := core.ActionResult{Action: req.Action, Target: req.Resource.Ref, Outcome: core.ActionRefused}
	if err := req.Validate(); err != nil {
		return result, err
	}
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	method := http.MethodPut
	path, err := resourcePath(req.Resource.Kind, req.Resource.Ref)
	if err != nil {
		return result, err
	}
	var payload any = struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}{req.Resource.Ref.Name, req.Resource.Ref.Namespace}
	if req.Action == core.ActionSubmit {
		method = http.MethodPost
		path = "/api/v1/workflows/" + url.PathEscape(req.Namespace) + "/submit"
		payload = struct {
			Namespace     string            `json:"namespace"`
			ResourceKind  core.ResourceKind `json:"resourceKind"`
			ResourceName  string            `json:"resourceName"`
			SubmitOptions struct {
				Parameters []string `json:"parameters,omitempty"`
			} `json:"submitOptions"`
		}{Namespace: req.Namespace, ResourceKind: req.Resource.Kind, ResourceName: req.Resource.Ref.Name, SubmitOptions: struct {
			Parameters []string `json:"parameters,omitempty"`
		}{req.Parameters}}
	} else {
		path += "/" + string(req.Action)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	httpReq, err := c.newRequest(ctx, path, nil)
	if err != nil {
		return result, errors.New("resource action refused: cannot prepare authenticated request")
	}
	httpReq.Method = method
	httpReq.Body = io.NopCloser(bytes.NewReader(body))
	httpReq.ContentLength = int64(len(body))
	httpReq.Header.Set("Content-Type", "application/json")
	result.Outcome = core.ActionUnknown
	resp, err := c.do(ctx, httpReq, endpointAction)
	if err != nil {
		return result, errors.New("resource action outcome unknown; inspect before retrying")
	}
	defer drainAndClose(resp.Body)
	response, readErr := readAllBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, errors.New("resource action was sent but not confirmed; inspect before retrying")
	}
	if readErr != nil {
		return result, errors.New("resource action response incomplete; inspect before retrying")
	}
	if req.Action == core.ActionSubmit {
		wf, err := decodeWorkflowDetail(response)
		if err != nil || wf.Summary.Ref.Namespace != req.Namespace || wf.Summary.Ref.UID == "" || wf.Summary.Ref.Name == "" {
			return result, errors.New("submission response has no valid workflow identity; inspect before retrying")
		}
		result.Workflow = &wf
		ref := wf.Summary.Ref
		result.Affected = &ref
	} else {
		state, err := decodeResourceState(core.KindCronWorkflow, response)
		if err != nil || state.Ref != req.Resource.Ref {
			return result, errors.New("cron response identity is unavailable; inspect before retrying")
		}
	}
	result.Outcome = core.ActionAccepted
	return result, nil
}
