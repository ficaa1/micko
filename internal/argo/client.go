// Package argo implements the core.Reader transport adapter against Argo
// Server's REST API, pinned to the v4.1.2 wire facts in docs/protocol.md.
//
// Scope and safety rules (plan §8 A1; ADR 0001):
//   - Standard-library HTTP/JSON only; no official client, no Kubernetes
//     client, no go.mod additions (F owns module files).
//   - Read-only surface: List/Get/StreamLogs. No write methods exist, so no
//     mutation can ever be retried by this package (plan §6).
//   - Tokens never appear in URLs, error messages or logs; the Authorization
//     header is rebuilt per request from the injected credential source to
//     support rotation (plan §3).
//   - Redirects are rejected (credentials must never reach another origin),
//     TLS verification is on by default with opt-in custom CA.
package argo

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"argo-tui/internal/core"
)

// Client is the production core.Reader. It is safe for concurrent use.
type Client struct {
	// base is the full server base URL (scheme://host[/prefix]); endpoint
	// paths are appended verbatim (docs/protocol.md §3 base path rule).
	base *url.URL
	// tokenFn reloads credential material per request (rotation support,
	// plan §3). Never stored, never logged.
	tokenFn func() (string, error)
	// tokenSource describes where credentials come from ("env var FOO",
	// "token file /path") for sanitized error guidance — names only,
	// never values (plan §3).
	tokenSource string
	// caFile records the CA bundle path for error guidance only.
	caFile string
	// insecureSkipTLSVerify mirrors the explicit user opt-in.
	insecureSkipTLSVerify bool
	// resolveBase, when set, returns the currently valid base URL. A managed
	// port-forward re-binds an ephemeral loopback port on every recovery, so
	// a base captured once at startup goes stale. Resolving per request keeps
	// the transport pointed at the owned endpoint across reconnects while the
	// configured scheme, path prefix and TLS material stay fixed.
	resolveBase func() string
	// http performs the requests; redirects are rejected at this layer.
	http *http.Client
	// clock enables deterministic Retry-After parsing in tests.
	now func() time.Time
	// maxRetries stays 0 forever: this package performs no automatic
	// retries; the app layer owns bounded backoff policy (plan §5/§6).
	maxRetries int
}

// assert the frozen read contract is satisfied at compile time (A1 gate).
var _ core.Reader = (*Client)(nil)

// Options configures the client. All credential material flows through a
// callback; no token field exists on this struct.
type Options struct {
	Server  string // full base URL including any path prefix
	TokenFn func() (string, error)
	// TokenSource is the sanitized description used in error messages
	// ("token file /path", "env var ARGO_TOKEN"). Empty means unknown.
	TokenSource string
	CAFile      string
	// InsecureSkipTLSVerify must be set only from an explicit user opt-in
	// (plan §3); when true, verification is disabled and a permanent UI
	// warning is the caller's obligation.
	InsecureSkipTLSVerify bool
	// Now overrides time.Now for deterministic Retry-After parsing.
	Now func() time.Time
	// DialTimeout bounds opening the connection (default 10s). Streaming
	// reads are NOT bounded by it.
	DialTimeout time.Duration
	// ResolveServer optionally supplies the live base URL per request. It
	// must return an endpoint equivalent to Server (same scheme and path
	// prefix); only the host and port may change, as they do when a managed
	// port-forward recovers onto a new ephemeral local port. An empty or
	// unparseable return keeps the last known good base.
	ResolveServer func() string
}

