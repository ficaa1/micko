package testkit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// collect reads a fake event stream until it has been quiet for a moment.
func collect(t *testing.T, f *FakeReader, req core.EventWatchRequest) ([]core.Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out []core.Event
	err := f.WatchEvents(ctx, req, func(e core.Event) error { out = append(out, e); return nil })
	return out, err
}

// The demo serves the events a cluster would record: the controller's on
// the workflow, the scheduler's and kubelet's on the pods, and the
// warnings of a failing retry and an out-of-memory kill.
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

// The stream filters by namespace and by the equality terms of the field
// selector, sends the stored events oldest first, passes on published
// events, rejects terms the API server does not support for events, and
// ends with the context.
func TestFakeWatchEvents(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	evs, err := collect(t, f, core.EventWatchRequest{Namespace: DemoNamespace,
		FieldSelector: "involvedObject.kind=Workflow,involvedObject.name=demo-oom-backfill"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if len(evs) != 3 || evs[0].Reason != "WorkflowRunning" {
		t.Fatalf("workflow events = %+v", evs)
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].LastSeen.Before(evs[i-1].LastSeen) {
			t.Fatal("events not oldest first")
		}
	}
	if evs, _ := collect(t, f, core.EventWatchRequest{Namespace: "other"}); len(evs) != 0 {
		t.Errorf("another namespace got %d events", len(evs))
	}
	if _, err := collect(t, f, core.EventWatchRequest{Namespace: DemoNamespace, FieldSelector: "involvedObject.name!=x"}); err == nil {
		t.Error("an unsupported term was accepted")
	}
	if _, err := collect(t, f, core.EventWatchRequest{Namespace: DemoNamespace, FieldSelector: "metadata.labels=x"}); err == nil {
		t.Error("an unsupported field was accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan core.Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- f.WatchEvents(ctx, core.EventWatchRequest{Namespace: DemoNamespace, FieldSelector: "reason=Evicted"},
			func(e core.Event) error { got <- e; return nil })
	}()
	for f.EventStartCount() < 5 {
		time.Sleep(5 * time.Millisecond)
	}
	for {
		f.mu.Lock()
		n := len(f.eventSubs)
		f.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.PublishEvent(core.Event{UID: "x", Namespace: DemoNamespace, Reason: "Pulled"})
	f.PublishEvent(core.Event{UID: "y", Namespace: DemoNamespace, Reason: "Evicted"})
	select {
	case e := <-got:
		if e.UID != "y" {
			t.Fatalf("published %+v passed the filter", e)
		}
	case <-time.After(time.Second):
		t.Fatal("the published event did not arrive")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || f.EventCancelCount() == 0 {
		t.Fatalf("err = %v, cancels %d", err, f.EventCancelCount())
	}
}
