package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/journal"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/actions"
)

// fleet is a backend of several workflows that applies each action's
// effect the way a server does, and records every request it receives.
type fleet struct {
	*testkit.FakeReader
	// sent is every Execute call, in order.
	sent []core.ActionRequest
	// failSend makes Execute fail for these names, after "sending".
	failSend map[string]error
	// replaced makes Get answer for these names with a workflow of the same
	// name and a new UID, as after a delete and re-create.
	replaced map[string]bool
}

func (f *fleet) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	wf, err := f.FakeReader.Get(ctx, ref)
	if err == nil && f.replaced[ref.Name] {
		wf.Summary.Ref.UID = "replacement-" + ref.UID
	}
	return wf, err
}

func (f *fleet) Execute(_ context.Context, req core.ActionRequest) (core.ActionResult, error) {
	f.sent = append(f.sent, req)
	if err := f.failSend[req.Ref.Name]; err != nil {
		return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionUnknown}, err
	}
	wf, ok := f.Workflows[req.Ref]
	if !ok {
		return core.ActionResult{}, core.ErrNotFoundf("workflow %s not found", req.Ref.Name)
	}
	switch req.Action {
	case core.ActionStop, core.ActionTerminate:
		wf.Summary.Phase = "Failed"
	case core.ActionSuspend:
		wf.Summary.Suspended = true
	case core.ActionResume:
		wf.Summary.Suspended = false
	case core.ActionRetry:
		wf.Summary.Phase = "Running"
	case core.ActionDelete:
		delete(f.Workflows, req.Ref)
		return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionAccepted}, nil
	}
	f.Workflows[req.Ref] = wf
	ref := req.Ref
	return core.ActionResult{Action: req.Action, Target: req.Ref, Affected: &ref, Outcome: core.ActionAccepted, Workflow: &wf}, nil
}

func (f *fleet) sentNames() []string {
	var out []string
	for _, r := range f.sent {
		out = append(out, r.Ref.Name)
	}
	return out
}

