package argo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"argo-tui/internal/core"
)

func TestWatchDecodesEventsBookmarksAndUnknownPhase(t *testing.T) {
	body := strings.Join([]string{
		`{"type":"ADDED","object":{"metadata":{"namespace":"ns","name":"wf","uid":"u","resourceVersion":"11"},"status":{"phase":"NewPhase"}}}`,
		`{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"12"}}}`,
	}, "\n") + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
	defer srv.Close()
	c := newTestClient(t, srv)
	var got []core.WatchEvent
	err := c.Watch(context.Background(), core.WatchRequest{Namespace: "ns", LabelSelector: "x=y", ResourceVersion: "10"}, func(e core.WatchEvent) error {
		got = append(got, e)
		return nil
	})
	var ended *core.WatchError
	if !errors.As(err, &ended) || ended.Kind != core.WatchEnded {
		t.Fatalf("Watch: expected ambiguous EOF, got %v", err)
	}
	if len(got) != 2 || got[0].Type != core.WatchAdded || got[0].Summary.Phase != "NewPhase" || got[1].Type != core.WatchBookmark || got[1].ResourceVersion != "12" {
		t.Fatalf("events = %#v", got)
	}
}

func TestWatchExpiredHTTP410IsClassified(t *testing.T) {
	srv := serveFixture(t, http.StatusGone, []byte(`{"code":10,"message":"resource version too old"}`), nil)
	c := newTestClient(t, srv)
	err := c.Watch(context.Background(), core.WatchRequest{Namespace: "ns", ResourceVersion: "rv"}, func(core.WatchEvent) error { return nil })
	var we *core.WatchError
	if !errors.As(err, &we) || we.Kind != core.WatchExpired || we.LastResourceVersion != "rv" {
		t.Fatalf("error = %#v", err)
	}
}

func TestWatchHTTPAuthAndThrottleErrorsPreserveAPIClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		kind   core.ErrorKind
	}{
		{"unauthenticated", http.StatusUnauthorized, core.ErrUnauthenticated},
		{"forbidden", http.StatusForbidden, core.ErrForbidden},
		{"rate limited", http.StatusTooManyRequests, core.ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "7")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"code":16,"message":"watch denied"}`))
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv)
			err := c.Watch(context.Background(), core.WatchRequest{Namespace: "ns", ResourceVersion: "rv"}, func(core.WatchEvent) error { return nil })
			var we *core.WatchError
			var ae *core.APIError
			if !errors.As(err, &we) || !errors.As(err, &ae) || ae.Kind != tc.kind || we.LastResourceVersion != "rv" {
				t.Fatalf("error = %v, watch=%#v api=%#v", err, we, ae)
			}
			if tc.status == http.StatusTooManyRequests && (ae.RetryAfter == nil || *ae.RetryAfter != 7*time.Second) {
				t.Fatalf("Retry-After = %#v", ae.RetryAfter)
			}
		})
	}
}

func TestWatchAcceptsWrappedJSONLinesAndSSEFrames(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, suffix string
	}{
		{"json lines", "", "\n"},
		{"sse", "data: ", "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.prefix + `{"result":{"type":"MODIFIED","object":{"metadata":{"namespace":"ns","name":"wf","uid":"u","resourceVersion":"22"},"status":{"phase":"Running"}}}}` + tc.suffix
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			c := newTestClient(t, srv)
			var got []core.WatchEvent
			err := c.Watch(context.Background(), core.WatchRequest{Namespace: "ns"}, func(e core.WatchEvent) error { got = append(got, e); return nil })
			var ended *core.WatchError
			if !errors.As(err, &ended) || ended.Kind != core.WatchEnded || len(got) != 1 || got[0].Type != core.WatchModified || got[0].Summary.Phase != "Running" {
				t.Fatalf("got=%#v err=%v", got, err)
			}
		})
	}
}

func TestWatchCancellationStopsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	done := make(chan error, 1)
	go func() {
		done <- c.Watch(ctx, core.WatchRequest{Namespace: "ns"}, func(core.WatchEvent) error { return nil })
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not cancel")
	}
}

func TestActionsUseExactPUTBodiesAndSingleRequest(t *testing.T) {
	var calls int32
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"metadata":{"namespace":"ns","name":"wf","uid":"new"},"status":{"phase":"Running"}}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	cases := []struct {
		action             core.Action
		wantPath, wantBody string
	}{
		{core.ActionRetry, "/api/v1/workflows/ns/wf/retry", `{"name":"wf","namespace":"ns"}`},
		{core.ActionResubmit, "/api/v1/workflows/ns/wf/resubmit", `{"name":"wf","namespace":"ns"}`},
		{core.ActionStop, "/api/v1/workflows/ns/wf/stop", `{"name":"wf","namespace":"ns"}`},
		{core.ActionTerminate, "/api/v1/workflows/ns/wf/terminate", `{"name":"wf","namespace":"ns"}`},
	}
	for _, tc := range cases {
		atomic.StoreInt32(&calls, 0)
		_, err := c.Execute(context.Background(), core.ActionRequest{Action: tc.action, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "old"}, Confirmation: core.Confirmation{Confirmed: true, TypedName: "wf"}})
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if calls != 1 || method != http.MethodPut || path != tc.wantPath || body != tc.wantBody {
			t.Fatalf("%s request = %d %s %s %q", tc.action, calls, method, path, body)
		}
	}
}

func TestResubmitReturnsServerIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"metadata":{"namespace":"ns","name":"wf-abc","uid":"new"},"status":{"phase":"Pending"}}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionResubmit, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "old"}, Confirmation: core.Confirmation{Confirmed: true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Affected == nil || *result.Affected != (core.Ref{Namespace: "ns", Name: "wf-abc", UID: "new"}) {
		t.Fatalf("affected = %#v", result.Affected)
	}
}

func TestActionDisconnectIsUnknownAndNotRetried(t *testing.T) {
	var calls int32
	served := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		close(served)
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionTerminate, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Confirmation: core.Confirmation{Confirmed: true, TypedName: "wf"}})
	<-served
	if err == nil || result.Outcome != core.ActionUnknown || atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, calls)
	}
}

func TestActionUnknownPhaseAndErrorResponseCompatibility(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"metadata":{"namespace":"ns","name":"wf","uid":"u"},"status":{"phase":"FuturePhase"},"newField":{"x":1}}`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	result, err := c.Execute(context.Background(), core.ActionRequest{Action: core.ActionStop, Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, Confirmation: core.Confirmation{Confirmed: true}})
	if err != nil || result.Workflow == nil || result.Workflow.Summary.Phase != "FuturePhase" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !json.Valid(result.Response) {
		t.Fatal("response was not preserved")
	}
}