// NewClient validates the URL contract and builds the transport.
func NewClient(opts Options) (*Client, error) {
	if opts.Server == "" {
		return nil, fmt.Errorf("argo client: server endpoint missing")
	}
	u, err := url.Parse(opts.Server)
	if err != nil {
		return nil, fmt.Errorf("argo client: parse server URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("argo client: server URL scheme %q rejected: only http/https allowed", u.Scheme)
	}
	if u.User != nil {
		return nil, fmt.Errorf("argo client: server URL userinfo rejected")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("argo client: server URL host missing")
	}
	for k := range u.Query() {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") || strings.Contains(lk, "password") ||
			strings.Contains(lk, "secret") || strings.Contains(lk, "credential") ||
			lk == "api_key" || lk == "apikey" || lk == "authorization" {
			return nil, fmt.Errorf("argo client: server URL query parameter %q looks like a credential and is rejected", k)
		}
	}
	// Plain HTTP to non-loopback is rejected here too (loopback stays
	// allowed for dev/demo; the plan §3 contract mirrors internal/config).
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return nil, fmt.Errorf("argo client: plain HTTP to non-loopback host %q rejected (use https; loopback http is allowed for dev/demo)", u.Hostname())
	}

	if opts.TokenFn == nil {
		return nil, fmt.Errorf("argo client: credential source (TokenFn) required")
	}

	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: dialTimeout,
		// Streaming responses must not be buffered through HTTP/2; force
		// HTTP/1.1 like the official client (ADR 0001 obligation: HTTP/1.1
		// semantics end-to-end).
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: opts.InsecureSkipTLSVerify, MinVersion: tls.VersionTLS12}, //nolint:gosec // explicit user opt-in only
	}
	if !opts.InsecureSkipTLSVerify && opts.CAFile != "" {
		pemData, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("argo client: read caFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("argo client: caFile %q contains no PEM certificates", opts.CAFile)
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	// Redirects are rejected via CheckRedirect returning an error; any
	// credentials attached to the outstanding request are therefore never
	// re-sent to the redirect target (plan §3; CONN-05).
	hc := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errRedirected{code: httpRespCodeFromRedirect(req.Response)}
		},
		// No overall timeout: StreamLogs must stay alive indefinitely;
		// cancellation is carried by the request context instead.
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		base:                  u,
		tokenFn:               opts.TokenFn,
		tokenSource:           opts.TokenSource,
		caFile:                opts.CAFile,
		insecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		http:                  hc,
		now:                   now,
		resolveBase:           opts.ResolveServer,
	}, nil
}

// errRedirected marks a rejected redirect hop.
type errRedirected struct{ code int }

func (e errRedirected) Error() string {
	if e.code == 0 {
		return "server redirected the request"
	}
	return fmt.Sprintf("server redirected the request (HTTP %d)", e.code)
}

func httpRespCodeFromRedirect(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]" ||
		(len(host) > 8 && strings.HasPrefix(host, "127."))
}

// --- request plumbing -----------------------------------------------------------

// baseURL returns the base URL to use for the next request. It preserves the
// configured scheme, path prefix and userinfo-free contract: only host and
// port are adopted from the resolver, and only when they parse and pass the
// same plain-HTTP loopback rule enforced at construction.
func (c *Client) baseURL() *url.URL {
	if c.resolveBase == nil {
		return c.base
	}
	raw := c.resolveBase()
	if raw == "" {
		return c.base
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return c.base
	}
	next := *c.base
	next.Host = u.Host
	if next.Scheme == "http" && !isLoopback(next.Hostname()) {
		return c.base
	}
	return &next
}

// setAuthorization attaches the bearer credential. An empty token means the
// deployment authenticates by another means (for example an Argo server run
// with --auth-mode=server), so no header is sent at all: an empty "Bearer "
// value is malformed and some gateways reject it outright.
func setAuthorization(req *http.Request, token string) {
	if token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

// newRequest builds an authenticated GET to the appended path.
func (c *Client) newRequest(ctx context.Context, path string, query url.Values) (*http.Request, error) {
	base := c.baseURL()
	target := *base
	target.Path = strings.TrimSuffix(base.Path, "/") + path
	target.RawQuery = query.Encode()
	token, err := c.tokenFn()
	if err != nil {
		// The error text comes from the credential source itself; sanitize
		// and re-raise as authenticaton-preparation failure. Never embed
		// the value — the source callback must not leak it either, but we
		// sanitize defensively.
		return nil, core.ErrUnauthenticatedf("reading credentials from %s: %s", c.describeTokenSource(), sanitizeLine(err.Error()))
	}
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, core.ErrProtocalf("building request: %v", err)
	}
	setAuthorization(req, token)
	req.Header.Set("User-Agent", "argo-tui/0.1")
	// Deliberately NO Accept header at all: no SSE hint (docs/protocol.md
	// §6 v0.1 policy) and no content-negotiation surprises. The gateway
	// marshaler ignores Accept.
	return req, nil
}

