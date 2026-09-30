package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// List filters and orders workflows before applying opaque continuation pages.
func TestFakeReaderList(t *testing.T) {
	workflows := []core.Workflow{
		SyntheticWorkflow("ns-a", "zeta", "Running", FixtureEpoch),
		SyntheticWorkflow("ns-a", "alpha", "Succeeded", FixtureEpoch),
		SyntheticWorkflow("ns-b", "other", "Failed", FixtureEpoch),
	}
	f := &FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	for _, wf := range workflows {
		f.Workflows[wf.Summary.Ref] = wf
	}
	cases := []struct {
		name  string
		query core.Query
		want  []string
	}{
		{"all namespaces sorted", core.Query{}, []string{"alpha", "zeta", "other"}},
		{"namespace", core.Query{Namespace: "ns-a"}, []string{"alpha", "zeta"}},
		{"missing namespace", core.Query{Namespace: "missing"}, nil},
		{"first page", core.Query{Namespace: "ns-a", Limit: 1}, []string{"alpha"}},
		{"uneven final page", core.Query{Limit: 2}, []string{"alpha", "zeta"}},
		{"invalid continuation restarts", core.Query{Namespace: "ns-a", Limit: 1, Continue: "garbage"}, []string{"alpha"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := f.List(context.Background(), c.query)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range page.Items {
				got = append(got, item.Ref.Name)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("names = %v, want %v", got, c.want)
			}
			if c.query.Limit > 0 {
				if page.Continue == "" {
					t.Fatal("first page has no continuation")
				}
				query := c.query
				query.Continue = page.Continue
				next, err := f.List(context.Background(), query)
				want := "zeta"
				if c.name == "uneven final page" {
					want = "other"
				}
				if err != nil || len(next.Items) != 1 || next.Items[0].Ref.Name != want || next.Continue != "" {
					t.Fatalf("second page = %+v, %v; want %s and no continuation", next, err, want)
				}
			} else if page.Continue != "" {
				t.Fatalf("continuation = %q, want empty", page.Continue)
			}
		})
	}
	t.Run("demo owner and forced pagination", func(t *testing.T) {
		demo := DemoReader(NewFakeClock(FixtureEpoch))
		page, err := demo.List(context.Background(), core.Query{Namespace: DemoNamespace})
		if err != nil || page.Continue == "" {
			t.Fatalf("demo page = %+v, %v; want continuation", page, err)
		}
		demo.PageLimit = 0
		owned, err := demo.List(context.Background(), core.Query{Namespace: DemoNamespace, LabelSelector: "workflows.argoproj.io/cron-workflow=demo-etl-hourly"})
		if err != nil || len(owned.Items) != 3 {
			t.Fatalf("owned runs = %d, %v; want 3", len(owned.Items), err)
		}
	})
}

