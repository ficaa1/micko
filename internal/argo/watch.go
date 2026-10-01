package argo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ficaa1/micko/internal/core"
)

// maxWatchEventBytes bounds one wire event so a peer cannot grow the stream
// parser without limit. Callers own reconnect/relist policy.
const maxWatchEventBytes = 2 * 1024 * 1024

type watchEnvelope struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
	Result json.RawMessage `json:"result"`
	Error  *errorChunk     `json:"error"`
}

type watchErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// Watch implements the Argo workflow-events stream. It emits accepted events
// serially and never retries, reconnects, or relists.
func (c *Client) Watch(ctx context.Context, req core.WatchRequest, cb func(core.WatchEvent) error) error {
	path := "/api/v1/workflow-events/" + url.PathEscape(req.Namespace)
	query := url.Values{}
	if req.ResourceVersion != "" {
		query.Set("listOptions.resourceVersion", req.ResourceVersion)
	}
	if req.LabelSelector != "" {
		query.Set("listOptions.labelSelector", req.LabelSelector)
	}
	body, err := c.openWatch(ctx, endpointWatch, path, query, req.ResourceVersion)
	if err != nil {
		return err
	}
	defer drainAndClose(body)
	return readWatchStream(ctx, body, req.ResourceVersion, false, func(typ string, object json.RawMessage, lastRV string) (string, error) {
		wf, err := decodeRawWorkflow(object)
		if err != nil {
			return "", core.NewWatchError(core.WatchProtocol, "watch workflow object is invalid", lastRV, err)
		}
		rv := wf.Summary.ResourceVersion
		if rv == "" {
			rv = lastRV
		}
		return rv, cb(core.WatchEvent{Type: typ, Summary: wf.Summary, ResourceVersion: rv})
	})
}

// openWatch starts a gateway watch stream and returns its body. A transport
// failure and an HTTP error both come back as a *core.WatchError that keeps
// lastRV and, where one applies, the APIError the status maps to.
func (c *Client) openWatch(ctx context.Context, ep endpoint, path string, query url.Values, lastRV string) (io.ReadCloser, error) {
	httpReq, err := c.newRequest(ctx, path, query)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, httpReq, ep)
	if err != nil {
		return nil, watchTransportError(ctx, lastRV, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer drainAndClose(resp.Body)
		ae := mapHTTPError(resp, http.MethodGet, path, readBody(resp.Body), nil, c.now)
		return nil, classifyWatchHTTP(lastRV, ae)
	}
	return resp.Body, nil
}

// readWatchStream reads one gateway watch stream until it ends: JSON lines
// or SSE frames, one envelope per line, each line bounded by
// maxWatchEventBytes, keepalive comments skipped, and an in-band error or an
// ERROR event turned into a *core.WatchError. Every object goes to onObject
// with the type its envelope named; onObject returns the resource version
// the object carried, which becomes the cursor a reconnect resumes from, or
// an error that stops the stream as it is. bareResult accepts a result that
// is the object itself rather than a {type, object} pair, which is how the
// Kubernetes event stream sends its events. A clean end of the body is
// WatchEnded: the stream stopping says nothing about what happened next.
func readWatchStream(ctx context.Context, body io.Reader, lastRV string, bareResult bool,
	onObject func(typ string, object json.RawMessage, lastRV string) (string, error)) error {
	reader := bufio.NewReaderSize(body, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := readWatchLine(reader)
		if len(line) > 0 {
			line = stripWatchSSE(line)
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte(":")) {
				if errors.Is(err, io.EOF) {
					return core.NewWatchError(core.WatchEnded, "watch stream ended before a terminal event; outcome may be stale", lastRV, io.EOF)
				}
				continue
			}
			typ, object, parsedErr := decodeWatchEnvelope(line, lastRV, bareResult)
			if parsedErr != nil {
				return parsedErr
			}
			if object != nil {
				rv, cbErr := onObject(typ, object, lastRV)
				if cbErr != nil {
					return cbErr
				}
				if rv != "" {
					lastRV = rv
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return core.NewWatchError(core.WatchEnded, "watch stream ended before a terminal event; outcome may be stale", lastRV, io.EOF)
		}
		if err != nil {
			var watchErr *core.WatchError
			if errors.As(err, &watchErr) {
				watchErr.LastResourceVersion = lastRV
				return err
			}
			return core.NewWatchError(core.WatchEnded, "watch stream read failed", lastRV, err)
		}
	}
}

func readWatchLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice('\n')
		out = append(out, part...)
		if len(out) > maxWatchEventBytes {
			return nil, core.NewWatchError(core.WatchProtocol, "watch event exceeds client size cap", "", nil)
		}
		if err == nil {
			return bytesTrimLine(out), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytesTrimLine(out), err
	}
}

