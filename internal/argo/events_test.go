package argo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// Payloads below follow io.k8s.api.core.v1.Event as the Argo server's
// WorkflowService_WatchEvents streams it: {"result": <Event>} per line.

const podEventJSON = `{"metadata":{"name":"wf-pod.17f0a","namespace":"ns","uid":"e1","resourceVersion":"501","creationTimestamp":"2026-09-08T11:00:00Z"},` +
	`"involvedObject":{"kind":"Pod","namespace":"ns","name":"wf-main-123","uid":"p1","apiVersion":"v1","fieldPath":"spec.containers{main}"},` +
	`"reason":"BackOff","message":"Back-off restarting failed container main in pod wf-main-123","source":{"component":"kubelet","host":"worker-1"},` +
	`"firstTimestamp":"2026-09-08T11:00:00Z","lastTimestamp":"2026-09-08T11:04:00Z","count":4,"type":"Warning","eventTime":null,"reportingComponent":"","reportingInstance":""}`

const workflowEventJSON = `{"metadata":{"name":"wf.17f0b","namespace":"ns","uid":"e2","resourceVersion":"502","creationTimestamp":"2026-09-08T11:05:00Z"},` +
	`"involvedObject":{"kind":"Workflow","namespace":"ns","name":"wf","uid":"w1","apiVersion":"argoproj.io/v1alpha1"},` +
	`"reason":"WorkflowFailed","message":"child 'main' failed","source":{"component":"workflow-controller"},` +
	`"firstTimestamp":"2026-09-08T11:05:00Z","lastTimestamp":"2026-09-08T11:05:00Z","count":1,"type":"Warning"}`

// A series event (events.k8s.io style): no count or timestamps of its own,
// an event time and a series.
const seriesEventJSON = `{"metadata":{"uid":"e3","resourceVersion":"503","creationTimestamp":"2026-09-08T11:00:00Z"},` +
	`"involvedObject":{"kind":"Pod","name":"wf-main-123"},"reason":"Unhealthy","message":"Readiness probe failed","type":"Warning",` +
	`"eventTime":"2026-09-08T11:01:00.123456Z","series":{"count":9,"lastObservedTime":"2026-09-08T11:09:00.000001Z"},"reportingComponent":"kubelet"}`

// eventServer serves body on the event stream path and records the request.
func eventServer(t *testing.T, body string, got *url.URL) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			*got = *r.URL
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collectEvents(t *testing.T, c *Client, req core.EventWatchRequest) ([]core.Event, error) {
	t.Helper()
	var out []core.Event
	err := c.WatchEvents(context.Background(), req, func(e core.Event) error {
		out = append(out, e)
		return nil
	})
	return out, err
}

// The stream decodes in JSON lines and in SSE frames, with the result as the
// event itself or as a {type, object} pair, and ends as WatchEnded when the
// body ends. The request carries the field selector and resource version.
func TestWatchEventsDecodesBothFramings(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, suffix string
	}{
		{"json lines", "", "\n"},
		{"sse", "data: ", "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.prefix + `{"result":` + podEventJSON + `}` + tc.suffix +
				": keepalive\n" +
				tc.prefix + `{"result":{"type":"ADDED","object":` + workflowEventJSON + `}}` + tc.suffix +
				tc.prefix + `{"result":` + seriesEventJSON + `}` + tc.suffix
			var got url.URL
			c := newTestClient(t, eventServer(t, body, &got))
			evs, err := collectEvents(t, c, core.EventWatchRequest{Namespace: "ns", FieldSelector: "involvedObject.kind=Pod", ResourceVersion: "400"})
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != core.WatchEnded || we.LastResourceVersion != "503" {
				t.Fatalf("err = %v (%#v)", err, we)
			}
			if got.Path != "/api/v1/stream/events/ns" || got.Query().Get("listOptions.fieldSelector") != "involvedObject.kind=Pod" ||
				got.Query().Get("listOptions.resourceVersion") != "400" {
				t.Errorf("request = %s", got.String())
			}
			if len(evs) != 3 {
				t.Fatalf("events = %#v", evs)
			}
			p := evs[0]
			if p.UID != "e1" || p.Type != "Warning" || p.Reason != "BackOff" || p.ObjectKind != "Pod" || p.ObjectName != "wf-main-123" ||
				p.Count != 4 || p.Source != "kubelet" || p.Namespace != "ns" || p.ResourceVersion != "501" ||
				!p.FirstSeen.Equal(time.Date(2026, 9, 8, 11, 0, 0, 0, time.UTC)) || !p.LastSeen.Equal(time.Date(2026, 9, 8, 11, 4, 0, 0, time.UTC)) {
				t.Errorf("pod event = %#v", p)
			}
			if w := evs[1]; w.ObjectKind != "Workflow" || w.Reason != "WorkflowFailed" || w.Source != "workflow-controller" || w.Deleted {
				t.Errorf("workflow event = %#v", w)
			}
			s := evs[2]
			if s.Count != 9 || s.Source != "kubelet" || !s.LastSeen.Equal(time.Date(2026, 9, 8, 11, 9, 0, 1000, time.UTC)) ||
				!s.FirstSeen.Equal(time.Date(2026, 9, 8, 11, 1, 0, 123456000, time.UTC)) {
				t.Errorf("series event = %#v", s)
			}
		})
	}
}

