package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

func TestFakeReaderListFilterAndOrder(t *testing.T) {
	f := &FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	w1 := SyntheticWorkflow("ns-a", "zeta", "Running", FixtureEpoch)
	w2 := SyntheticWorkflow("ns-a", "alpha", "Succeeded", FixtureEpoch)
	w3 := SyntheticWorkflow("ns-b", "other", "Failed", FixtureEpoch)
	for _, w := range []core.Workflow{w1, w2, w3} {
		f.Workflows[w.Summary.Ref] = w
	}
	page, err := f.List(context.Background(), core.Query{Namespace: "ns-a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2 (ns filter)", len(page.Items))
	}
	if page.Items[0].Ref.Name != "alpha" || page.Items[1].Ref.Name != "zeta" {
		t.Errorf("order = %q,%q, want alpha,zeta", page.Items[0].Ref.Name, page.Items[1].Ref.Name)
	}
	if page.Continue != "" {
		t.Errorf("unexpected continuation %q", page.Continue)
	}
}

func TestFakeReaderPaginationOpaqueContinue(t *testing.T) {
	f := &FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	want := []core.Workflow{
		SyntheticWorkflow("ns", "wf-1", "Running", FixtureEpoch),
		SyntheticWorkflow("ns", "wf-2", "Failed", FixtureEpoch),
		SyntheticWorkflow("ns", "wf-3", "Succeeded", FixtureEpoch),
	}
	for _, w := range want {
		f.Workflows[w.Summary.Ref] = w
	}
	p1, err := f.List(context.Background(), core.Query{Namespace: "ns", Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(p1.Items) != 2 || p1.Continue == "" {
		t.Fatalf("page1 = %d items, continue %q", len(p1.Items), p1.Continue)
	}
	p2, err := f.List(context.Background(), core.Query{Namespace: "ns", Limit: 2, Continue: p1.Continue})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(p2.Items) != 1 || p2.Items[0].Ref.Name != "wf-3" {
		t.Fatalf("page2 items = %+v", p2.Items)
	}
	if p2.Continue != "" {
		t.Errorf("page2 must terminate the collection, got continue %q", p2.Continue)
	}
	// The token is opaque to callers: passing garbage must not panic; the
	// fake starts from the beginning in that case.
	pBad, err := f.List(context.Background(), core.Query{Namespace: "ns", Limit: 2, Continue: "garbage"})
	if err != nil {
		t.Fatalf("page-garbage: %v", err)
	}
	if len(pBad.Items) != 2 {
		t.Errorf("garbage continue: items = %d, want 2 (restart from head)", len(pBad.Items))
	}
}

func TestFakeReaderInjectableErrorsAndDelays(t *testing.T) {
	t.Run("list error", func(t *testing.T) {
		f := &FakeReader{ListErr: core.ErrForbiddenf("list denied in ns")}
		_, err := f.List(context.Background(), core.Query{Namespace: "ns"})
		if !isKind(err, core.ErrForbidden) {
			t.Fatalf("err = %v, want forbidden", err)
		}
	})
	t.Run("get error", func(t *testing.T) {
		f := &FakeReader{GetErr: core.ErrNotFoundf("gone")}
		_, err := f.Get(context.Background(), core.Ref{Namespace: "ns", Name: "wf"})
		if !isKind(err, core.ErrNotFound) {
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
		if !isKind(err, core.ErrNotFound) {
			t.Fatalf("err = %v, want not_found", err)
		}
	})
}

func TestFakeReaderStreamLogs(t *testing.T) {
	t.Run("clean finite EOF returns nil", func(t *testing.T) {
		f := &FakeReader{StreamSequence: []core.LogRecord{
			{Content: "line-1"}, {Content: "line-2"},
		}}
		var got []core.LogRecord
		err := f.StreamLogs(context.Background(), core.LogRequest{Container: "main"}, func(r core.LogRecord) error {
			got = append(got, r)
			return nil
		})
		if err != nil {
			t.Fatalf("StreamLogs = %v, want nil (clean EOF)", err)
		}
		if len(got) != 2 || got[0].Content != "line-1" || got[1].Container != "main" {
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
			done <- f.StreamLogs(ctx, core.LogRequest{}, func(core.LogRecord) error { return nil })
		}()
		time.Sleep(10 * time.Millisecond)
		cancel()
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
	t.Run("serial callback ordering", func(t *testing.T) {
		f := &FakeReader{StreamSequence: []core.LogRecord{{Content: "1"}, {Content: "2"}, {Content: "3"}}}
		var order []string
		_ = f.StreamLogs(context.Background(), core.LogRequest{}, func(r core.LogRecord) error {
			order = append(order, r.Content)
			return nil
		})
		if len(order) != 3 || order[0] != "1" || order[2] != "3" {
			t.Fatalf("order = %v", order)
		}
	})
}

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

func TestDemoDataIsSyntheticAndPaginates(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	for ref := range f.Workflows {
		if ref.Namespace != "demo" {
			t.Fatalf("demo namespace drift: %v", ref)
		}
		if len(ref.Name) < 5 || ref.Name[:5] != "demo-" {
			t.Fatalf("demo workflow name %q lacks demo- prefix", ref.Name)
		}
	}
	p1, err := f.List(context.Background(), core.Query{Namespace: "demo", Limit: 3})
	if err != nil {
		t.Fatalf("demo list: %v", err)
	}
	if p1.Continue == "" {
		t.Fatal("demo dataset should paginate with default limit")
	}
}

func isKind(err error, kind core.ErrorKind) bool {
	ae := core.AsAPIError(err)
	return ae != nil && ae.Kind == kind
}

// The demo node maps follow the controller's shapes, and every pod that ran
// has its own log. Views are checked against the demo, so a demo that drifts
// from those shapes would hide the cases they have to handle.
func TestDemoCarriesTheShapesTheViewsHandle(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	var suspended, stepGroups, retried, hooked, skipped, joins, noNodes int
	for _, wf := range f.Workflows {
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