// newFleet builds a root on the list route over running workflows with the
// given names, listed and fresh, with actions enabled.
func newFleet(t *testing.T, names ...string) (*Root, *fleet) {
	t.Helper()
	f := &fleet{FakeReader: &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{}}, failSend: map[string]error{}, replaced: map[string]bool{}}
	var items []core.Summary
	for i, name := range names {
		wf := testkit.SyntheticWorkflow("ns", name, "Running", testkit.FixtureEpoch.Add(-time.Duration(i)*time.Minute))
		f.Workflows[wf.Summary.Ref] = wf
		f.Order = append(f.Order, wf.Summary.Ref)
		items = append(items, wf.Summary)
	}
	m := NewRootWithOptions(f, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second, actions.Options{AllowActions: true, Server: "https://argo.test", Profile: "dev"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.handleListLoaded(listLoadedMsg{Page: core.Page{Items: items}})
	old, oldN := stopObservationInterval, stopObservationAttempts
	stopObservationInterval, stopObservationAttempts = time.Millisecond, 2
	t.Cleanup(func() { stopObservationInterval, stopObservationAttempts = old, oldN })
	return m, f
}

func key(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

// keys presses each key and returns the command of the last one.
func keys(m *Root, ks ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range ks {
		_, cmd = m.Update(key(k))
	}
	return cmd
}

// drive feeds the action commands back into the root until the chain
// ends. before, when set, runs ahead of each bulk step's delivery, so a
// test can change the world between two requests.
func drive(m *Root, cmd tea.Cmd, before func(step int)) {
	step := 0
	for i := 0; cmd != nil && i < 100; i++ {
		msg := cmd()
		switch msg.(type) {
		case bulkStepMsg:
			if before != nil {
				before(step)
			}
			step++
		case actions.BulkIntentMsg, actions.ActionIntentMsg, actionResultMsg:
		default:
			return
		}
		_, cmd = m.Update(msg)
	}
}

func outcomes(m *Root) map[string]core.ActionOutcome {
	out := map[string]core.ActionOutcome{}
	for _, it := range m.actionView.BulkItems() {
		out[it.Result.Target.Name] = it.Result.Outcome
	}
	return out
}

// Marked workflows are acted on one request at a time, in display order,
// each exactly once, and each is read back to its own outcome.
func TestBulkStopRunsOncePerTargetInOrder(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b", "wf-c")
	keys(m, "space", "j", "space", "j", "space")
	if m.listView.MarkCount() != 3 {
		t.Fatalf("marks = %d", m.listView.MarkCount())
	}
	keys(m, "a")
	if m.actionView == nil || !m.actionView.Bulk() || m.actionView.State() != actions.StateMenu {
		t.Fatalf("a did not open the bulk menu")
	}
	drive(m, keys(m, "s", "y"), nil)
	rows := m.listView.Rows()
	want := []string{rows[0].Ref.Name, rows[1].Ref.Name, rows[2].Ref.Name}
	if got := f.sentNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sent %v, want %v", got, want)
	}
	if m.actionView.State() != actions.StateOutcome {
		t.Fatalf("state = %v, want the outcome pane", m.actionView.State())
	}
	for name, o := range outcomes(m) {
		if o != core.ActionConfirmed {
			t.Errorf("%s: %s, want confirmed", name, o)
		}
	}
	view := screen(m)
	if !strings.Contains(view, "total: 3 confirmed, 0 accepted, 0 refused, 0 unknown (3 workflows)") ||
		!strings.Contains(view, "CONFIRMED  ns/wf-b — phase Failed") {
		t.Fatalf("outcome pane:\n%s", view)
	}
}

// A target whose identity changed since it was marked is refused without
// a request, and the other targets are acted on regardless.
func TestBulkRefusesAReplacedTargetAndContinues(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b", "wf-c")
	f.replaced["wf-b"] = true
	keys(m, "space", "j", "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), nil)
	for _, name := range f.sentNames() {
		if name == "wf-b" {
			t.Fatal("a request was sent for the replaced workflow")
		}
	}
	got := outcomes(m)
	if got["wf-b"] != core.ActionRefused || got["wf-a"] != core.ActionConfirmed || got["wf-c"] != core.ActionConfirmed {
		t.Fatalf("outcomes = %v", got)
	}
	if !strings.Contains(screen(m), "REFUSED    ns/wf-b — not sent: workflow UID mismatch") {
		t.Fatalf("the refusal reason is not shown:\n%s", screen(m))
	}
}

// A target whose send failed is UNKNOWN and is never sent again; the run
// goes on to the next target.
func TestBulkNeverResendsAnUnknownTarget(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	f.failSend["wf-a"] = errors.New("connection reset after send")
	keys(m, "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), nil)
	counts := map[string]int{}
	for _, name := range f.sentNames() {
		counts[name]++
	}
	if counts["wf-a"] != 1 || counts["wf-b"] != 1 {
		t.Fatalf("sends = %v, want exactly one each", counts)
	}
	got := outcomes(m)
	if got["wf-a"] != core.ActionUnknown || got["wf-b"] != core.ActionConfirmed {
		t.Fatalf("outcomes = %v", got)
	}
}

// A target that no longer applies when its turn comes is refused rather
// than sent into an error Argo is certain to return.
func TestBulkRefusesATargetThatNoLongerApplies(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	keys(m, "space", "j", "space", "a")
	cmd := keys(m, "s", "y")
	// wf-b finishes on its own while the run is in progress.
	for ref, wf := range f.Workflows {
		if ref.Name == "wf-b" {
			wf.Summary.Phase = "Succeeded"
			f.Workflows[ref] = wf
		}
	}
	drive(m, cmd, nil)
	if got := outcomes(m); got["wf-b"] != core.ActionRefused {
		t.Fatalf("outcomes = %v", got)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %v, want only wf-a", f.sentNames())
	}
}

// Data that goes stale between two requests stops the run: the targets not
// yet sent are refused, and nothing more is sent.
func TestBulkStopsWhenTheDataGoesStale(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b", "wf-c")
	keys(m, "space", "j", "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), func(step int) {
		if step == 0 {
			m.SetConnectionState(false, "")
		}
	})
	if len(f.sent) != 1 {
		t.Fatalf("sent %v after the connection was lost", f.sentNames())
	}
	items := m.actionView.BulkItems()
	if len(items) != 3 || items[1].Result.Outcome != core.ActionRefused || items[2].Result.Outcome != core.ActionRefused {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(items[1].Reason, "connection lost") {
		t.Fatalf("reason = %q", items[1].Reason)
	}
}

// Esc during a run stops it after the request in flight.
func TestBulkEscStopsAfterTheRequestInFlight(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b", "wf-c")
	keys(m, "space", "j", "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), func(step int) {
		if step == 0 {
			keys(m, "esc")
		}
	})
	if len(f.sent) != 1 {
		t.Fatalf("sent %v after esc", f.sentNames())
	}
	if got := outcomes(m); got[f.sent[0].Ref.Name] != core.ActionConfirmed || len(got) != 3 {
		t.Fatalf("outcomes = %v", got)
	}
}

// Closing the bulk outcome clears the marks and refreshes the list.
func TestClosingTheBulkOutcomeRefreshesAndClearsTheMarks(t *testing.T) {
	m, _ := newFleet(t, "wf-a", "wf-b")
	keys(m, "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), nil)
	listsBefore := m.deps.reader.(*fleet).ListCalls
	cmd := keys(m, "esc")
	if m.actionView.State() != actions.StateIdle || m.listView.MarkCount() != 0 {
		t.Fatalf("state=%v marks=%d", m.actionView.State(), m.listView.MarkCount())
	}
	if cmd == nil || !m.listState.loading {
		t.Fatal("closing the outcome did not refresh the list")
	}
	runCmd(cmd)
	if m.deps.reader.(*fleet).ListCalls == listsBefore {
		t.Fatal("no list request followed the close")
	}
}

// a on the list opens the single-workflow menu for the selected row, and
// stale data blocks it exactly as it does on the detail route.
func TestListActionKeyActsOnTheSelectionBehindTheFreshnessGate(t *testing.T) {
	m, _ := newFleet(t, "wf-a", "wf-b")
	keys(m, "j", "a")
	if m.actionView.Bulk() || m.actionView.Ref() != m.listView.SelectedRef() || m.actionView.State() != actions.StateMenu {
		t.Fatalf("menu: bulk=%v ref=%v state=%v", m.actionView.Bulk(), m.actionView.Ref(), m.actionView.State())
	}
	keys(m, "esc")
	m.SetConnectionState(false, "")
	keys(m, "a")
	if m.actionView.State() != actions.StateUnavailable || !strings.Contains(m.actionView.View().Content, "connection lost") {
		t.Fatalf("stale list action: state=%v view=%s", m.actionView.State(), m.actionView.View().Content)
	}
}

// A namespace switch drops the marks: none of them can be in the next
// snapshot, and a bulk action must never reach across the switch.
func TestANamespaceSwitchClearsTheMarks(t *testing.T) {
	m, _ := newFleet(t, "wf-a", "wf-b")
	keys(m, "space")
	m.switchNamespace("other")
	if m.listView.MarkCount() != 0 {
		t.Fatalf("marks = %d after the switch", m.listView.MarkCount())
	}
}

// The demo refuses every write, bulk or single, and writes no journal.
func TestTheDemoRefusesBulkWritesAndJournalsNothing(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	m.actionOpts = actions.Options{AllowActions: true, Demo: true}
	path := filepath.Join(t.TempDir(), "actions.jsonl")
	m.SetJournal(journal.New(path))
	keys(m, "space", "j", "space", "a")
	if m.actionView.State() != actions.StateUnavailable || !strings.Contains(m.actionView.View().Content, "demo mode") {
		t.Fatalf("demo bulk menu: state=%v", m.actionView.State())
	}
	reqs := []core.ActionRequest{{Ref: m.listView.Marked()[0].Ref, Action: core.ActionStop, Confirmation: core.Confirmation{Confirmed: true}}}
	if cmd := m.startBulk(reqs); cmd != nil {
		t.Fatal("the demo started a bulk action")
	}
	if cmd := m.startAction(reqs[0]); cmd != nil {
		t.Fatal("the demo started an action")
	}
	if len(f.sent) != 0 {
		t.Fatalf("sent %v in the demo", f.sentNames())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the demo wrote a journal: %v", err)
	}
}

func readJournal(t *testing.T, path string) []journal.Entry {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer fh.Close()
	var out []journal.Entry
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		var e journal.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("journal line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

// Every attempt is journaled with its settled outcome, a refused one
// included, under the session's profile and server.
func TestEveryAttemptIsJournaled(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	f.replaced["wf-b"] = true
	path := filepath.Join(t.TempDir(), "actions.jsonl")
	m.SetJournal(journal.New(path))
	keys(m, "space", "j", "space", "a")
	drive(m, keys(m, "s", "y"), nil)
	entries := readJournal(t, path)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	byName := map[string]journal.Entry{}
	for _, e := range entries {
		byName[e.Name] = e
		if e.Profile != "dev" || e.Server != "https://argo.test" || e.Namespace != "ns" || e.Verb != "stop" || e.UID == "" || e.Time.IsZero() {
			t.Fatalf("entry = %+v", e)
		}
	}
	if byName["wf-a"].Outcome != "confirmed" || byName["wf-b"].Outcome != "refused" || !strings.Contains(byName["wf-b"].Error, "UID mismatch") {
		t.Fatalf("entries = %+v", entries)
	}
}

// A journal that cannot be written changes nothing about the action and is
// reported once, on the footer.
func TestAJournalFailureIsIsolatedAndReportedOnce(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	dir := filepath.Join(t.TempDir(), "actions.jsonl")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m.SetJournal(journal.New(dir))

	keys(m, "space", "a")
	drive(m, keys(m, "s", "y"), nil)
	if len(f.sent) != 1 {
		t.Fatalf("sent %v", f.sentNames())
	}
	if !strings.Contains(m.flash, "action journal not written") || !strings.Contains(m.flash, "stop confirmed") {
		t.Fatalf("flash = %q", m.flash)
	}
	keys(m, "j", "a")
	drive(m, keys(m, "s", "y"), nil)
	if len(f.sent) != 2 {
		t.Fatalf("the second action was not sent: %v", f.sentNames())
	}
	if strings.Contains(m.flash, "journal") {
		t.Fatalf("the journal failure was reported twice: %q", m.flash)
	}
}

// Suspend settles when the workflow reports itself suspended.
func TestSuspendSettlesWhenTheWorkflowReportsSuspended(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{ref: {Summary: core.Summary{Ref: ref, Phase: "Running", Suspended: true}}}}
	if _, settled, err := readBack(context.Background(), f, ref, core.ActionSuspend); err != nil || !settled {
		t.Fatalf("suspended workflow: settled=%v err=%v", settled, err)
	}
	f.Workflows[ref] = core.Workflow{Summary: core.Summary{Ref: ref, Phase: "Running"}}
	if _, settled, _ := readBack(context.Background(), f, ref, core.ActionSuspend); settled {
		t.Fatal("a workflow that does not report suspended was read back as suspended")
	}
}

// Terminate settles when the workflow reaches a terminal phase.
func TestTerminateSettlesOnATerminalPhase(t *testing.T) {
	m, r := newStopRoot(t, "Running", "Running", "Failed")
	req := core.ActionRequest{Ref: workflowFixture("wf").Summary.Ref, Action: core.ActionTerminate, Confirmation: core.Confirmation{Confirmed: true, TypedName: "wf"}}
	got := runCmd(m.startAction(req))[0].(actionResultMsg).Result
	if got.Outcome != core.ActionConfirmed || r.execCalls != 1 {
		t.Fatalf("outcome=%s execs=%d", got.Outcome, r.execCalls)
	}
	m2, _ := newStopRoot(t, "Running")
	got = runCmd(m2.startAction(req))[0].(actionResultMsg).Result
	if got.Outcome != core.ActionAccepted {
		t.Fatalf("still running: outcome=%s, want accepted", got.Outcome)
	}
}

// Delete settles when a read answers not found. A workflow that is still
// readable (an archived copy, or a finalizer holding it) leaves the delete
// ACCEPTED, never UNKNOWN: the server did apply it.
func TestDeleteSettlesWhenTheWorkflowIsNotFound(t *testing.T) {
	old, oldN := stopObservationInterval, stopObservationAttempts
	stopObservationInterval, stopObservationAttempts = time.Millisecond, 2
	t.Cleanup(func() { stopObservationInterval, stopObservationAttempts = old, oldN })

	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"}
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	if _, settled, err := readBack(context.Background(), f, ref, core.ActionDelete); err != nil || !settled {
		t.Fatalf("gone workflow: settled=%v err=%v", settled, err)
	}
	f.Workflows[ref] = core.Workflow{Summary: core.Summary{Ref: ref, Phase: "Succeeded"}}
	if _, settled, err := readBack(context.Background(), f, ref, core.ActionDelete); err != nil || settled {
		t.Fatalf("readable workflow: settled=%v err=%v", settled, err)
	}

	m, fl := newFleet(t, "wf-a")
	keys(m, "a", "d", "y")
	drive(m, keys(m, "D"), nil)
	if len(fl.sent) != 1 || fl.sent[0].Action != core.ActionDelete || !fl.sent[0].Confirmation.Final {
		t.Fatalf("sent = %+v", fl.sent)
	}
	if !strings.Contains(m.flash, "delete confirmed") {
		t.Fatalf("flash = %q", m.flash)
	}
}

// Deleting the workflow the detail route shows returns to the list: there
// is no detail left to show.
func TestDeletingFromTheDetailReturnsToTheList(t *testing.T) {
	m, f := newFleet(t, "wf-a", "wf-b")
	m.Update(OpenWorkflowMsg{Ref: m.listView.SelectedRef()})
	if m.route != RouteDetail {
		t.Fatalf("route = %v", m.route)
	}
	keys(m, "a", "d", "y")
	drive(m, keys(m, "D"), nil)
	if len(f.sent) != 1 || m.route != RouteList {
		t.Fatalf("sent=%v route=%v, want one delete and the list", f.sentNames(), m.route)
	}
}
