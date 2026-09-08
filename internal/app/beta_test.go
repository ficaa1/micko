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

type betaReader struct {
	*testkit.FakeReader
	execCalls int
	execErr   error
}

func (b *betaReader) Execute(_ context.Context, req core.ActionRequest) (core.ActionResult, error) {
	b.execCalls++
	if b.execErr != nil {
		return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}, b.execErr
	}
	ref := req.Ref
	return core.ActionResult{Action: req.Action, Target: req.Ref, Affected: &ref, Outcome: core.ActionConfirmed}, nil
}

func TestWatchEventUpdatesByUIDAndBookmarksDoNothing(t *testing.T) {
	wf := workflowFixture("wf")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := testRoot(t, f)
	m.listState.items = []core.Summary{wf.Summary}
	changed := wf.Summary
	changed.Phase = "Succeeded"
	changed.ResourceVersion = "rv-2"
	m.Update(watchEventMsg{genStamp: genStamp{}, Event: core.WatchEvent{Type: core.WatchModified, Summary: changed, ResourceVersion: "rv-2"}})
	if len(m.listState.items) != 1 || m.listState.items[0].Phase != "Succeeded" || m.watchRV != "rv-2" {
		t.Fatalf("watch update not applied: items=%+v rv=%q", m.listState.items, m.watchRV)
	}
	m.Update(watchEventMsg{genStamp: genStamp{}, Event: core.WatchEvent{Type: core.WatchBookmark, ResourceVersion: "rv-3"}})
	if m.watchRV != "rv-3" || len(m.listState.items) != 1 {
		t.Fatalf("bookmark changed snapshot: items=%d rv=%q", len(m.listState.items), m.watchRV)
	}
}

func TestActionExecutesOnceAndRequiresReadBack(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}}
	msg := runCmd(m.startAction(req))
	if len(msg) != 1 {
		t.Fatalf("action command messages=%d", len(msg))
	}
	result := msg[0].(actionResultMsg)
	if result.Result.Outcome != core.ActionConfirmed || result.Err != nil {
		t.Fatalf("action result=%+v err=%v", result.Result, result.Err)
	}
	if b.execCalls != 1 {
		t.Fatalf("execute calls=%d, want exactly one", b.execCalls)
	}
	if b.GetCalls != 2 {
		t.Fatalf("GET calls=%d, want preflight + read-back", b.GetCalls)
	}
}

func TestAmbiguousActionNeverRetries(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{
		FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}},
		execErr:    errors.New("connection reset after send"),
	}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionTerminate, Confirmation: core.Confirmation{Confirmed: true, TypedName: wf.Summary.Ref.Name}}
	msg := runCmd(m.startAction(req))
	result := msg[0].(actionResultMsg)
	if result.Result.Outcome != core.ActionUnknown || result.Err == nil {
		t.Fatalf("ambiguous outcome=%+v err=%v", result.Result, result.Err)
	}
	if b.execCalls != 1 {
		t.Fatalf("ambiguous execute calls=%d, want one", b.execCalls)
	}
}
