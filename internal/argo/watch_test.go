package argo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// Kubernetes events as the Argo server streams them.
const (
	podEventJSON = `{"metadata":{"name":"wf-pod.17f0a","namespace":"ns","uid":"e1","resourceVersion":"501","creationTimestamp":"2026-09-08T11:00:00Z"},` +
		`"involvedObject":{"kind":"Pod","namespace":"ns","name":"wf-main-123","uid":"p1","apiVersion":"v1","fieldPath":"spec.containers{main}"},` +
		`"reason":"BackOff","message":"Back-off restarting failed container main in pod wf-main-123","source":{"component":"kubelet","host":"worker-1"},` +
		`"firstTimestamp":"2026-09-08T11:00:00Z","lastTimestamp":"2026-09-08T11:04:00Z","count":4,"type":"Warning","eventTime":null,"reportingComponent":"","reportingInstance":""}`
	workflowEventJSON = `{"metadata":{"name":"wf.17f0b","namespace":"ns","uid":"e2","resourceVersion":"502","creationTimestamp":"2026-09-08T11:05:00Z"},` +
		`"involvedObject":{"kind":"Workflow","namespace":"ns","name":"wf","uid":"w1","apiVersion":"argoproj.io/v1alpha1"},` +
		`"reason":"WorkflowFailed","message":"child 'main' failed","source":{"component":"workflow-controller"},` +
		`"firstTimestamp":"2026-09-08T11:05:00Z","lastTimestamp":"2026-09-08T11:05:00Z","count":1,"type":"Warning"}`
	// seriesEventJSON has no count or timestamps of its own, only an event
	// time and a series.
	seriesEventJSON = `{"metadata":{"uid":"e3","resourceVersion":"503","creationTimestamp":"2026-09-08T11:00:00Z"},` +
		`"involvedObject":{"kind":"Pod","name":"wf-main-123"},"reason":"Unhealthy","message":"Readiness probe failed","type":"Warning",` +
		`"eventTime":"2026-09-08T11:01:00.123456Z","series":{"count":9,"lastObservedTime":"2026-09-08T11:09:00.000001Z"},"reportingComponent":"kubelet"}`
)

// framings are the two ways the server frames a stream line.
var framings = []struct{ name, prefix, suffix string }{
	{"json lines", "", "\n"},
	{"sse", "data: ", "\n\n"},
}

// frame writes each line in one framing, after a keepalive.
func frame(prefix, suffix string, lines ...string) string {
	out := ": keepalive\n"
	for _, l := range lines {
		out += prefix + l + suffix
	}
	return out
}

// streamServer serves body once and records the request URL.
func streamServer(t *testing.T, body string, got *url.URL) *Client {
	t.Helper()
	return newTestClient(t, serve(t, func(w http.ResponseWriter, r *http.Request) {
		*got = *r.URL
		_, _ = io.WriteString(w, body)
	}))
}

// The workflow watch decodes each event framing and ends as WatchEnded at
// the last cursor.
func TestWatchDecodesTheStream(t *testing.T) {
	added := `{"type":"ADDED","object":{"metadata":{"namespace":"ns","name":"wf","uid":"u","resourceVersion":"11"},"status":{"phase":"NewPhase"}}}`
	modified := `{"result":{"type":"MODIFIED","object":{"metadata":{"namespace":"ns","name":"wf","uid":"u","resourceVersion":"12"},"status":{"phase":"Running"}}}}`
	bookmark := `{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"13"}}}`
	for _, f := range framings {
		t.Run(f.name, func(t *testing.T) {
			var req url.URL
			c := streamServer(t, frame(f.prefix, f.suffix, added, modified, bookmark), &req)
			var got []string
			err := c.Watch(context.Background(), core.WatchRequest{Namespace: "ns", LabelSelector: "x=y", ResourceVersion: "10"}, func(e core.WatchEvent) error {
				got = append(got, e.Type+" "+e.Summary.Phase+" "+e.ResourceVersion)
				return nil
			})
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != core.WatchEnded || we.LastResourceVersion != "13" {
				t.Fatalf("err = %v, want the stream ended at 13", err)
			}
			if want := "ADDED NewPhase 11|MODIFIED Running 12|BOOKMARK  13"; strings.Join(got, "|") != want {
				t.Errorf("events = %q, want %q", strings.Join(got, "|"), want)
			}
			if req.Path != "/api/v1/workflow-events/ns" || req.Query().Get("listOptions.labelSelector") != "x=y" || req.Query().Get("listOptions.resourceVersion") != "10" {
				t.Errorf("request = %s", req.String())
			}
		})
	}
}

