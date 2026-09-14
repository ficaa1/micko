package argo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"argo-tui/internal/core"
)

var _ core.Actioner = (*Client)(nil)

// Execute sends exactly one action request. It deliberately has no retry
// path: a transport failure after the server receives a PUT is ambiguous.
func (c *Client) Execute(ctx context.Context, req core.ActionRequest) (core.ActionResult, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	result := core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}
	if req.Action != core.ActionResume && req.Action != core.ActionRetry && req.Action != core.ActionResubmit && req.Action != core.ActionStop && req.Action != core.ActionTerminate {
		return result, fmt.Errorf("unsupported action %q", req.Action)
	}
	if !req.Confirmation.Confirmed {
		return result, core.ErrActionNotConfirmed
	}
	if req.Action == core.ActionTerminate && req.Confirmation.TypedName != req.Ref.Name {
		return result, core.ErrConfirmationNameMismatch
	}
	body, err := json.Marshal(req.WireBody())
	if err != nil {
		return result, fmt.Errorf("marshal %s action: %w", req.Action, err)
	}
	path := "/api/v1/workflows/" + url.PathEscape(req.Ref.Namespace) + "/" + url.PathEscape(req.Ref.Name) + "/" + string(req.Action)
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), bytes.NewReader(body))
	if err != nil {
		return result, core.ErrProtocalf("building action request: %v", err)
	}
	setAuthorization(httpReq, token)
	httpReq.Header.Set("User-Agent", c.userAgent)
	httpReq.Header.Set("Content-Type", "application/json")
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
		return result, mapHTTPError(resp, http.MethodPut, path, responseBody, nil, c.now)
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
	result.Outcome = core.ActionConfirmed
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