// Reader failures and cancellation remain distinguishable to callers.
func TestFakeReaderInjectableErrorsAndDelays(t *testing.T) {
	t.Run("list error", func(t *testing.T) {
		f := &FakeReader{ListErr: core.ErrForbiddenf("list denied in ns")}
		_, err := f.List(context.Background(), core.Query{Namespace: "ns"})
		if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrForbidden {
			t.Fatalf("err = %v, want forbidden", err)
		}
	})
	t.Run("get error", func(t *testing.T) {
		f := &FakeReader{GetErr: core.ErrNotFoundf("gone")}
		_, err := f.Get(context.Background(), core.Ref{Namespace: "ns", Name: "wf"})
		if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrNotFound {
			t.Fatalf("err = %v, want not_found", err)
		}
	})
	t.Run("list delay honors cancellation", func(t *testing.T) {
		f := &FakeReader{ListDelay: 50 * time.Millisecond}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := f.List(ctx, core.Query{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
	t.Run("get unknown workflow is not_found", func(t *testing.T) {
		f := &FakeReader{Workflows: map[core.Ref]core.Workflow{}}
		_, err := f.Get(context.Background(), core.Ref{Namespace: "ns", Name: "nope"})
		if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrNotFound {
			t.Fatalf("err = %v, want not_found", err)
		}
	})
}

// Log streams preserve records, stop on callback errors, and honor cancellation.
func TestFakeReaderStreamLogs(t *testing.T) {
	t.Run("clean finite EOF returns nil", func(t *testing.T) {
		f := &FakeReader{StreamSequence: []core.LogRecord{
			{Content: "line-1"}, {Content: "line-2"}, {Content: "line-3"},
		}}
		var got []core.LogRecord
		err := f.StreamLogs(context.Background(), core.LogRequest{Container: "main"}, func(r core.LogRecord) error {
			got = append(got, r)
			return nil
		})
		if err != nil {
			t.Fatalf("StreamLogs = %v, want nil (clean EOF)", err)
		}
		if len(got) != 3 || got[0].Content != "line-1" || got[1].Content != "line-2" || got[2].Content != "line-3" || got[1].Container != "main" {
			t.Fatalf("records = %+v", got)
		}
	})
	t.Run("callback error stops stream", func(t *testing.T) {
		f := &FakeReader{StreamSequence: []core.LogRecord{
			{Content: "a"}, {Content: "b"}, {Content: "c"},
		}}
		var n int
		err := f.StreamLogs(context.Background(), core.LogRequest{}, func(core.LogRecord) error {
			n++
			if n == 2 {
				return errors.New("stop")
			}
			return nil
		})
		if err == nil || err.Error() != "stop" {
			t.Fatalf("err = %v, want callback error", err)
		}
		if n != 2 {
			t.Fatalf("callback ran %d times, want 2", n)
		}
	})
	t.Run("cancellation distinguishable from failure", func(t *testing.T) {
		f := &FakeReader{StreamDelay: time.Hour, StreamSequence: []core.LogRecord{{Content: "x"}, {Content: "y"}}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- f.StreamLogs(ctx, core.LogRequest{}, func(core.LogRecord) error { cancel(); return nil })
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("stream did not return after cancel")
		}
		if f.StreamCancels != 1 {
			t.Errorf("StreamCancels = %d, want 1", f.StreamCancels)
		}
	})
	t.Run("timestamps are stamped the way the API stamps them", func(t *testing.T) {
		at := time.Date(2026, 9, 7, 10, 0, 3, 137000000, time.UTC)
		f := &FakeReader{StreamSequence: []core.LogRecord{{Content: "hello", ReceivedAt: at}}}
		var got []string
		for _, ts := range []bool{false, true} {
			_ = f.StreamLogs(context.Background(), core.LogRequest{Timestamps: ts}, func(r core.LogRecord) error {
				got = append(got, r.Content)
				return nil
			})
		}
		if len(got) != 2 || got[0] != "hello" || got[1] != "2026-09-07T10:00:03.137000000Z hello" {
			t.Fatalf("contents = %q", got)
		}
		if f.StreamSequence[0].Content != "hello" {
			t.Fatal("stamping changed the stored record")
		}
	})

}

// The demo clock advances only by the requested duration.
func TestFakeClockAdvance(t *testing.T) {
	c := NewFakeClock(FixtureEpoch)
	if !c.Now().Equal(FixtureEpoch) {
		t.Fatalf("now = %v", c.Now())
	}
	c.Advance(90 * time.Second)
	if got, want := c.Now(), FixtureEpoch.Add(90*time.Second); !got.Equal(want) {
		t.Fatalf("now = %v, want %v", got, want)
	}
}

// Demo workflows have synthetic identities, valid graphs, and logs for pods that ran.
func TestDemoCarriesTheShapesTheViewsHandle(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	var suspended, stepGroups, retried, hooked, skipped, joins, noNodes int
	for _, wf := range f.Workflows {
		ref := wf.Summary.Ref
		if (ref.Namespace != DemoNamespace && ref.Namespace != DemoMLNamespace) || !strings.HasPrefix(ref.Name, "demo-") {
			t.Fatalf("demo identity = %+v; want demo namespace and name", ref)
		}
		if wf.Summary.Suspended {
			suspended++
		}
		if len(wf.Nodes) == 0 {
			noNodes++
		}
		parents := map[string]int{}
		for _, n := range wf.Nodes {
			for _, c := range n.Children {
				if _, ok := wf.Nodes[c]; !ok {
					t.Errorf("%s: node %s names missing child %s", wf.Summary.Ref.Name, n.ID, c)
				}
				parents[c]++
			}
			switch {
			case n.Type == "StepGroup":
				stepGroups++
			case n.Retried:
				retried++
			case n.Hooked:
				hooked++
			case n.Phase == "Skipped":
				skipped++
			}
			if n.PodName != "" && n.StartedAt != nil && len(f.PodLogs[n.PodName]) == 0 {
				t.Errorf("%s: pod %s ran but has no log", wf.Summary.Ref.Name, n.PodName)
			}
		}
		for _, c := range parents {
			if c > 1 {
				joins++
			}
		}
		if _, err := json.Marshal(wf.Resource); err != nil || !json.Valid(wf.Resource) {
			t.Errorf("%s: resource is not valid JSON", wf.Summary.Ref.Name)
		}
	}
	for what, n := range map[string]int{
		"suspended workflow": suspended, "step group": stepGroups, "retry attempt": retried,
		"exit handler": hooked, "skipped node": skipped, "DAG join": joins, "workflow without nodes": noNodes,
	} {
		if n == 0 {
			t.Errorf("demo has no %s", what)
		}
	}
}