// The event watch decodes bare, paired, series and deleted events, and a
// bookmark only moves the cursor.
func TestWatchEventsDecodesTheStream(t *testing.T) {
	for _, f := range framings {
		t.Run(f.name, func(t *testing.T) {
			var req url.URL
			c := streamServer(t, frame(f.prefix, f.suffix,
				`{"result":`+podEventJSON+`}`,
				`{"result":{"type":"ADDED","object":`+workflowEventJSON+`}}`,
				`{"result":`+seriesEventJSON+`}`,
				`{"result":{"type":"DELETED","object":`+podEventJSON+`}}`,
				`{"result":{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"900"}}}}`,
			), &req)
			var evs []core.Event
			err := c.WatchEvents(context.Background(), core.EventWatchRequest{Namespace: "ns", FieldSelector: "involvedObject.kind=Pod", ResourceVersion: "400"},
				func(e core.Event) error { evs = append(evs, e); return nil })
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != core.WatchEnded || we.LastResourceVersion != "900" {
				t.Fatalf("err = %v, want the stream ended at 900", err)
			}
			if req.Path != "/api/v1/stream/events/ns" || req.Query().Get("listOptions.fieldSelector") != "involvedObject.kind=Pod" ||
				req.Query().Get("listOptions.resourceVersion") != "400" {
				t.Errorf("request = %s", req.String())
			}
			if len(evs) != 4 {
				t.Fatalf("events = %+v", evs)
			}
			at := func(h, m, s, ns int) time.Time { return time.Date(2026, 9, 8, h, m, s, ns, time.UTC) }
			p := evs[0]
			if p.UID != "e1" || p.Type != "Warning" || p.Reason != "BackOff" || p.ObjectKind != "Pod" || p.ObjectName != "wf-main-123" ||
				p.Count != 4 || p.Source != "kubelet" || p.Namespace != "ns" || p.ResourceVersion != "501" || p.Deleted ||
				!p.FirstSeen.Equal(at(11, 0, 0, 0)) || !p.LastSeen.Equal(at(11, 4, 0, 0)) {
				t.Errorf("pod event = %+v", p)
			}
			if w := evs[1]; w.ObjectKind != "Workflow" || w.Reason != "WorkflowFailed" || w.Source != "workflow-controller" {
				t.Errorf("workflow event = %+v", w)
			}
			if s := evs[2]; s.Count != 9 || s.Source != "kubelet" || !s.FirstSeen.Equal(at(11, 1, 0, 123456000)) || !s.LastSeen.Equal(at(11, 9, 0, 1000)) {
				t.Errorf("series event = %+v", s)
			}
			if d := evs[3]; !d.Deleted || d.UID != "e1" {
				t.Errorf("deleted event = %+v", d)
			}
		})
	}
}

// An error inside a stream ends it at the last event's cursor, classified by
// its code.
func TestWatchInBandErrors(t *testing.T) {
	cases := []struct {
		name, line string
		kind       core.WatchErrorKind
		api        core.ErrorKind
	}{
		{"forbidden", `{"error":{"grpc_code":7,"http_code":403,"message":"events is forbidden"}}`, core.WatchEnded, core.ErrForbidden},
		{"expired", `{"result":{"type":"ERROR","object":{"code":410,"message":"too old resource version: 400 (900)"}}}`, core.WatchExpired, ""},
		{"gateway code", `{"error":{"code":16,"message":"token expired"}}`, core.WatchEnded, core.ErrUnauthenticated},
		{"malformed", `{"result":`, core.WatchProtocol, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var req url.URL
			cl := streamServer(t, `{"result":`+podEventJSON+"}\n"+c.line+"\n", &req)
			var got int
			err := cl.WatchEvents(context.Background(), core.EventWatchRequest{Namespace: "ns"}, func(core.Event) error { got++; return nil })
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != c.kind || we.LastResourceVersion != "501" || got != 1 {
				t.Fatalf("err = %v after %d events, want %s at 501 after 1", err, got, c.kind)
			}
			if ae := core.AsAPIError(err); (ae == nil) != (c.api == "") || (ae != nil && ae.Kind != c.api) {
				t.Errorf("api error = %v, want %q", ae, c.api)
			}
		})
	}
}

