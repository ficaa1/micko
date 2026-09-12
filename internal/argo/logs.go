// logs.go — the log streaming transport (GET /api/v1/workflows/{ns}/{name}/log,
// docs/development.md).
//
// Framing: the parser accepts BOTH candidate framings behind one choke
// point (docs/development.md resolution, gate G-1):
//
//	(a) bare JSON-lines: {"result": {...}}\n   (gateway v1.16.0 source)
//	(b) SSE: "data: {"result": {...}}\n\n"     (client + e2e expectations)
//
// Each line is dispatched to the same record decoder; SSE `data: ` prefixes
// are stripped and keepalive comment lines ignored.
//
// Edge cases covered (docs/development.md): chunk boundaries not aligned
// to records, records coalesced/split per read, final line without trailing
// newline, in-band {"error": …} final chunk, single-record size bounds
// (server caps lines at 1 MiB; client cap 2 MiB), malformed UTF-8 inside
// content (Go JSON decoding replaces with U+FFFD).
package argo

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/shared"
)

// MaxLogRecordBytes bounds one decoded record (LogEntry.content) delivered
// to the consumer. The server pipeline force-flushes single lines at
// 1 MiB (maxTokenLength, docs/development.md); a misbehaving proxy could
// deliver more — anything past this cap is dropped with a visible marker
// instead of growing memory unboundedly (STR-05/LOG-05).
const MaxLogRecordBytes = 2 * 1024 * 1024

// OversizeRecordMarker is the Content delivered instead of an oversized
// record — a visible truncation marker (plan §5).
const OversizeRecordMarker = "[log record dropped: exceeds client size cap]"

// logStreamEnvelope is one stream chunk: either {"result": <LogEntry>} or a
// final {"error": {...}} (docs/development.md).
type logStreamEnvelope struct {
	Result *logEntry   `json:"result"`
	Error  *errorChunk `json:"error"`
}

// logEntry pins LogEntry exactly: content + podName, nothing else
// (docs/development.md — no timestamp, no container field).
type logEntry struct {
	Content string `json:"content"`
	PodName string `json:"podName"`
}

// errorChunk mirrors grpc-gateway v1.16.0 StreamError (field names
// verified in pinned source internal/errors.pb.go) plus the plain
// {"code":N,"message":"..."} shape the gateway emits for in-band stream
// errors (docs/development.md: json.Marshal of map[string]any{"error":
// status.FromContextError(...)} — bare gRPC code). camelCase variants are
// tolerated because deployments may marshal differently.
type errorChunk struct {
	Code        int32  `json:"code"` // bare gRPC code (gateway in-band shape)
	GrpcCode    int32  `json:"grpc_code"`
	GrpcCodeAlt int32  `json:"grpcCode"`
	HttpCode    int32  `json:"http_code"`
	HttpCodeAlt int32  `json:"httpCode"`
	Message     string `json:"message"`
	HttpStatus  string `json:"http_status"`
}

// StreamLogs implements core.Reader. Records are delivered serially on the
// reader goroutine; StreamLogs returns nil for a clean finite EOF, and an
// error wrapping context.Canceled/DeadlineExceeded when cancellation ends
// the stream (distinguishable from network failure — plan §4; STR-01).
func (c *Client) StreamLogs(ctx context.Context, req core.LogRequest, cb func(core.LogRecord) error) error {
	// Workflow-wide or pod-scoped route per request.PodName; empty podName
	// uses the non-deprecated WorkflowLogs route (docs/development.md).
	var path string
	if req.PodName == "" {
		path = "/api/v1/workflows/" + url.PathEscape(req.Ref.Namespace) + "/" +
			url.PathEscape(req.Ref.Name) + "/log"
	} else {
		path = "/api/v1/workflows/" + url.PathEscape(req.Ref.Namespace) + "/" +
			url.PathEscape(req.Ref.Name) + "/" + url.PathEscape(req.PodName) + "/log"
	}
	query := url.Values{}
	query.Set("podName", req.PodName) // always explicit; empty = all pods
	query.Set("logOptions.container", req.Container)
	if req.Follow {
		query.Set("logOptions.follow", "true")
	}
	if req.TailLines > 0 {
		query.Set("logOptions.tailLines", fmt.Sprintf("%d", req.TailLines))
	}
	if req.Timestamps {
		query.Set("logOptions.timestamps", "true")
	}
	httpReq, err := c.newRequest(ctx, path, query)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, httpReq)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		// Stream error before any chunk ⇒ unary-style HTTP error (gateway
		// handler.go: wroteHeader=false path — docs/development.md).
		return mapHTTPError(resp, http.MethodGet, path, readBody(resp.Body), nil, c.now)
	}

	// Headers arrive well before the first payload (server flushes early,
	// docs/development.md keepalive note). SSO/reverse-proxy interception
	// with HTTP 200 (CONN-14) is caught inside the parser: the first
	// unparseable line that looks like HTML surfaces the login-page
	// guidance instead of protocol-error noise.
	return consumeLogStream(ctx, resp.Body, req, headerGet(resp, "Content-Type"), c.now, cb)
}