// A DELETED event is marked and a BOOKMARK only moves the cursor.
func TestWatchEventsDeletedAndBookmark(t *testing.T) {
	body := `{"result":{"type":"DELETED","object":` + podEventJSON + `}}` + "\n" +
		`{"result":{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"900"}}}}` + "\n"
	c := newTestClient(t, eventServer(t, body, nil))
	evs, err := collectEvents(t, c, core.EventWatchRequest{Namespace: "ns"})
	var we *core.WatchError
	if !errors.As(err, &we) || we.LastResourceVersion != "900" {
		t.Fatalf("err = %v", err)
	}
	if len(evs) != 1 || !evs[0].Deleted {
		t.Fatalf("events = %#v", evs)
	}
}

// An error in band ends the stream with its classification: permission
// errors keep their APIError, an expired cursor is WatchExpired, and a
// malformed line is a protocol error.
func TestWatchEventsInBandErrors(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		kind       core.WatchErrorKind
		api        core.ErrorKind
	}{
		{"forbidden", `{"error":{"grpc_code":7,"http_code":403,"message":"events is forbidden: User \"system:serviceaccount:argo:argo-server\" cannot watch resource \"events\""}}`, core.WatchEnded, core.ErrForbidden},
		{"expired", `{"result":{"type":"ERROR","object":{"code":410,"message":"too old resource version: 400 (900)"}}}`, core.WatchExpired, ""},
		{"gateway code", `{"error":{"code":16,"message":"token expired"}}`, core.WatchEnded, core.ErrUnauthenticated},
		{"malformed", `{"result":`, core.WatchProtocol, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, eventServer(t, `{"result":`+podEventJSON+"}\n"+tc.line+"\n", nil))
			evs, err := collectEvents(t, c, core.EventWatchRequest{Namespace: "ns"})
			var we *core.WatchError
			if !errors.As(err, &we) || we.Kind != tc.kind || we.LastResourceVersion != "501" {
				t.Fatalf("err = %v (%#v)", err, we)
			}
			if len(evs) != 1 {
				t.Errorf("events before the error = %d", len(evs))
			}
			if tc.api != "" {
				if ae := core.AsAPIError(err); ae == nil || ae.Kind != tc.api {
					t.Errorf("api error = %#v, want %s", ae, tc.api)
				}
			}
		})
	}
}

// A 403 keeps its APIError, and a server with no event stream (404 or 501)
// is WatchUnsupported.
func TestWatchEventsHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   core.WatchErrorKind
		api    core.ErrorKind
	}{
		{http.StatusForbidden, core.WatchProtocol, core.ErrForbidden},
		{http.StatusNotFound, core.WatchUnsupported, core.ErrNotFound},
		{http.StatusNotImplemented, core.WatchUnsupported, ""},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"code":5,"message":"Not Found"}`)
		}))
		c := newTestClient(t, srv)
		_, err := collectEvents(t, c, core.EventWatchRequest{Namespace: "ns", ResourceVersion: "7"})
		srv.Close()
		var we *core.WatchError
		if !errors.As(err, &we) || we.Kind != tc.kind || we.LastResourceVersion != "7" {
			t.Errorf("%d: err = %v (%#v)", tc.status, err, we)
			continue
		}
		if tc.api != "" {
			if ae := core.AsAPIError(err); ae == nil || ae.Kind != tc.api {
				t.Errorf("%d: api error = %#v", tc.status, ae)
			}
		}
	}
}

// Canceling the context ends a stream the server holds open.
func TestWatchEventsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"result":`+podEventJSON+"}\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	done := make(chan error, 1)
	got := make(chan core.Event, 4)
	go func() {
		done <- c.WatchEvents(ctx, core.EventWatchRequest{Namespace: "ns"}, func(e core.Event) error { got <- e; return nil })
	}()
	<-started
	select {
	case e := <-got:
		if e.UID != "e1" {
			t.Fatalf("event = %#v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first event did not arrive while the stream stayed open")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream did not end on cancel")
	}
}