func bytesTrimLine(b []byte) []byte {
	return []byte(strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"))
}

func stripWatchSSE(line []byte) []byte {
	line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
	if bytes.HasPrefix(line, []byte("data: ")) {
		return line[len("data: "):]
	}
	if bytes.Equal(line, []byte("data:")) {
		return nil
	}
	return line
}

// decodeWatchEnvelope unwraps one stream line: {"type", "object"} bare or
// inside "result", or with bareResult a result that is the object itself.
// It returns the envelope's type and the raw object, or the error the line
// carries in band.
func decodeWatchEnvelope(line []byte, lastRV string, bareResult bool) (string, json.RawMessage, error) {
	var env watchEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return "", nil, core.NewWatchError(core.WatchProtocol, "watch event is not valid JSON", lastRV, err)
	}
	if len(env.Result) > 0 {
		result := env.Result
		if err := json.Unmarshal(result, &env); err != nil {
			return "", nil, core.NewWatchError(core.WatchProtocol, "watch result is not valid JSON", lastRV, err)
		}
		if bareResult && env.Error == nil && len(env.Object) == 0 {
			return "", result, nil
		}
	}
	if env.Error != nil {
		return "", nil, classifyWatchChunk(env.Error, lastRV)
	}
	if len(env.Object) == 0 {
		return "", nil, core.NewWatchError(core.WatchProtocol, "watch event has no object", lastRV, nil)
	}
	if env.Type == "ERROR" {
		var obj watchErrorObject
		if err := json.Unmarshal(env.Object, &obj); err != nil {
			return "", nil, core.NewWatchError(core.WatchProtocol, "watch error object is invalid", lastRV, err)
		}
		return "", nil, classifyWatchStatus(obj.Code, obj.Message, lastRV)
	}
	return env.Type, env.Object, nil
}

func classifyWatchChunk(ec *errorChunk, lastRV string) error {
	code := int(ec.HttpCode)
	if code == 0 {
		code = int(ec.HttpCodeAlt)
	}
	if code == 0 && ec.Code != 0 {
		code = grpcHTTPStatus(int(ec.Code))
	}
	if code == 0 && ec.GrpcCode != 0 {
		code = grpcHTTPStatus(int(ec.GrpcCode))
	}
	return classifyWatchStatus(code, ec.Message, lastRV)
}

func classifyWatchStatus(status int, message, lastRV string) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return core.NewWatchError(core.WatchEnded, redactMessage(message), lastRV,
			core.NewAPIError(core.KindOf(status), 0, redactMessage(message)))
	}
	if status == http.StatusTooManyRequests {
		return core.NewWatchError(core.WatchEnded, redactMessage(message), lastRV,
			core.NewAPIError(core.ErrRateLimited, 0, redactMessage(message)))
	}
	kind := core.WatchProtocol
	if status == http.StatusGone || status == http.StatusConflict {
		kind = core.WatchExpired
	}
	if status == http.StatusNotImplemented {
		kind = core.WatchUnsupported
	}
	if message == "" {
		message = fmt.Sprintf("watch server error (HTTP %d)", status)
	}
	return core.NewWatchError(kind, redactMessage(message), lastRV, nil)
}

func classifyWatchHTTP(lastRV string, ae *core.APIError) error {
	if ae == nil {
		return core.NewWatchError(core.WatchProtocol, "watch request failed", lastRV, nil)
	}
	kind := core.WatchProtocol
	if ae.Status == http.StatusGone || ae.Status == http.StatusConflict {
		kind = core.WatchExpired
	}
	if ae.Status == http.StatusNotImplemented {
		kind = core.WatchUnsupported
	}
	return core.NewWatchError(kind, ae.Message, lastRV, ae)
}

func watchTransportError(ctx context.Context, lastRV string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return core.NewWatchError(core.WatchEnded, "watch connection failed before a definitive result", lastRV, err)
}