// consumeLogStream parses the response body chunk-by-chunk, dispatching
// records to cb serially. It is the single framing-normalization choke
// point for gate G-1. contentType is the response's Content-Type header,
// used only to give HTML-interception a precise message.
func consumeLogStream(
	ctx context.Context,
	body io.Reader,
	req core.LogRequest,
	contentType string,
	now func() time.Time,
	cb func(core.LogRecord) error,
) error {
	if now == nil {
		now = time.Now
	}
	reader := bufioReader(body)
	parser := newLogChunkParser()

	for {
		// Abort promptly on cancellation even when the server does not
		// close the pipe (bounded work per call, plan §5).
		if err := ctx.Err(); err != nil {
			// Network failure vs cancellation is distinguishable at every
			// exit path: cancellation wraps the context error (STR-01).
			return fmt.Errorf("log stream: %w", err)
		}
		line, readErr := readStreamLine(reader)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			// Read error (connection reset etc.) — network failure, not
			// cancellation.
			return core.ErrUnavailablef("log stream: read failed: %s", sanitizeLine(readErr.Error()))
		}
		if len(line) > 0 {
			if serr := feedLine(parser, line, req, contentType, now, cb); serr != nil {
				return serr
			}
		}
		if errors.Is(readErr, io.EOF) {
			// Whole-frame parse must be forced at EOF: a final record may
			// lack the trailing newline (docs/development.md). The
			// parser's line buffer holds a partial record; if it is
			// complete JSON it is dispatched now, otherwise it is a
			// protocol error surface.
			if err := parser.flushEOF(req, contentType, now, cb); err != nil {
				return err
			}
			// Clean finite EOF: HTTP body ended normally with no error
			// chunk ⇒ nil (the caller owns reconnect policy; docs §6.3.7:
			// mid-stream close without error chunk stays ambiguous but
			// ends cleanly here).
			return nil
		}
		if parser.byteOverflow {
			// Oversized single line: drop with a visible marker, keep the
			// stream usable (bounded memory, LOG-05/14).
			if err := emitRecord(ctx, req, OversizeRecordMarker, podNameOr(req.PodName, ""), now(), cb); err != nil {
				return err
			}
			parser.resetAfterOversize()
		}
	}
}

// feedLine handles one raw line (already stripped of SSE prefixes at read
// time or here) through the parser.
func feedLine(p *logChunkParser, line []byte, req core.LogRequest, contentType string, now func() time.Time, cb func(core.LogRecord) error) error {
	payload, isSSEData := stripSSE(line)
	if isSSEData && len(payload) == 0 {
		return nil // SSE keepalive comment / empty data line
	}
	if isSSEData {
		// SSE events join trailing space by spec; with single-line events
		// this is a no-op. Empty after strip = separator already consumed.
		if isWhitespace(payload) {
			return nil
		}
		return p.consume(payload, req, contentType, now, cb)
	}
	if isWhitespace(payload) || looksLikeSSEComment(line) {
		return nil
	}
	return p.consume(payload, req, contentType, now, cb)
}

func looksLikeSSEComment(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	return line[0] == ':'
}

func isWhitespace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

// stripSSE detects and strips the "data: " prefix. Returns payload and
// whether the line was an SSE data line.
func stripSSE(line []byte) ([]byte, bool) {
	const prefix = "data: "
	if len(line) >= len(prefix) && string(line[:len(prefix)]) == prefix {
		return line[len(prefix):], true
	}
	if len(line) == 5 && string(line) == "data:" {
		return nil, true
	}
	return line, false
}

func podNameOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// logChunkParser accumulates bytes until a complete line is assembled
// (handles records split across reads and several records per read — the
// bufio reader already split at newlines; this also enforces the per-record
// byte cap).
type logChunkParser struct {
	buf          []byte
	byteOverflow bool
}

func newLogChunkParser() *logChunkParser { return &logChunkParser{} }

// consume processes one complete wire line from the (already bufio-ed)
// body. split lines that needed buffering are assembled here. contentType
// feeds the HTML-interception message (may be empty).
func (p *logChunkParser) consume(line []byte, req core.LogRequest, contentType string, now func() time.Time, cb func(core.LogRecord) error) error {
	// Cap on RAW line bytes, before decoding: one raw JSON byte yields at
	// most one decoded content byte (JSON never expands), so the decoded
	// content cannot exceed MaxLogRecordBytes either. Checking here keeps
	// memory bounded regardless of how the line is split across reads.
	if len(p.buf)+len(line) > MaxLogRecordBytes {
		p.byteOverflow = true
		return nil
	}
	p.buf = append(p.buf, line...)
	env := logStreamEnvelope{}
	// Tolerant decode: json.Unmarshal replaces malformed UTF-8 bytes with
	// U+FFFD (Go semantics accepted per docs/development.md).
	if err := json.Unmarshal(p.buf, &env); err != nil {
		// Not complete JSON. Could be a split frame segment (keep
		// buffering until the next line arrives) — or HTML interception
		// (CONN-14): surface the login-page guidance once.
		if isHTMLBody(p.buf, nil) ||
			(contentType != "" && strings.Contains(strings.ToLower(contentType), "text/html")) {
			return core.NewAPIError(core.ErrUnauthenticated, http.StatusUnauthorized,
				"server returned an HTML page instead of a log stream (content-type "+
					sanitizeLine(contentType)+
					"); this endpoint expects interactive browser login — configure a server/service-account token; do not retry automatically")
		}
		return nil
	}
	p.buf = p.buf[:0]
	switch {
	case env.Error != nil:
		return inBandError(env.Error)
	case env.Result != nil:
		return emitRecord(nil, req, env.Result.Content, env.Result.PodName, now(), cb)
	default:
		// {} or unknown envelope — tolerated silently (gateway keepalive
		// could appear as an empty object in some deployments).
		return nil
	}
}