// A refused watch keeps its cursor and is classified by its status.
func TestWatchRefusals(t *testing.T) {
	cases := []struct {
		stream string
		status int
		kind   core.WatchErrorKind
		api    core.ErrorKind
	}{
		{"workflows", http.StatusGone, core.WatchExpired, core.ErrProtocol},
		{"workflows", http.StatusUnauthorized, core.WatchProtocol, core.ErrUnauthenticated},
		{"workflows", http.StatusForbidden, core.WatchProtocol, core.ErrForbidden},
		{"workflows", http.StatusTooManyRequests, core.WatchProtocol, core.ErrRateLimited},
		{"workflows", http.StatusNotImplemented, core.WatchUnsupported, core.ErrUnsupported},
		{"events", http.StatusForbidden, core.WatchProtocol, core.ErrForbidden},
		{"events", http.StatusNotFound, core.WatchUnsupported, core.ErrNotFound},
		{"events", http.StatusNotImplemented, core.WatchUnsupported, core.ErrUnsupported},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %d", c.stream, c.status), func(t *testing.T) {
			cl := newTestClient(t, serveBody(t, c.status, `{"code":5,"message":"refused"}`))
			var err error
			if c.stream == "events" {
				err = cl.WatchEvents(context.Background(), core.EventWatchRequest{Namespace: "ns", ResourceVersion: "7"}, func(core.Event) error { return nil })
			} else {
				err = cl.Watch(context.Background(), core.WatchRequest{Namespace: "ns", ResourceVersion: "7"}, func(core.WatchEvent) error { return nil })
			}
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != c.kind || we.LastResourceVersion != "7" {
				t.Fatalf("err = %v, want %s at 7", err, c.kind)
			}
			if ae := core.AsAPIError(err); ae == nil || ae.Kind != c.api {
				t.Errorf("api error = %v, want %s", ae, c.api)
			}
		})
	}
}

// A quiet watch survives the dial timeout until its context is canceled.
func TestWatchesEndOnCancel(t *testing.T) {
	cases := []struct {
		name, line string
		watch      func(ctx context.Context, c *Client, got chan<- struct{}) error
	}{
		{"workflows", `{"result":{"type":"ADDED","object":{"metadata":{"namespace":"ns","name":"wf","uid":"u","resourceVersion":"1"}}}}`,
			func(ctx context.Context, c *Client, got chan<- struct{}) error {
				return c.Watch(ctx, core.WatchRequest{Namespace: "ns"}, func(core.WatchEvent) error { got <- struct{}{}; return nil })
			}},
		{"events", `{"result":` + podEventJSON + `}`,
			func(ctx context.Context, c *Client, got chan<- struct{}) error {
				return c.WatchEvents(ctx, core.EventWatchRequest{Namespace: "ns"}, func(core.Event) error { got <- struct{}{}; return nil })
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const dialTimeout = 50 * time.Millisecond
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(3 * dialTimeout)
				_, _ = io.WriteString(w, c.line+"\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := make(chan struct{}, 4)
			done := make(chan error, 1)
			client := newClientWith(t, Options{Server: srv.URL, DialTimeout: dialTimeout})
			go func() { done <- c.watch(ctx, client, got) }()
			select {
			case <-got:
			case err := <-done:
				t.Fatalf("the stream ended before its first event: %v", err)
			case <-time.After(2 * time.Second):
				t.Fatal("the first event did not arrive while the stream stayed open")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err = %v, want canceled", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the stream did not end on cancel")
			}
		})
	}
}
