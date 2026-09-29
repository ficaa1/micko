package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
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
	// A resume takes effect at once, as it does on a server: the read-back
	// that follows sees the gate closed.
	if wf, ok := b.Workflows[req.Ref]; ok && req.Action == core.ActionResume {
		wf.Summary.Suspended = false
		b.Workflows[req.Ref] = wf
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
	wf.Summary.Phase = "Failed"
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	req := core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionResubmit, Confirmation: core.Confirmation{Confirmed: true}}
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

func TestConnectionLossDisablesActionsUntilFreshSnapshot(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.SetConnectionState(false, "http://127.0.0.1:43127")
	if m.connectionReady || m.connectionFresh {
		t.Fatal("loss left gate open")
	}
	if cmd := m.startAction(core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionResume, Confirmation: core.Confirmation{Confirmed: true}}); cmd != nil {
		t.Fatal("action started while disconnected")
	}
	m.SetConnectionState(true, "http://127.0.0.1:43128")
	if m.connectionFresh {
		t.Fatal("reconnect enabled actions before fresh data")
	}
	m.handleListLoaded(listLoadedMsg{genStamp: genStamp{}, Page: core.Page{Items: []core.Summary{wf.Summary}}})
	if !m.connectionFresh {
		t.Fatal("fresh snapshot did not re-enable actions")
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

func TestRootKeyboardActionReachesExecutorAndRendersOutcome(t *testing.T) {
	wf := workflowFixture("wf")
	wf.Summary.Suspended = true
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.route = RouteDetail
	m.selection = wf.Summary.Ref
	m.detailState = detailState{ref: wf.Summary.Ref, workflow: wf}

	_, _ = m.Update(tea.KeyPressMsg{Text: "a"})
	_, _ = m.Update(tea.KeyPressMsg{Text: "u"}) // resume
	_, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	if cmd == nil {
		t.Fatal("confirmation did not emit an action intent command")
	}
	intent := cmd()
	if _, ok := intent.(actions.ActionIntentMsg); !ok {
		t.Fatalf("intent type = %T, want actions.ActionIntentMsg", intent)
	}
	_, cmd = m.Update(intent)
	if cmd == nil {
		t.Fatal("root did not start action execution")
	}
	result := cmd()
	_, _ = m.Update(result)
	if b.execCalls != 1 {
		t.Fatalf("execute calls = %d, want 1", b.execCalls)
	}
	// The pane hands the screen back by itself and reports on the footer of
	// the route behind it. Waiting for an Esc left the reader one press away
	// from the workflow they had just changed.
	if m.actionView.State() != actions.StateIdle {
		t.Fatalf("action pane stayed open: state=%v", m.actionView.State())
	}
	if m.route != RouteDetail {
		t.Fatalf("route = %v, want the detail view back", m.route)
	}
	if !strings.Contains(screen(m), "resume confirmed") {
		t.Fatalf("view did not report the outcome: %s", screen(m))
	}
}

// An UNKNOWN outcome is the one a reader must see. It stays on the screen.
func TestUnknownOutcomeKeepsThePaneOpen(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.route = RouteDetail
	m.selection = wf.Summary.Ref
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
	m.actionView.Open(core.ActionStop)
	m.actionView.Confirm()
	m.handleActionResult(actionResultMsg{
		genStamp: genStamp{Attempt: m.actionAttempt},
		Result:   core.ActionResult{Action: core.ActionStop, Outcome: core.ActionUnknown},
	})
	if m.actionView.State() != actions.StateOutcome {
		t.Fatalf("unknown outcome was dismissed: state=%v", m.actionView.State())
	}
}

func TestRootCtrlCQuitsWhileActionModalIsOpen(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.route = RouteDetail
	m.actionView = actions.NewWithOptions(core.Ref{Name: "wf", Namespace: "ns", UID: "uid"}, actions.Options{AllowActions: true})
	m.actionView.OpenMenu()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl-c did not return quit command")
	}
	if !m.quitting {
		t.Fatal("ctrl-c did not mark root quitting")
	}
}

func TestWatchAuthFailureStopsAutomaticRecovery(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	_, cmd := m.Update(watchDoneMsg{genStamp: genStamp{}, Err: core.NewWatchError(core.WatchEnded, "unauthorized", "", core.NewAPIError(core.ErrUnauthenticated, 401, "unauthorized"))})
	if cmd != nil {
		t.Fatal("auth failure scheduled automatic recovery")
	}
	if !strings.Contains(m.watchMode, "authentication") {
		t.Fatalf("watch mode = %q, want authentication terminal state", m.watchMode)
	}
}

