package argo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ficaa1/micko/internal/core"
)

var _ core.ArchiveReader = (*Client)(nil)

// archivePath is the workflow archive, GET /api/v1/archived-workflows, and
// its detail route /api/v1/archived-workflows/{uid}.
//
// The list is narrowed to a namespace with the field selector
// metadata.namespace=<ns>. Servers from v3.5 also take a `namespace` query
// parameter, but every version reads the field selector, and a server that
// gets both checks that they agree, so the selector alone is the portable
// form. Its continuation token is an offset into the archive's query, passed
// back verbatim like any other.
const archivePath = "/api/v1/archived-workflows"

// ListArchivedWorkflows implements core.ArchiveReader.
//
// A server with no archive configured answers the list with an empty page:
// its null archive lists nothing and says nothing more, so that case cannot
// be told apart from an empty archive here. A server without the route at
// all (404, 501) is reported as an archive that is not enabled.
func (c *Client) ListArchivedWorkflows(ctx context.Context, q core.ArchiveQuery) (core.ArchivePage, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	if q.Namespace == "" {
		if err := c.checkClusterScope(ctx); err != nil {
			return core.ArchivePage{}, err
		}
	}
	query := url.Values{}
	if q.Namespace != "" {
		query.Set("listOptions.fieldSelector", "metadata.namespace="+q.Namespace)
	}
	if q.LabelSelector != "" {
		query.Set("listOptions.labelSelector", q.LabelSelector)
	}
	if q.Limit > 0 {
		query.Set("listOptions.limit", fmt.Sprintf("%d", q.Limit))
	}
	if q.Continue != "" {
		query.Set("listOptions.continue", q.Continue)
	}
	body, err := c.getBody(ctx, archivePath, query)
	if err != nil {
		return core.ArchivePage{}, archiveError(err, true)
	}
	var env struct {
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.ArchivePage{}, core.WrapAPIError(core.ErrProtocol, 0, "archived workflow list: unparseable response (protocol mismatch)", err)
	}
	page := core.ArchivePage{Continue: env.Metadata.Continue}
	for _, raw := range env.Items {
		wf, err := wfFromRaw(raw, raw)
		if err != nil {
			return core.ArchivePage{}, core.WrapAPIError(core.ErrProtocol, 0, "archived workflow list: unparseable item", err)
		}
		page.Items = append(page.Items, wf)
	}
	return page, nil
}

// GetArchivedWorkflow implements core.ArchiveReader. The answer is a whole
// Workflow, decoded like a live one. A server with no archive answers with
// an internal error, which is reported as the archive not being enabled.
func (c *Client) GetArchivedWorkflow(ctx context.Context, uid string) (core.Workflow, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	path := archivePath + "/" + url.PathEscape(uid)
	body, err := c.getBody(ctx, path, nil)
	if err != nil {
		return core.Workflow{}, archiveError(err, false)
	}
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		return core.Workflow{}, core.WrapAPIError(core.ErrProtocol, 0, "archived workflow: unparseable response", err)
	}
	if uid != "" && wf.Summary.Ref.UID != uid {
		return core.Workflow{}, core.ErrConflictf("archived workflow: response UID %q does not match the requested %q", wf.Summary.Ref.UID, uid)
	}
	return wf, nil
}

// archiveError turns the answers of a server without an archive into one
// statement. Such a server's null archive fails the detail route with
// "getting archived workflows not supported" as an internal error, and an
// older server or a gateway without the route answers 501, or 404 on the
// list. A 404 on the detail route is the archive's own "not found" for a UID
// it does not hold, and passes through as that; so does every other error,
// a 403 included.
func archiveError(err error, listing bool) error {
	ae := core.AsAPIError(err)
	if ae == nil {
		return err
	}
	msg := strings.ToLower(ae.Message)
	disabled := ae.Status == http.StatusNotImplemented ||
		(listing && ae.Status == http.StatusNotFound) ||
		(strings.Contains(msg, "not supported") && strings.Contains(msg, "archived")) ||
		strings.Contains(msg, "archive is not enabled")
	if !disabled {
		return err
	}
	return &core.APIError{
		Kind:    core.ErrUnsupported,
		Status:  ae.Status,
		Message: core.ArchiveDisabledMessage + " (the server said: " + ae.Message + ")",
		Err:     err,
	}
}