func (p *logChunkParser) resetAfterOversize() {
	p.buf = p.buf[:0]
	p.byteOverflow = false
}

// flushEnd-of-stream fragments: last chunk without trailing newline.
// Complete JSON ⇒ final in-band error (error envelopes may end the stream
// without a newline when the proxy truncates mid-flush); incomplete JSON ⇒
// dropped with a protocol surface, per docs/development.md
func (p *logChunkParser) flushEOF(req core.LogRequest, contentType string, now func() time.Time, cb func(core.LogRecord) error) error {
	if len(p.buf) == 0 {
		return nil
	}
	line := p.buf
	p.buf = p.buf[:0]
	payload, wasSSE := stripSSE(line)
	if wasSSE || !looksLikeSSEComment(line) {
		if isWhitespace(payload) {
			return nil
		}
		env := logStreamEnvelope{}
		if err := json.Unmarshal(payload, &env); err != nil {
			if isHTMLBody(payload, nil) ||
				(contentType != "" && strings.Contains(strings.ToLower(contentType), "text/html")) {
				return core.NewAPIError(core.ErrUnauthenticated, http.StatusUnauthorized,
					"server returned an HTML page instead of a log stream (content-type "+
						sanitizeLine(contentType)+
						"); this endpoint expects interactive browser login — configure a server/service-account token; do not retry automatically")
			}
			// Parse the partial line only if it is complete JSON;
			// otherwise it is a protocol-error surface (dropped as
			// incomplete, docs/development.md).
			return core.ErrProtocalf("log stream: truncated final record dropped (stream cut mid-frame)")
		}
		switch {
		case env.Error != nil:
			return inBandError(env.Error)
		case env.Result != nil:
			return emitRecord(nil, req, env.Result.Content, env.Result.PodName, now(), cb)
		}
	}
	return nil
}

// inBandError converts a stream error envelope into a typed APIError with no
// HTTP status (none applies — docs/development.md).
func inBandError(ec *errorChunk) error {
	code := int(ec.Code)
	if code == 0 {
		code = int(ec.GrpcCode)
	}
	if code == 0 {
		code = int(ec.GrpcCodeAlt)
	}
	// A gateway StreamError with only http_code set (grpc code absent)
	// still maps through the HTTP status it encodes.
	if code == 0 && (ec.HttpCode != 0 || ec.HttpCodeAlt != 0) {
		hc := int(ec.HttpCode)
		if hc == 0 {
			hc = int(ec.HttpCodeAlt)
		}
		return core.NewAPIError(core.KindOf(hc), 0, shared.RedactTokens(sanitizeLine(ec.Message)))
	}
	kind, _ := classifyInBand(code)
	msg := shared.RedactTokens(sanitizeLine(ec.Message))
	return core.NewAPIError(kind, 0, msg)
}

// emitRecord builds and delivers one record; callback errors stop the
// stream by being returned (contract: callback returns an error to stop).
func emitRecord(ctx context.Context, req core.LogRequest, content, podName string, at time.Time, cb func(core.LogRecord) error) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("log stream: %w", err)
		}
	}
	rec := core.LogRecord{
		PodName:    podName,
		Container:  req.Container,
		Content:    content,
		ReceivedAt: at,
	}
	return cb(rec)
}

// readStreamLine reads one \\n-terminated line, bounding its length. When a
// line exceeds MaxLogRecordBytes it consumes through the end of the line
// and reports overflow via the parser flag set by the caller's loop (the
// reader cap is enforced here by returning the oversized remainder info as
// an error-free long line).
func readStreamLine(reader *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		out = append(out, chunk...)
		if err == nil {
			// Includes the newline; caller strips.
			return out, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue // keep assembling a long line (bounded by caller cap)
		}
		if errors.Is(err, io.EOF) {
			return out, io.EOF
		}
		return out, err
	}
}

// bufioReader wraps with a generous buffer (bufio.Scanner's default token
// size must not be relied on — plan slice 4; the manual ReadSlice loop
// handles tokens of any size).
func bufioReader(r io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(r, 64*1024)
}