func TestWatchRateLimitDoesNotRelistImmediately(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	_, cmd := m.Update(watchDoneMsg{genStamp: genStamp{}, Err: core.NewWatchError(core.WatchEnded, "slow down", "", core.NewAPIError(core.ErrRateLimited, 429, "slow down"))})
	if cmd == nil {
		t.Fatal("rate limit did not schedule delayed recovery")
	}
	if !strings.Contains(m.watchMode, "rate limited") {
		t.Fatalf("watch mode = %q, want rate-limited state", m.watchMode)
	}
}

func TestActionLateResultCannotOverwriteLaterAttempt(t *testing.T) {
	wf := workflowFixture("wf")
	b := &betaReader{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}}
	m := NewRootWithOptions(b, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true})
	m.selection = wf.Summary.Ref
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
	firstMsg := m.startAction(core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}})().(actionResultMsg)
	m.clearInflight("action", uint64(firstMsg.Attempt))
	m.actionView = actions.NewWithOptions(wf.Summary.Ref, actions.Options{AllowActions: true})
	secondMsg := m.startAction(core.ActionRequest{Ref: wf.Summary.Ref, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}})().(actionResultMsg)
	if firstMsg.Attempt == 0 || firstMsg.Attempt == secondMsg.Attempt {
		t.Fatalf("attempt identities = %d and %d", firstMsg.Attempt, secondMsg.Attempt)
	}
	m.actionView.Open(core.ActionRetry)
	wantState := m.actionView.State()
	_, _ = m.Update(firstMsg)
	if m.actionView.State() != wantState {
		t.Fatalf("late result changed current action state to %v", m.actionView.State())
	}
}

func TestTerminalWatchStateSuppressesQueuedTick(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.watchMode = "authentication/permission required"
	_, cmd := m.Update(tickMsg{})
	if cmd != nil {
		t.Fatal("terminal auth state allowed queued polling tick")
	}
}

func TestStaleWatchAttemptEventIsDiscarded(t *testing.T) {
	wf := workflowFixture("wf")
	m := testRoot(t, &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}})
	m.listState.items = []core.Summary{wf.Summary}
	m.watchAttempt = 2
	changed := wf.Summary
	changed.Phase = "Failed"
	m.Update(watchEventMsg{genStamp: genStamp{Attempt: 1}, Event: core.WatchEvent{Type: core.WatchModified, Summary: changed}})
	if m.listState.items[0].Phase != "Running" {
		t.Fatalf("stale watch event applied: %s", m.listState.items[0].Phase)
	}
}

func TestWatchRetryDelayIsExponentialAndCapped(t *testing.T) {
	if got := watchRetryDelay(1, nil); got < time.Second || got > 2*time.Second {
		t.Fatalf("first retry delay = %v", got)
	}
	if got := watchRetryDelay(5, nil); got < 16*time.Second || got > 31*time.Second {
		t.Fatalf("fifth retry delay = %v", got)
	}
	wait := 45 * time.Second
	if got := watchRetryDelay(1, &wait); got < wait {
		t.Fatalf("retry-after lower bound shortened: %v", got)
	}
}

func TestActionContextRendersServerProfileAndPhase(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	m := actions.NewWithOptions(ref, actions.Options{AllowActions: true, Server: "https://argo.test", Profile: "dev", Phase: "Running"})
	m.Open(core.ActionRetry)
	if view := m.View().Content; !strings.Contains(view, "server: https://argo.test") || !strings.Contains(view, "profile: dev") || !strings.Contains(view, "phase: Running") {
		t.Fatalf("context missing from action view: %s", view)
	}
}

func TestTerminalWatchStateSuppressesQueuedListRecovery(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.watchMode = "authentication/permission required"
	_, cmd := m.Update(listLoadedMsg{Page: core.Page{Items: []core.Summary{{Ref: core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}}}}})
	if cmd != nil {
		t.Fatal("terminal auth state allowed queued list to restart recovery")
	}
}

func TestRefreshClearsTerminalWatchState(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.watchMode = "authentication/permission required"
	// KeyPressMsg "r" is routed into the list child, which answers with a
	// RefreshListMsg intent command; the root converts that intent into the
	// refresh effect. Drive the same round-trip the Tea runtime
	// would: execute the command, feed the message back into Update.
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "r"})
	m = updated.(*Root)
	if cmd == nil {
		t.Fatal("r did not produce a refresh intent command")
	}
	for _, msg := range runCmd(cmd) {
		next, cmd := m.Update(msg)
		m = next.(*Root)
		if cmd == nil {
			t.Fatal("refresh intent command missing")
		}
	}
	if !m.listState.loading {
		t.Fatal("refresh did not start a new list")
	}
	if m.watchMode != "" {
		t.Fatalf("refresh retained terminal watch state: %q", m.watchMode)
	}
}

func TestStaleWatchRetryCannotStartReplacement(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.watchMode = "rate limited"
	m.watchAttempt = 2
	_, cmd := m.Update(watchRetryMsg{genStamp: genStamp{Attempt: 1}})
	if cmd != nil {
		t.Fatal("stale retry started a replacement watch")
	}
}
