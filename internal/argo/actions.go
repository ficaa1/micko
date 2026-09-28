package argo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ficaa1/micko/internal/core"
)

var _ core.Actioner = (*Client)(nil)

// actionEndpoint is the method, path and body of one action, as Argo's
// OpenAPI spec defines it:
//
//   - resume, suspend, retry, resubmit, stop, terminate: PUT
//     /api/v1/workflows/{namespace}/{name}/{action} with a JSON body naming
//     the workflow; the answer is the workflow.
//   - delete: DELETE /api/v1/workflows/{namespace}/{name} with no body; the
//     answer is an empty object.
func actionEndpoint(req core.ActionRequest) (method, path string, body []byte, err error) {
	path = "/api/v1/workflows/" + url.PathEscape(req.Ref.Namespace) + "/" + url.PathEscape(req.Ref.Name)
	if req.Action == core.ActionDelete {
		return http.MethodDelete, path, nil, nil
	}
	body, err = json.Marshal(req.WireBody())
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal %s action: %w", req.Action, err)
	}
	return http.MethodPut, path + "/" + string(req.Action), body, nil
}

// Execute sends exactly one action request. It deliberately has no retry
// path: a transport failure after the server receives a write is ambiguous.
func (c *Client) Execute(ctx context.Context, req core.ActionRequest) (core.ActionResult, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	result := core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}
	if err := req.CheckConfirmation(); err != nil {
		return result, err
	}
	method, path, body, err := actionEndpoint(req)
	if err != nil {
		return result, err
	}
	base, err := c.baseURL()
	if err != nil {
		// Nothing was sent, so the outcome is not unknown: the request was
		// refused before it left this process.
		return result, err
	}
	target := *base
	target.Path = stringsTrimSlash(base.Path) + path
	target.RawQuery = ""
	token, err := c.tokenFn()
	if err != nil {
		return result, core.ErrUnauthenticatedf("reading credentials from %s: %s", c.describeTokenSource(), sanitizeLine(err.Error()))
	}
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), reqBody)
	if err != nil {
		return result, core.ErrProtocalf("building action request: %v", err)
	}
	setAuthorization(httpReq, token)
	httpReq.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	// The same helper every read goes through: it rejects a redirect with a
	// clear message instead of a raw transport error, and names a TLS
	// failure as one.
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, core.WrapAPIError(core.ErrUnavailable, 0, "action outcome unknown: connection failed after request was sent; inspect before retrying", err)
	}
	defer drainAndClose(resp.Body)
	responseBody, readErr := readAllBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Outcome = ""
		return result, mapHTTPError(resp, method, path, responseBody, nil, c.now)
	}
	if req.Action == core.ActionDelete {
		// A 2xx to a DELETE is the whole answer: the body is an empty
		// object with nothing to decode, and a body that could not be read
		// takes nothing away from the status line. There is no workflow
		// left to name, so Affected stays nil.
		result.Outcome = core.ActionAccepted
		if readErr == nil {
			result.Response = append(json.RawMessage(nil), responseBody...)
		}
		return result, nil
	}
	if readErr != nil {
		// The request was sent and the server answered 2xx, so the mutation
		// may well have been applied; only the answer was lost.
		return result, core.WrapAPIError(core.ErrUnavailable, 0, "action outcome unknown: the server's answer could not be read; inspect before retrying", readErr)
	}
	if len(responseBody) == 0 {
		return result, core.WrapAPIError(core.ErrProtocol, 0, "action outcome unknown: server returned an empty response; inspect before retrying", io.ErrUnexpectedEOF)
	}
	wf, err := decodeWorkflowDetail(responseBody)
	if err != nil {
		return result, core.WrapAPIError(core.ErrProtocol, 0, "action outcome unknown: server returned an invalid workflow response; inspect before retrying", err)
	}
	// The server returned success, so the mutation is applied. Confirmed
	// means more than that: it means the expected end state was observed.
	// The transport observes nothing, so the caller's read-back promotes
	// this.
	result.Outcome = core.ActionAccepted
	result.Workflow = &wf
	result.Response = append(json.RawMessage(nil), responseBody...)
	ref := wf.Summary.Ref
	if req.Action == core.ActionResubmit || ref != (core.Ref{}) {
		result.Affected = &ref
	}
	return result, nil
}

func stringsTrimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
