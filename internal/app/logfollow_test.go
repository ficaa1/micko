package app

import (
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// TestOpenLogsRequestsFollow asserts the log stream asks the server to keep
// the connection open. Without Follow the server closes at the current end
// of the log, so a running workflow stops updating until the reader leaves
// the view and returns.
func TestOpenLogsRequestsFollow(t *testing.T) {
	got := make(chan core.LogRequest, 1)
	f := &testkit.FakeReader{
		Workflows: map[core.Ref]core.Workflow{},
		StreamHook: func(req core.LogRequest) error {
			select {
			case got <- req:
			default:
			}
			return nil
		},
	}
	wf := workflowFixture("wf-1")
	f.Workflows[wf.Summary.Ref] = wf

	updated, cmd := testRoot(t, f).Update(OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"})
	if root := updated.(*Root); root.route != RouteLogs {
		t.Fatalf("route = %v, want logs", root.route)
	}
	if cmd == nil {
		t.Fatal("OpenLogsMsg returned no command")
	}
	go cmd()

	select {
	case req := <-got:
		if !req.Follow {
			t.Error("LogRequest.Follow = false, want true")
		}
		if req.Container != "main" {
			t.Errorf("Container = %q, want main", req.Container)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream never started")
	}
}