func (c *Client) describeTokenSource() string {
	if c.tokenSource != "" {
		return c.tokenSource
	}
	return "configured credential source"
}

// do executes the request with redirect rejection applied at transport
// level. The response is returned with Body open; caller closes it.
func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)
	resp, err := c.http.Do(req)
	if err != nil {
		var rerr errRedirected
		if errors.As(err, &rerr) {
			return nil, core.ErrProtocalf(
				"server attempted a redirect; refusing (credentials must never be sent to another origin); explain the response to your administrator (no credentials leaked)")
		}
		// Distinguish TLS failures from other transport errors for guidance
		// (CONN-03: name the problem class, not a raw dump).
		msg := err.Error()
		if isTLSFailure(msg) {
			return nil, core.ErrUnavailablef("TLS handshake with server failed: %s (check server certificate or configured caFile %s)", sanitizeLine(msg), c.caFileOr("none"))
		}
		return nil, core.ErrUnavailablef("server unreachable: %s", sanitizeLine(msg))
	}
	// No redirect hop may ever carry our credentials (plan §3; CONN-05).
	// A 3xx served as a *final* answer without a redirect hop (unusual
	// gateway/proxy behavior) is still readable — but its Location, if
	// present, must never be followed by us.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.Header.Get("Location") != "" {
		drainAndClose(resp.Body)
		return nil, core.ErrProtocalf(
			"server responded HTTP %d with a redirect target; refusing to follow (credentials must never be sent to another origin); explain the response to your administrator", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) caFileOr(def string) string {
	if c.caFile != "" {
		return c.caFile
	}
	if c.insecureSkipTLSVerify {
		return "(insecure-skip-tls-verify is ON)"
	}
	return def
}

