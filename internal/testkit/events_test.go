package testkit

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// Demo events cover controller, scheduler, kubelet, retry, and out-of-memory outcomes.
func TestDemoEvents(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	reasons := map[string]bool{}
	for _, e := range f.Events {
		reasons[e.Reason] = true
		inDemo := e.Namespace == DemoNamespace || e.Namespace == DemoMLNamespace
		if e.UID == "" || !inDemo || e.LastSeen.IsZero() || e.Count < 1 {
			t.Fatalf("incomplete event %+v", e)
		}
	}
	for _, want := range []string{"Scheduled", "Pulled", "Started", "BackOff", "OOMKilling", "WorkflowRunning", "WorkflowFailed", "WorkflowNodeFailed"} {
		if !reasons[want] {
			t.Errorf("the demo has no %s event", want)
		}
	}
}

// Event streams filter stored and published records, preserve time order, and stop on cancellation.
func TestFakeWatchEvents(t *testing.T) {
	events := []core.Event{
		{UID: "late", Namespace: "demo", ObjectKind: "Workflow", ObjectName: "run", Reason: "Failed", Type: "Warning", LastSeen: FixtureEpoch.Add(time.Second)},
		{UID: "other", Namespace: "other", ObjectKind: "Pod", ObjectName: "pod", Reason: "Started", Type: "Normal", LastSeen: FixtureEpoch.Add(2 * time.Second)},
		{UID: "early", Namespace: "demo", ObjectKind: "Workflow", ObjectName: "run", Reason: "Running", Type: "Normal", LastSeen: FixtureEpoch},
	}
	cases := []struct {
		name    string
		request core.EventWatchRequest
		want    []string
		invalid bool
	}{
		{"all oldest first", core.EventWatchRequest{}, []string{"early", "late", "other"}, false},
		{"namespace", core.EventWatchRequest{Namespace: "demo"}, []string{"early", "late"}, false},
		{"missing namespace", core.EventWatchRequest{Namespace: "missing"}, nil, false},
		{"workflow conjunction", core.EventWatchRequest{FieldSelector: "involvedObject.kind=Workflow,involvedObject.name=run"}, []string{"early", "late"}, false},
		{"object namespace", core.EventWatchRequest{FieldSelector: "involvedObject.namespace=other"}, []string{"other"}, false},
		{"reason", core.EventWatchRequest{FieldSelector: "reason=Failed"}, []string{"late"}, false},
		{"type", core.EventWatchRequest{FieldSelector: "type=Normal"}, []string{"early", "other"}, false},
		{"unsupported operator", core.EventWatchRequest{FieldSelector: "involvedObject.name!=x"}, nil, true},
		{"unsupported field", core.EventWatchRequest{FieldSelector: "metadata.labels=x"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &FakeReader{Events: events}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var got []string
			err := f.WatchEvents(ctx, c.request, func(e core.Event) error { got = append(got, e.UID); return nil })
			if c.invalid {
				var watchErr *core.WatchError
				if !errors.As(err, &watchErr) {
					t.Fatalf("error = %v, want WatchError", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want canceled", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("UIDs = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("published events and cancellation", func(t *testing.T) {
		ready := make(chan struct{})
		f := &FakeReader{EventHook: func(core.EventWatchRequest) error { close(ready); return nil }}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		got := make(chan core.Event, 2)
		done := make(chan error, 1)
		go func() {
			done <- f.WatchEvents(ctx, core.EventWatchRequest{Namespace: "demo", FieldSelector: "reason=Evicted"}, func(e core.Event) error { got <- e; return nil })
		}()
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("event stream did not start")
		}
		f.PublishEvent(core.Event{UID: "ignored", Namespace: "demo", Reason: "Pulled"})
		f.PublishEvent(core.Event{UID: "delivered", Namespace: "demo", Reason: "Evicted"})
		select {
		case e := <-got:
			if e.UID != "delivered" {
				t.Fatalf("UID = %q, want delivered", e.UID)
			}
		case <-time.After(time.Second):
			t.Fatal("published event did not arrive")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || f.EventCancelCount() != 1 {
				t.Fatalf("error = %v, cancels = %d; want canceled and 1", err, f.EventCancelCount())
			}
		case <-time.After(time.Second):
			t.Fatal("event stream did not cancel")
		}
	})
}
