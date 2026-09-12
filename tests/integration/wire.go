//go:build integration

package integration

// WireClient is a test-only HTTP client for fixture-server and root-model tests.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"argo-tui/internal/core"
)

// WireClientConfig configures the test wire client.
type WireClientConfig struct {
	// BaseURL is the full server base (scheme://host[/prefix]).
	BaseURL string
	// Token sent as "Authorization: Bearer <token>"; empty = no header.
	Token string
	// HTTPClient overrides the transport (TLS tests, redirect blocking).
	HTTPClient *http.Client
}

// WireClient is a minimal Reader over the pinned v4.1.2 REST surface.
type WireClient struct {
	cfg WireClientConfig
}

// NewWireClient builds a client.
func NewWireClient(cfg WireClientConfig) *WireClient {
	return &WireClient{cfg: cfg}
}

// compile-time proof the test client satisfies the frozen contract.
var _ core.Reader = (*WireClient)(nil)

// List implements core.Reader over the pinned list endpoint (§4).
func (c *WireClient) List(ctx context.Context, q core.Query) (core.Page, error) {
	u := c.url("/api/v1/workflows/" + url.PathEscape(q.Namespace))
	qv := url.Values{}
	if q.Limit > 0 {
		qv.Set("listOptions.limit", strconv.FormatInt(q.Limit, 10))
	}
	if q.Continue != "" {
		qv.Set("listOptions.continue", q.Continue) // verbatim pass-back
	}
	if q.LabelSelector != "" {
		qv.Set("listOptions.labelSelector", q.LabelSelector)
	}
	u += "?" + qv.Encode()
	body, status, err := c.get(ctx, u)
	if err != nil {
		return core.Page{}, err
	}
	if status != http.StatusOK {
		return core.Page{}, unaryError(body, status)
	}
	var resp struct {
		Metadata struct {
			Continue        string `json:"continue"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Items []WireWorkflow `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return core.Page{}, core.ErrProtocalf("list: malformed JSON response: %v", err)
	}
	page := core.Page{
		Items:           make([]core.Summary, 0, len(resp.Items)),
		Continue:        resp.Metadata.Continue,
		ResourceVersion: resp.Metadata.ResourceVersion,
	}
	for _, it := range resp.Items {
		page.Items = append(page.Items, summarize(it))
	}
	return page, nil
}

// Get implements core.Reader over the pinned detail endpoint (§5), always
// passing the UID (argo-tui policy: UID on every detail GET).
func (c *WireClient) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	u := c.url("/api/v1/workflows/" + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.Name))
	if ref.UID != "" {
		u += "?uid=" + url.QueryEscape(ref.UID)
	}
	body, status, err := c.get(ctx, u)
	if err != nil {
		return core.Workflow{}, err
	}
	if status != http.StatusOK {
		return core.Workflow{}, unaryError(body, status)
	}
	var wf WireWorkflow
	if err := json.Unmarshal(body, &wf); err != nil {
		return core.Workflow{}, core.ErrProtocalf("get: malformed JSON response: %v", err)
	}
	out := toWorkflow(wf)
	out.NodesAvailable = wf.Status.Nodes != nil || wf.Status.OffloadNodeStatusVersion == ""
	if wf.Status.Nodes == nil && wf.Status.OffloadNodeStatusVersion != "" {
		out.NodesUnavailableReason = "node status offloaded (offloadNodeStatusVersion=" +
			wf.Status.OffloadNodeStatusVersion + "); not hydrated by server"
	}
	return out, nil
}

// StreamLogs implements core.Reader over the pinned log endpoint (§6).
// Parses both JSON-lines and SSE framings, detects in-band error chunks,
// and honors context cancellation (drip tests).
func (c *WireClient) StreamLogs(ctx context.Context, req core.LogRequest, cb func(core.LogRecord) error) error {
	u := c.url("/api/v1/workflows/" + url.PathEscape(req.Ref.Namespace) + "/" +
		url.PathEscape(req.Ref.Name) + "/log")
	qv := url.Values{}
	if req.PodName != "" {
		qv.Set("podName", req.PodName)
	}
	qv.Set("logOptions.container", req.Container) // always explicit (§6.2)
	if req.Follow {
		qv.Set("logOptions.follow", "true")
	}
	if req.Timestamps {
		qv.Set("logOptions.timestamps", "true")
	}
	if req.TailLines > 0 {
		qv.Set("logOptions.tailLines", strconv.FormatInt(req.TailLines, 10))
	}
	u += "?" + qv.Encode()

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return core.ErrInvalidf("logs: bad request: %v", err)
	}
	c.applyAuth(hreq)
	// v0.1 policy (§6): do NOT send Accept: text/event-stream.
	hresp, err := c.httpClient().Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // distinguishable from network failure (plan §4)
		}
		return core.ErrUnavailablef("logs: transport error: %v", err)
	}
	defer hresp.Body.Close()

	if hresp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(hresp.Body, 1<<20))
		return unaryError(body, hresp.StatusCode)
	}

	return parseLogStream(ctx, hresp.Body, req, cb)
}

