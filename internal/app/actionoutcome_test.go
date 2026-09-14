package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
	"argo-tui/internal/ui/actions"
)

// refusingReader fails the preflight read, so no action is ever sent.
type refusingReader struct{ *testkit.FakeReader }

func (r refusingReader) Get(context.Context, core.Ref) (core.Workflow, error) {
	return core.Workflow{}, errors.New("preflight read failed")
}

func (r refusingReader) Execute(context.Context, core.ActionRequest) (core.ActionResult, error) {
	panic("the action was sent even though the preflight failed")
}

// A check that fails before anything is sent changed nothing on the server.
// Reporting it as UNKNOWN put a modal pane in front of the reader telling
// them to go and inspect a cluster that never heard of the request.
func TestAFailedPreflightIsRefusedNotUnknown(t *testing.T) {
	wf := workflowFixture("wf-1")
	base := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := NewRootWithOptions(refusingReader{base}, testkit.NewFakeClock(testkit.FixtureEpoch), "ns",
		time.Second, actions.Options{AllowActions: true})
	m.selection = wf.Summary.Ref
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})

	cmd := m.startAction(core.ActionRequest{
		Ref:          wf.Summary.Ref,
		Action:       core.ActionRetry,
		Confirmation: core.Confirmation{Confirmed: true},
	})
	msg, ok := cmd().(actionResultMsg)
	if !ok {
		t.Fatal("the action produced no result")
	}
	if msg.Result.Outcome != core.ActionRefused {
		t.Fatalf("outcome = %q, want refused", msg.Result.Outcome)
	}
}

// Confirmed means the expected end state was observed, not merely that the
// server answered. A resume that leaves the gate open is not confirmed.
func TestAResumeIsConfirmedOnlyWhenTheGateIsClosed(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	still := core.Workflow{Summary: core.Summary{Ref: ref, Phase: "Running", Suspended: true}}
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{ref: still}}

	_, settled, err := readBack(context.Background(), f, ref, core.ActionResume)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if settled {
		t.Fatal("a workflow still holding an open gate was reported as resumed")
	}

	f.Workflows[ref] = core.Workflow{Summary: core.Summary{Ref: ref, Phase: "Running"}}
	if _, settled, err = readBack(context.Background(), f, ref, core.ActionResume); err != nil || !settled {
		t.Fatalf("a resumed workflow was not reported as settled (settled=%v, err=%v)", settled, err)
	}
}

// A retry that leaves the workflow in its terminal phase has not been
// observed to take effect either.
func TestARetryIsConfirmedOnlyWhenTheWorkflowLeavesItsTerminalPhase(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{
		ref: {Summary: core.Summary{Ref: ref, Phase: "Failed"}},
	}}
	if _, settled, _ := readBack(context.Background(), f, ref, core.ActionRetry); settled {
		t.Fatal("a workflow still in its failed phase was reported as retried")
	}

	f.Workflows[ref] = core.Workflow{Summary: core.Summary{Ref: ref, Phase: "Running"}}
	if _, settled, _ := readBack(context.Background(), f, ref, core.ActionRetry); !settled {
		t.Fatal("a running workflow was not reported as retried")
	}
}
