package argo

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/ficaa1/micko/internal/diagnostics"
)

// endpoint names a call in request timings. It is one of the fixed values
// below, so no namespace, workflow name or query reaches the diagnostics.
type endpoint string

const (
	endpointList             endpoint = "list"
	endpointGate             endpoint = "gate"
	endpointGet              endpoint = "get"
	endpointInfo             endpoint = "info"
	endpointNamespaces       endpoint = "namespaces"
	endpointLogs             endpoint = "logs"
	endpointWatch            endpoint = "watch"
	endpointEvents           endpoint = "events"
	endpointCron             endpoint = "cron"
	endpointTemplates        endpoint = "templates"
	endpointClusterTemplates endpoint = "clustertemplates"
	endpointArchived         endpoint = "archived"
	endpointArchivedGet      endpoint = "archivedget"
	endpointAction           endpoint = "action"
)

// requestTimer collects the timings of one request. httptrace calls it from
// the transport's goroutines, so every field is guarded by mu.
type requestTimer struct {
	sink *diagnostics.Sink
	ep   endpoint

	mu                     sync.Mutex
	start                  time.Time
	connectStart, tlsStart time.Time
	connect, tls, ttfb     time.Duration
	reused                 bool
	status                 int
	bytes                  int64
	once                   sync.Once
}

// startTimer returns a timer for one request, or nil when diagnostics are off.
func (c *Client) startTimer(ep endpoint) *requestTimer {
	if c.diagnostics == nil {
		return nil
	}
	return &requestTimer{sink: c.diagnostics, ep: ep, start: time.Now()}
}

// trace returns ctx with the timer's hooks attached.
func (t *requestTimer) trace(ctx context.Context) context.Context {
	if t == nil {
		return ctx
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			t.reused = info.Reused
			t.mu.Unlock()
		},
		ConnectStart: func(string, string) {
			t.mu.Lock()
			t.connectStart = time.Now()
			t.mu.Unlock()
		},
		ConnectDone: func(string, string, error) {
			t.mu.Lock()
			t.connect = time.Since(t.connectStart)
			t.mu.Unlock()
		},
		TLSHandshakeStart: func() {
			t.mu.Lock()
			t.tlsStart = time.Now()
			t.mu.Unlock()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.mu.Lock()
			t.tls = time.Since(t.tlsStart)
			t.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			t.ttfb = time.Since(t.start)
			t.mu.Unlock()
		},
	})
}

// fail records a request that got no response.
func (t *requestTimer) fail() {
	if t != nil {
		t.emit(true)
	}
}

// track records resp when its body is read, so the timing covers reading
// the body too.
func (t *requestTimer) track(resp *http.Response) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.status = resp.StatusCode
	t.mu.Unlock()
	resp.Body = &timedBody{ReadCloser: resp.Body, t: t}
}

func (t *requestTimer) emit(failed bool) {
	t.once.Do(func() {
		t.mu.Lock()
		r := diagnostics.Request{
			Endpoint: string(t.ep), Failed: failed, Status: t.status, Reused: t.reused,
			Connect: t.connect, TLS: t.tls, TTFB: t.ttfb, Total: time.Since(t.start), Bytes: t.bytes,
		}
		t.mu.Unlock()
		t.sink.EmitRequest(r)
	})
}

// timedBody counts the bytes read and reports the timing at the end of the
// body or on Close, whichever comes first. A caller that closes the body
// late, after a second request, does not add that request to this one. A
// read error, such as a canceled stream, reports the timing as failed.
type timedBody struct {
	io.ReadCloser
	t *requestTimer
}

func (b *timedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.t.mu.Lock()
	b.t.bytes += int64(n)
	b.t.mu.Unlock()
	switch {
	case err == io.EOF:
		b.t.emit(false)
	case err != nil:
		b.t.emit(true)
	}
	return n, err
}

func (b *timedBody) Close() error {
	err := b.ReadCloser.Close()
	b.t.emit(false)
	return err
}