func isTLSFailure(msg string) bool {
	l := strings.ToLower(msg)
	for _, p := range []string{"tls:", "x509", "certificate", "handshake"} {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// --- error mapping --------------------------------------------------------------

// errorEnvelope mirrors the gateway unary error shape
// ({"code":N,"message":"..."} — docs/protocol.md §5; "error"/"details"
// tolerated).
type errorEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Error   string `json:"error"`
	Details any    `json:"details"`
}

// grpcHTTPStatus reconstructs the HTTP status the gateway would map a gRPC
// code to (round-trip table pinned in docs/protocol.md §9). Used for
// in-band stream errors where no HTTP status applies.
func grpcHTTPStatus(code int) int {
	// Subset covering the codes the streams may emit (gateway v1.16.0
	// runtime.HTTPStatusFromCode).
	switch code {
	case 3: // InvalidArgument
		return 400
	case 5: // NotFound
		return 404
	case 6: // AlreadyExists
		return 409
	case 7: // PermissionDenied
		return 403
	case 8: // ResourceExhausted
		return 429
	case 12: // Unimplemented
		return 501
	case 14: // Unavailable
		return 503
	case 16: // Unauthenticated
		return 401
	case 1: // Canceled
		return 408
	case 4: // DeadlineExceeded
		return 504
	case 13: // Internal
		return 500
	default:
		return 500
	}
}

// classifyInBand maps a gRPC code from an in-band error envelope to a kind
// and the HTTP status the gateway would have used (kept for message
// context only; APIError.Status stays 0 per the frozen contract).
func classifyInBand(code int) (core.ErrorKind, int) {
	st := grpcHTTPStatus(code)
	return core.KindOf(st), st
}

// mapHTTPError builds a typed APIError from a non-200 response. The server
// body's message passes through (it may name the workflow/namespace — fine,
// docs/protocol.md §9); no credential material is ever added by us, and
// Retry-After is typed for 429s (plan slice 5; LIST-08). `now` injects the
// clock for HTTP-date Retry-After parsing (nil ⇒ time.Now).
func mapHTTPError(resp *http.Response, method, path string, body []byte, rawErr error, now func() time.Time) *core.APIError {
	if now == nil {
		now = time.Now
	}
	status := resp.StatusCode
	// Login page / reverse proxy interception (plan §3, CONN-14) can
	// appear at any status (200 through 503 from SSO gateways). HTML
	// bodies get unauthenticated-style guidance with the content type
	// named, never retry-looped.
	if isHTMLBody(body, resp) {
		return core.NewAPIError(core.ErrUnauthenticated, http.StatusUnauthorized,
			"server returned an HTML page instead of API data (content-type "+
				sanitizeLine(headerGet(resp, "Content-Type"))+
				"); this endpoint expects interactive browser login — configure a server/service-account token; do not retry automatically")
	}
	env := errorEnvelope{}
	if isJSONBody(body, resp) {
		_ = json.Unmarshal(body, &env)
	}
	message := env.Message
	if message == "" {
		message = env.Error
	}
	if message == "" {
		message = fmt.Sprintf("%s %s failed", method, path)
	}
	ae := core.NewAPIError(core.KindOf(status), status, redactMessage(message))
	if status == http.StatusTooManyRequests {
		ae.RetryAfter = retryAfterOf(resp, now)
	}
	if rawErr != nil {
		ae.Err = rawErr
	}
	return ae
}

// currentNow was removed: the clock is injected through mapHTTPError's
// signature (Client.now), avoiding a mutable package-level var.

func headerGet(resp *http.Response, key string) string {
	if resp == nil {
		return "" // byte-only probes (stream parser) pass nil resp
	}
	v := resp.Header.Get(key)
	return strings.TrimSpace(v)
}

// isJSONBody decides whether the error body should be JSON-decoded. The
// byte probe wins: real deployments may omit or lie about Content-Type
// (Go's server sniffs a JSON error body as text/plain; an SSO proxy may
// label JSON as text/html).
func isJSONBody(body []byte, resp *http.Response) bool {
	if len(body) == 0 {
		return false
	}
	if looksLikeJSON(body) {
		return true
	}
	ct := strings.ToLower(headerGet(resp, "Content-Type"))
	return ct != "" && strings.Contains(ct, "json")
}

func looksLikeJSON(body []byte) bool {
	i := 0
	for i < len(body) && (body[i] == ' ' || body[i] == '	' || body[i] == '\r' || body[i] == '\n') {
		i++
	}
	return i < len(body) && (body[i] == '{' || body[i] == '[')
}

// isHTMLBody reports whether the body is an HTML login/interception page.
func isHTMLBody(body []byte, resp *http.Response) bool {
	if ct := strings.ToLower(headerGet(resp, "Content-Type")); strings.Contains(ct, "text/html") {
		return true
	}
	if len(body) == 0 {
		return false
	}
	head := body
	if len(head) > 1024 {
		head = head[:1024]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<html") || strings.Contains(s, "<!doctype html") ||
		(strings.Contains(s, "<form") && strings.Contains(s, "login"))
}

// readBody drains up to 64 KiB of an error response for message extraction.
func readBody(r io.Reader) []byte {
	if r == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(r, 64*1024))
	return b
}

// drainAndClose finishes reading (so the connection may be reused) and
// closes the body.
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

// --- Reader.List ----------------------------------------------------------------

func (c *Client) List(ctx context.Context, q core.Query) (core.Page, error) {
	path := "/api/v1/workflows/" + url.PathEscape(q.Namespace)
	query := url.Values{}
	if q.Limit > 0 {
		// v4.1.2 listOptions.limit is string-encoded integer on the wire
		// (docs/protocol.md §4).
		query.Set("listOptions.limit", fmt.Sprintf("%d", q.Limit))
	}
	if q.Continue != "" {
		// Continuation token passed back verbatim, never parsed.
		query.Set("listOptions.continue", q.Continue)
	}
	if q.LabelSelector != "" {
		query.Set("listOptions.labelSelector", q.LabelSelector)
	}
	req, err := c.newRequest(ctx, path, query)
	if err != nil {
		return core.Page{}, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return core.Page{}, err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return core.Page{}, mapHTTPError(resp, http.MethodGet, path, readBody(resp.Body), nil, c.now)
	}
	body := readAllBody(resp.Body)
	if body == nil || len(body) == 0 {
		return core.Page{}, core.ErrProtocalf("list: empty response body")
	}
	// SSO/reverse-proxy interception can answer 200 with a login page
	// (plan §3, CONN-14): surface the guidance instead of a protocol error.
	if isHTMLBody(body, resp) {
		return core.Page{}, mapHTTPError(resp, http.MethodGet, path, body, nil, c.now)
	}
	page, err := decodeListPage(body)
	if err != nil {
		return core.Page{}, core.WrapAPIError(core.ErrProtocol, 0, "list: unparseable response (protocol mismatch; see docs/protocol.md)", err)
	}
	return page, nil
}

// readAllBody reads a bounded response body (list/get are unary and small).
func readAllBody(r io.Reader) []byte {
	b, _ := io.ReadAll(io.LimitReader(r, 16*1024*1024))
	return b
}

// --- Reader.Get -----------------------------------------------------------------

func (c *Client) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	path := "/api/v1/workflows/" + url.PathEscape(ref.Namespace) + "/" + url.PathEscape(ref.Name)
	query := url.Values{}
	if ref.UID != "" {
		// Always pass UID so the server can fall back to the archive for
		// same-name workflows (docs/protocol.md §5; DET-02 prequisite).
		query.Set("uid", ref.UID)
	}
	req, err := c.newRequest(ctx, path, query)
	if err != nil {
		return core.Workflow{}, err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return core.Workflow{}, err
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return core.Workflow{}, mapHTTPError(resp, http.MethodGet, path, readBody(resp.Body), nil, c.now)
	}
	body := readAllBody(resp.Body)
	if len(body) == 0 {
		return core.Workflow{}, core.ErrProtocalf("get %s/%s: empty response body", ref.Namespace, ref.Name)
	}
	// Same 200-with-login-page interception guard as List (CONN-14).
	if isHTMLBody(body, resp) {
		return core.Workflow{}, mapHTTPError(resp, http.MethodGet, path, body, nil, c.now)
	}
	wf, err := decodeWorkflowDetail(body)
	if err != nil {
		return core.Workflow{}, core.WrapAPIError(core.ErrNotFound, http.StatusNotFound,
			fmt.Sprintf("workflow %s/%s not found", ref.Namespace, ref.Name), nil)
	}
	// UID mismatch is same-name replacement detection (DET-02/LIST-12):
	// returned UID differs from the requested one ⇒ typed conflict, never
	// silently swapped.
	if ref.UID != "" && wf.Summary.Ref.UID != ref.UID {
		return core.Workflow{}, core.ErrConflictf(
			"workflow %s/%s: response UID %q does not match selected UID %q (same-name replacement?) — reselect the workflow",
			ref.Namespace, ref.Name, wf.Summary.Ref.UID, ref.UID)
	}
	return wf, nil
}

// --- shared helpers ---------------------------------------------------------------

// sanitizeLine strips control characters from text embedded into error
// messages so server-provided strings cannot inject terminal sequences or
// log-forgery newlines (SEC-01/02 applied to transport surfaces).
func sanitizeLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || (r >= 0x00 && r <= 0x1F) || (r >= 0x7F && r <= 0x9F) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// bufioPeek-based helpers live in logs.go; keep this file focused on the
// transport and request plumbing.

var _ = bufio.NewReader // referenced by logs.go in the same package
