package argo

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// logRequest is the request every log test streams.
var logRequest = core.LogRequest{Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Container: "main"}

// logServer answers with status, then writes each chunk as its own flushed
// read, delay apart, until the client goes away.
func logServer(t *testing.T, status int, chunks []string, delay time.Duration) *Client {
	t.Helper()
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		for _, ch := range chunks {
			if _, err := w.Write([]byte(ch)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
	})
	return newTestClient(t, srv)
}

// line is one JSON-lines log record.
func line(content, pod string) string {
	return `{"result":{"content":"` + content + `","podName":"` + pod + `"}}` + "\n"
}

// Records decode from JSON lines and SSE frames, whole or split, and one
// past the size cap becomes a marker.
func TestStreamLogsRecords(t *testing.T) {
	fixture := []string{"line-one pod-a", "line-two pod-b", "line-�bad pod-b", "final-no-newline pod-c"}
	atCap := strings.Repeat("y", maxLogRecordBytes-len(line("", "p1")))
	cases := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{"json lines", []string{string(loadFixture(t, "logs_jsonl.txt"))}, fixture},
		{"sse", []string{string(loadFixture(t, "logs_sse.txt"))}, fixture},
		{"split across reads", []string{
			line("alpha", "p1") + `{"result":{"content":"bet`,
			`a","podName":"p1"}}` + "\n" + line("gamma", "p2"),
		}, []string{"alpha p1", "beta p1", "gamma p2"}},
		{"over the cap", []string{
			line("ok-line", "p1"), line(strings.Repeat("x", 3<<20), "p1"), line("after", "p1"),
		}, []string{"ok-line p1", oversizeRecordMarker + " ", "after p1"}},
		{"at the cap", []string{line(atCap, "p1")}, []string{atCap + " p1"}},
		{"one byte over the cap", []string{line(atCap+"y", "p1")}, []string{oversizeRecordMarker + " "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			err := logServer(t, http.StatusOK, c.chunks, 5*time.Millisecond).StreamLogs(context.Background(), logRequest, func(r core.LogRecord) error {
				if r.Container != "main" || r.ReceivedAt.IsZero() {
					t.Errorf("record = %+v, want the requested container and a receive time", r)
				}
				got = append(got, r.Content+" "+r.PodName)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("records =\n%.200s\nwant\n%.200s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

// A failing log stream ends with a typed error after the records before it.
func TestStreamLogsErrors(t *testing.T) {
	before := line("before-error", "p1")
	cases := []struct {
		name   string
		status int
		chunk  string
		kind   core.ErrorKind
		code   int
		msg    string
	}{
		{"pod deleted", 200, `{"error":{"code":5,"message":"pod deleted"}}` + "\n", core.ErrNotFound, 0, "pod deleted"},
		{"forbidden", 200, `{"error":{"code":7,"message":"forbidden"}}` + "\n", core.ErrForbidden, 0, "forbidden"},
		{"too many open files", 200, `{"error":{"code":8,"message":"too many open files"}}` + "\n", core.ErrRateLimited, 0, "open files"},
		{"bad grep", 200, `{"error":{"code":3,"message":"invalid regexp"}}` + "\n", core.ErrInvalid, 0, "invalid regexp"},
		{"logs disabled", 200, `{"error":{"code":12,"message":"logs disabled"}}` + "\n", core.ErrUnsupported, 0, "logs disabled"},
		{"gateway shape", 200, `{"error":{"grpc_code":5,"http_code":404,"message":"pod gone","http_status":"Not Found"}}` + "\n",
			core.ErrNotFound, 0, "pod gone"},
		{"grpc code only", 200, `{"error":{"grpc_code":7,"message":"denied"}}` + "\n", core.ErrForbidden, 0, "denied"},
		{"http code only", 200, `{"error":{"http_code":403,"message":"denied"}}` + "\n", core.ErrForbidden, 0, "denied"},
		{"login page", 200, "<html><form>login</form></html>\n", core.ErrUnauthenticated, 401, "HTML page"},
		{"cut final record", 200, `{"result":{"content":"half`, core.ErrProtocol, 0, "truncated"},
		{"refused", 404, `{"code":5,"message":"workflow ns/ghost not found"}`, core.ErrNotFound, 404, "ghost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chunks := []string{before, c.chunk}
			wantRecords := 1
			if c.status != http.StatusOK {
				chunks, wantRecords = []string{c.chunk}, 0
			}
			var got int
			err := logServer(t, c.status, chunks, time.Millisecond).StreamLogs(context.Background(), logRequest, func(core.LogRecord) error {
				got++
				return nil
			})
			ae := wantAPIError(t, err, c.kind, c.msg)
			if ae.Status != c.code || got != wantRecords {
				t.Errorf("status %d after %d records, want %d after %d", ae.Status, got, c.code, wantRecords)
			}
		})
	}
}

// The stream stops at the first record when the callback fails or the
// context is canceled, and returns that cause.
func TestStreamLogsStops(t *testing.T) {
	full := errors.New("consumer full")
	cases := []struct {
		name string
		stop func(cancel context.CancelFunc) error
		want error
	}{
		{"callback error", func(context.CancelFunc) error { return full }, full},
		{"cancel", func(cancel context.CancelFunc) error { cancel(); return nil }, context.Canceled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cl := logServer(t, http.StatusOK, []string{line("first", "p1"), line("second", "p1"), line("third", "p1")}, 200*time.Millisecond)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var got int
			done := make(chan error, 1)
			go func() {
				done <- cl.StreamLogs(ctx, logRequest, func(core.LogRecord) error {
					got++
					return c.stop(cancel)
				})
			}()
			select {
			case err := <-done:
				if !errors.Is(err, c.want) || got != 1 {
					t.Fatalf("err = %v after %d records, want %v after 1", err, got, c.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the stream did not stop")
			}
		})
	}
}