// parseLogStream reads a framed log stream, tolerating chunk boundaries
// mid-line (docs/development.md cases 1–3) and detecting in-band error
// envelopes. A final chunk without a trailing newline is parsed only if it
// is complete JSON; a truncated stream surfaces context cancellation first
// (distinguishable from failure) and otherwise a protocol error — never a
// silent clean pass.
func parseLogStream(ctx context.Context, r io.Reader, req core.LogRequest, cb func(core.LogRecord) error) error {
	br := bufio.NewReaderSize(r, 64*1024) // 64 KiB buffer; NOT bufio.Scanner's default token size
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := br.ReadString('\n')
		if err == nil {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				continue // SSE event separator (or stray blank line)
			}
			payload := strings.TrimPrefix(line, "data: ") // SSE variant (§6)
			if payload == line && strings.HasPrefix(line, "data:") {
				payload = strings.TrimPrefix(line, "data:") // tolerate no-space form
			}
			if payload == "" {
				continue
			}
			if derr := deliverLogChunk(ctx, payload, req, cb); derr != nil {
				return derr
			}
			continue
		}
		if err == io.EOF {
			// Final chunk without trailing newline (§6.3 case 3): parse
			// only if complete JSON; a mid-JSON cut is a severed stream —
			// report cancellation first, protocol error otherwise.
			if line != "" {
				line = strings.TrimRight(line, "\r")
				if cerr := ctx.Err(); cerr != nil {
					return cerr
				}
				if derr := deliverLogChunk(ctx, line, req, cb); derr != nil {
					return derr
				}
			}
			return nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return core.ErrUnavailablef("log stream: read: %v", err)
	}
}

// deliverLogChunk parses one envelope chunk and dispatches its record or
// in-band error.
func deliverLogChunk(ctx context.Context, chunk string, req core.LogRequest, cb func(core.LogRecord) error) error {
	var envelope struct {
		Result *WireLogEntry `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(chunk), &envelope); err != nil {
		return core.ErrProtocalf("log stream: malformed chunk %q: %v", truncateForMsg(chunk), err)
	}
	switch {
	case envelope.Error != nil:
		kind := inBandKind(envelope.Error.Code)
		return core.NewAPIError(kind, 0, "in-band stream error: "+envelope.Error.Message)
	case envelope.Result != nil:
		rec := core.LogRecord{
			PodName:    envelope.Result.PodName,
			Container:  req.Container, // container context from the request (§6.2)
			Content:    envelope.Result.Content,
			ReceivedAt: time.Now(),
		}
		if err := cb(rec); err != nil {
			return err
		}
	}
	return nil
}

// inBandKind maps a gRPC code to the frozen ErrorKind table (§10).
func inBandKind(code int) core.ErrorKind {
	switch code {
	case grpcUnauth:
		return core.ErrUnauthenticated
	case grpcPermDenied:
		return core.ErrForbidden
	case grpcNotFound:
		return core.ErrNotFound
	case grpcUnavailable:
		return core.ErrUnavailable
	case grpcUnimplemented:
		return core.ErrUnsupported
	case grpcInvalid:
		return core.ErrInvalid
	default:
		return core.ErrProtocol
	}
}

// get performs one authenticated GET, returning the body and status.
func (c *WireClient) get(ctx context.Context, u string) ([]byte, int, error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, core.ErrInvalidf("bad request: %v", err)
	}
	c.applyAuth(hreq)
	resp, err := c.httpClient().Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, core.ErrUnavailablef("transport error: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, core.ErrUnavailablef("read body: %v", err)
	}
	return body, resp.StatusCode, nil
}

func (c *WireClient) url(path string) string {
	base := strings.TrimSuffix(c.cfg.BaseURL, "/")
	return base + path
}

func (c *WireClient) httpClient() *http.Client {
	if c.cfg.HTTPClient != nil {
		return c.cfg.HTTPClient
	}
	return http.DefaultClient
}

func (c *WireClient) applyAuth(r *http.Request) {
	if c.cfg.Token != "" {
		r.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
}

// unaryError converts a gateway {"code","message"} body to an APIError with
// the mapped kind (§10).
func unaryError(body []byte, status int) error {
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		kind := core.KindOf(status)
		return core.NewAPIError(kind, status,
			fmt.Sprintf("HTTP %d: non-JSON error body (%s)", status, core.KindOf(status)))
	}
	kind := core.KindOf(status)
	return core.NewAPIError(kind, status, env.Message)
}

// summarize projects a WireWorkflow to the list-level Summary DTO.
func summarize(wf WireWorkflow) core.Summary {
	s := core.Summary{
		Ref: core.Ref{
			Namespace: wf.Metadata.Namespace,
			Name:      wf.Metadata.Name,
			UID:       wf.Metadata.UID,
		},
		ResourceVersion: wf.Metadata.ResourceVersion,
		Phase:           wf.Status.Phase,
		Message:         wf.Status.Message,
		Labels:          wf.Metadata.Labels,
	}
	if t := parseRFC3339(wf.Metadata.CreationTimestamp); t != nil {
		s.CreatedAt = *t
	}
	if t := parseRFC3339(wf.Status.StartedAt); t != nil {
		s.StartedAt = t
	}
	if t := parseRFC3339(wf.Status.FinishedAt); t != nil {
		s.FinishedAt = t
	}
	return s
}

// toWorkflow converts a WireWorkflow detail to the core.Workflow DTO.
func toWorkflow(wf WireWorkflow) core.Workflow {
	out := core.Workflow{Summary: summarize(wf), Resource: mustJSON(wf)}
	if wf.Status.Nodes != nil {
		out.Nodes = make(map[string]core.Node, len(wf.Status.Nodes))
		for id, n := range wf.Status.Nodes {
			node := core.Node{
				ID:            n.ID,
				Name:          n.Name,
				DisplayName:   n.DisplayName,
				Type:          n.Type,
				Phase:         n.Phase,
				Message:       n.Message,
				BoundaryID:    n.BoundaryID,
				Children:      n.Children,
				OutboundNodes: n.OutboundNodes,
				PodName:       n.PodName,
			}
			if t := parseRFC3339(n.StartedAt); t != nil {
				node.StartedAt = t
			}
			if t := parseRFC3339(n.FinishedAt); t != nil {
				node.FinishedAt = t
			}
			out.Nodes[id] = node
		}
	}
	return out
}

func parseRFC3339(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func truncateForMsg(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
