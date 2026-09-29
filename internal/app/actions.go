package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/journal"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/shared"
)

type actionResultMsg struct {
	genStamp
	Result core.ActionResult
	Err    error
	// JournalErr is a failed journal append. It is reported, never acted
	// on: the action it describes is already finished.
	JournalErr error
}

// bulkStepMsg is the result of one request of a bulk action. Index is the
// position of the request in the run, so a reply can only ever retire the
// request it belongs to.
type bulkStepMsg struct {
	genStamp
	Index      int
	Result     core.ActionResult
	Err        error
	JournalErr error
}

// bulkRun is the state of a bulk action in progress: the confirmed
// requests, the results so far, and the index of the next request. The
// index only moves forward, which is what guarantees no request is ever
// sent twice.
type bulkRun struct {
	reqs  []core.ActionRequest
	items []actions.BulkItem
	next  int
}

// SetJournal installs the action journal. Nil turns it off. The demo never
// gets one: it cannot write, so it has nothing to record.
func (m *Root) SetJournal(j *journal.Journal) { m.deps.journal = j }

// actionsPermitted reports whether a mutation may start right now: the
// session opted in, is not read-only or the demo, has an actioner, and
// holds fresh data from a live connection.
func (m *Root) actionsPermitted() bool {
	return m.actionOpts.AllowActions && !m.actionOpts.ReadOnly && !m.actionOpts.Demo &&
		m.deps.actioner != nil && m.connectionReady && m.connectionFresh
}

// actionExec is what one request needs off the update loop. It is copied
// out of the root before the command starts, so the command never reads
// model state from another goroutine.
type actionExec struct {
	reader   core.Reader
	actioner core.Actioner
	journal  *journal.Journal
	clock    Clock
	profile  string
	server   string
}

func (m *Root) actionExec() actionExec {
	return actionExec{
		reader:   m.deps.reader,
		actioner: m.deps.actioner,
		journal:  m.deps.journal,
		clock:    m.deps.clock,
		profile:  m.actionOpts.Profile,
		server:   m.actionOpts.Server,
	}
}

// startAction performs the safety-critical sequence in one cancelable command:
// preflight identity, exactly one mutation, then authoritative read-back.
func (m *Root) startAction(req core.ActionRequest) tea.Cmd {
	if !m.actionsPermitted() {
		return nil
	}
	if req.Ref.UID == "" || req.Ref.Namespace == "" || req.Ref.Name == "" {
		return nil
	}
	if m.hasInflight("action") {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.actionAttempt++
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.actionAttempt}
	// The entry is tagged with the attempt the reply will carry, so a late
	// reply from an earlier attempt cannot retire this one.
	m.setInflight("action", uint64(m.actionAttempt), cancel)
	exec := m.actionExec()
	return func() tea.Msg {
		result, err := exec.run(ctx, req)
		return actionResultMsg{genStamp: g, Result: result, Err: err, JournalErr: exec.record(req, result, err)}
	}
}

// run sends one request: preflight identity and applicability, exactly one
// mutation, then read-back. It is the whole of a single action and one step
// of a bulk one.
func (x actionExec) run(ctx context.Context, req core.ActionRequest) (core.ActionResult, error) {
	// Every check runs before anything is sent. A failure here changed
	// nothing on the server, so the outcome is refused rather than unknown:
	// there is nothing to go and inspect.
	actual, err := x.reader.Get(ctx, req.Ref)
	if err != nil {
		return refusedAction(req), err
	}
	if err := req.Validate(actual.Summary.Ref); err != nil {
		return refusedAction(req), err
	}
	if !req.Action.AppliesTo(actual.Summary) {
		return refusedAction(req), &core.NotApplicableError{Action: req.Action, Phase: actual.Summary.Phase}
	}
	result, err := x.actioner.Execute(ctx, req) // exactly one call
	if err != nil || result.Outcome == core.ActionUnknown {
		// An ambiguous send is never repeated. Best-effort inspection is
		// allowed, but the UI remains UNKNOWN regardless of what it finds.
		if inspected, inspectErr := x.reader.Get(ctx, req.Ref); inspectErr == nil {
			result.Workflow = &inspected
		}
		result.Action, result.Target = req.Action, req.Ref
		result.Outcome = core.ActionUnknown
		if err == nil {
			err = errors.New("action outcome unknown; inspect before retrying")
		}
		return result, err
	}
	// The server returned success, so the mutation is applied. From here
	// on the outcome is at worst ACCEPTED; it never degrades to UNKNOWN,
	// because a failed observation says nothing about a request the
	// server already acknowledged.
	ref := req.Ref
	if result.Affected != nil {
		ref = *result.Affected
	}
	wf, settled, readErr := readBack(ctx, x.reader, ref, req.Action)
	if readErr != nil {
		result.Outcome = core.ActionAccepted
		return result, readErr
	}
	// A deleted workflow has nothing left to show. Whatever a read found
	// under its name is an archived copy, and its phase would read as the
	// state of a workflow that is gone.
	if req.Action != core.ActionDelete {
		result.Workflow = &wf
	}
	if settled {
		result.Outcome = core.ActionConfirmed
	} else {
		result.Outcome = core.ActionAccepted
	}
	return result, nil
}

// record appends one journal line for a finished attempt. The outcome is
// settled first, so the journal says what the pane says.
func (x actionExec) record(req core.ActionRequest, result core.ActionResult, err error) error {
	if x.journal == nil {
		return nil
	}
	e := journal.Entry{
		Profile:   x.profile,
		Server:    x.server,
		Namespace: req.Ref.Namespace,
		Name:      req.Ref.Name,
		UID:       req.Ref.UID,
		Verb:      string(req.Action),
		Outcome:   string(settledOutcome(result.Outcome, err)),
	}
	if x.clock != nil {
		e.Time = x.clock.Now().UTC()
	}
	if err != nil {
		e.Error = err.Error()
	}
	return x.journal.Record(e)
}

// settledOutcome is the outcome a result is reported with. An error
// alongside an accepted result describes a failed observation, not a failed
// mutation, so the accepted outcome stands. Only a result that is neither
// confirmed nor accepted degrades to unknown.
func settledOutcome(o core.ActionOutcome, err error) core.ActionOutcome {
	if err != nil && o != core.ActionAccepted && o != core.ActionUnknown && o != core.ActionRefused {
		return core.ActionUnknown
	}
	return o
}

// refusedAction reports an action that never left this process.
func refusedAction(req core.ActionRequest) core.ActionResult {
	return core.ActionResult{Action: req.Action, Target: req.Ref, Outcome: core.ActionRefused}
}

// stopObservationInterval and stopObservationAttempts bound how long an
// accepted Stop, Terminate or Delete is watched for its end state. A
// graceful Stop runs the workflow's exit handler first, so the phase can lag
// the accepted request by many seconds.
//
// The budget is short on purpose. The action pane is modal, so every second
// spent here is a second the reader stares at "waiting for the server" with
// no way forward. Five seconds separates a quick stop from a slow one; past
// that the outcome is reported as ACCEPTED and the detail pane, which
// refetches on the same poll, shows the phase settle in place. A bulk
// action spends the budget per target, which is one more reason to keep it
// short.
//
// They are variables, not constants, so a test can shorten the budget without
// waiting out a real exit handler.
var (
	stopObservationInterval = time.Second
	stopObservationAttempts = 5
)

// readBack observes the state that follows an accepted mutation. settled
// reports whether the expected end state was seen inside the budget; false
// means the mutation is applied but still in progress, never that it failed.
//
// The end state of each action:
//   - resume: the workflow no longer waits for a person.
//   - suspend: the workflow reports itself suspended (spec.suspend is set
//     in the server's answer at once; the phase stays Running).
//   - retry: the workflow has left its terminal phase.
//   - stop, terminate: the workflow has reached a terminal phase.
//   - resubmit: the new workflow exists.
//   - delete: a read of the workflow answers not found.
func readBack(ctx context.Context, reader core.Reader, ref core.Ref, action core.Action) (wf core.Workflow, settled bool, err error) {
	if action == core.ActionDelete {
		return observeUntil(ctx, reader, ref, nil)
	}
	wf, err = reader.Get(ctx, ref)
	if err != nil {
		return core.Workflow{}, false, err
	}
	switch action {
	case core.ActionStop, core.ActionTerminate:
		return observeUntil(ctx, reader, ref, &wf)
	case core.ActionResume:
		return wf, !wf.Summary.Suspended, nil
	case core.ActionSuspend:
		return wf, wf.Summary.Suspended, nil
	case core.ActionRetry:
		return wf, !terminalPhase(wf.Summary.Phase), nil
	default:
		// Resubmit is read back under the new name, so the workflow
		// existing is the observation.
		return wf, true, nil
	}
}

// observeUntil polls ref inside the observation budget. With a first read
// in hand it waits for a terminal phase; without one it is watching a
// delete, and waits for a read that answers not found.
//
// A server with the workflow archive enabled answers a read of a deleted
// workflow from the archive. Such a delete never reads as gone, and
// reaches ACCEPTED when the budget runs out: the server did apply it.
func observeUntil(ctx context.Context, reader core.Reader, ref core.Ref, first *core.Workflow) (core.Workflow, bool, error) {
	deleting := first == nil
	done := func(wf core.Workflow) bool { return terminalPhase(wf.Summary.Phase) }
	var wf core.Workflow
	if deleting {
		next, err := reader.Get(ctx, ref)
		if isNotFound(err) {
			return core.Workflow{}, true, nil
		}
		if err != nil {
			return core.Workflow{}, false, err
		}
		wf = next
		done = func(core.Workflow) bool { return false }
	} else {
		wf = *first
	}
	for i := 0; i < stopObservationAttempts && !done(wf); i++ {
		timer := time.NewTimer(stopObservationInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			// Cancellation is not evidence about the workflow. Report the
			// last observed state as still in progress.
			return wf, false, nil
		case <-timer.C:
		}
		next, getErr := reader.Get(ctx, ref)
		if deleting && isNotFound(getErr) {
			return core.Workflow{}, true, nil
		}
		if getErr != nil {
			return wf, false, getErr
		}
		wf = next
	}
	return wf, done(wf), nil
}

// isNotFound reports whether err is the server saying the workflow does not
// exist.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	apiErr := core.AsAPIError(err)
	return apiErr != nil && apiErr.Kind == core.ErrNotFound
}

func terminalPhase(phase string) bool {
	switch strings.ToLower(phase) {
	case "succeeded", "failed", "error", "killed", "stopped", "terminated":
		return true
	default:
		return false
	}
}

func (m *Root) handleActionResult(msg actionResultMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.actionAttempt {
		return nil
	}
	m.clearInflight("action", uint64(msg.Attempt))
	warning := m.journalWarning(msg.JournalErr)
	if m.actionView == nil {
		return nil
	}
	msg.Result.Outcome = settledOutcome(msg.Result.Outcome, msg.Err)
	m.actionView.SetOutcome(msg.Result)
	// A finished action hands the screen back by itself. Waiting for an Esc
	// left the reader one press away from the workflow they had just changed.
	// The one-line report moves to the footer of the route behind it.
	//
	// UNKNOWN is the exception: nobody knows whether the server applied it,
	// and that is precisely the state a reader must see rather than dismiss.
	if msg.Result.Outcome == core.ActionUnknown {
		m.flash = warning
		return nil
	}
	m.flash = joinFlash(m.actionView.OutcomeLine(), warning)
	m.actionView.Close()
	// A deleted workflow has no detail left to show, so the reader goes
	// back to the list, which back() refreshes.
	if msg.Result.Action == core.ActionDelete && m.route == RouteDetail {
		m.actionFromMarks = false
		return m.back()
	}
	return m.afterActionPane()
}

// startBulk begins a confirmed bulk action. Its requests run one at a time,
// each through the same preflight, send and read-back as a single action.
// One runs at a time for two reasons: every result must be attributable to
// exactly one request, and a stale snapshot or lost connection discovered
// halfway must stop the rest before they are sent.
func (m *Root) startBulk(reqs []core.ActionRequest) tea.Cmd {
	if !m.actionsPermitted() || len(reqs) == 0 || m.bulk != nil || m.hasInflight("action") {
		return nil
	}
	for _, req := range reqs {
		if req.Ref.UID == "" || req.Ref.Namespace == "" || req.Ref.Name == "" {
			return nil
		}
	}
	m.bulk = &bulkRun{reqs: append([]core.ActionRequest(nil), reqs...)}
	return m.bulkStep()
}

// bulkStep starts the next request of the bulk run.
func (m *Root) bulkStep() tea.Cmd {
	i := m.bulk.next
	req := m.bulk.reqs[i]
	ctx, cancel := context.WithCancel(context.Background())
	m.actionAttempt++
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.actionAttempt}
	m.setInflight("action", uint64(m.actionAttempt), cancel)
	exec := m.actionExec()
	return func() tea.Msg {
		result, err := exec.run(ctx, req)
		return bulkStepMsg{genStamp: g, Index: i, Result: result, Err: err, JournalErr: exec.record(req, result, err)}
	}
}

// handleBulkStep records one finished request and starts the next, or
// reports the whole run.
//
// Before each further request the gates are checked again. A connection
// that dropped, a snapshot that went stale, or the reader asking to stop
// ends the run there: the requests not yet sent are reported REFUSED, which
// is exactly true of them.
func (m *Root) handleBulkStep(msg bulkStepMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.actionAttempt ||
		m.bulk == nil || msg.Index != m.bulk.next {
		return nil
	}
	m.clearInflight("action", uint64(msg.Attempt))
	if w := m.journalWarning(msg.JournalErr); w != "" {
		m.flash = w
	}
	run := m.bulk
	result := msg.Result
	result.Outcome = settledOutcome(result.Outcome, msg.Err)
	run.items = append(run.items, actions.BulkItem{Result: result, Reason: errText(msg.Err)})
	run.next++
	if m.actionView != nil {
		m.actionView.SetBulkProgress(run.next)
	}
	if run.next < len(run.reqs) {
		reason := ""
		switch {
		case m.actionView == nil || m.actionView.StopRequested():
			reason = "stopped before it was sent"
		case !m.actionsPermitted():
			reason = "actions blocked before it was sent (" + staleActionReason(m.connectionReady) + ")"
		}
		if reason == "" {
			return m.bulkStep()
		}
		for _, req := range run.reqs[run.next:] {
			run.items = append(run.items, actions.BulkItem{Result: refusedAction(req), Reason: reason})
		}
	}
	m.bulk = nil
	if m.actionView != nil {
		m.actionView.SetBulkOutcome(run.items)
	}
	return nil
}

// afterActionPane runs when a finished action's pane closes: the marks it
// acted on are dropped, so a second press of a cannot resend the same set
// by accident, and the route behind it is refetched, because the workflows
// the reader is looking at have just changed.
func (m *Root) afterActionPane() tea.Cmd {
	if m.actionFromMarks {
		m.listView.ClearMarks()
		m.actionFromMarks = false
	}
	switch m.route {
	case RouteDetail:
		if m.selection.UID != "" {
			return m.startDetailFetch()
		}
	case RouteList:
		if !m.listState.loading && !terminalWatchMode(m.watchMode) {
			return m.startListGeneration()
		}
	}
	return nil
}

// journalWarning turns the first journal failure of the session into a
// footer line, and every later one into nothing. The journal is a record,
// not a gate: repeating the warning after every action would bury the
// outcome it sits beside.
func (m *Root) journalWarning(err error) string {
	if err == nil || m.journalWarned {
		return ""
	}
	m.journalWarned = true
	return "action journal not written: " + shortReason(shared.Sanitize(err.Error()))
}

// joinFlash puts two footer reports on one line.
func joinFlash(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " · " + b
	}
}

// errText is the reason shown beside one bulk target, empty for no error.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// openDetailActions opens the action pane for the workflow the detail route
// shows. Its availability is judged from the loaded workflow, or from the
// workflow's list row while the detail is still loading; with neither, the
// menu offers every action rather than guess at a phase.
func (m *Root) openDetailActions() {
	var targets []core.Summary
	if s, ok := m.knownSummary(m.selection); ok {
		targets = []core.Summary{s}
	}
	m.openActions(m.selection, targets, false)
}

// openListActions opens the action pane from the list: on the marked
// workflows when there are marks, otherwise on the selected row.
func (m *Root) openListActions() {
	if marked := m.listView.Marked(); len(marked) > 0 {
		m.openActions(marked[0].Ref, marked, true)
		return
	}
	sel := m.listView.Selected()
	if sel.Ref.UID == "" {
		m.flash = "no workflow selected"
		return
	}
	m.openActions(sel.Ref, []core.Summary{sel}, false)
}

// openActions builds the action pane for targets and opens its menu, or its
// blocked state when the data behind it is stale. The freshness gate is the
// same on every route: an action chosen from a stale list is exactly as
// dangerous as one chosen from a stale detail pane.
func (m *Root) openActions(ref core.Ref, targets []core.Summary, fromMarks bool) {
	m.actionView = m.newActionView(ref)
	if len(targets) > 0 {
		m.actionView.SetTargets(targets)
	}
	phase := ""
	if len(targets) == 1 {
		phase = targets[0].Phase
	}
	m.actionView.SetContext(m.actionOpts.Server, m.actionOpts.Profile, phase)
	m.actionView.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	m.actionFromMarks = fromMarks
	if m.connectionReady && m.connectionFresh {
		m.actionView.OpenMenu()
	} else {
		m.actionView.OpenUnavailable(staleActionReason(m.connectionReady))
	}
}

// knownSummary is the freshest summary held for ref: the loaded detail,
// else the list row. ok is false when neither is held.
func (m *Root) knownSummary(ref core.Ref) (core.Summary, bool) {
	if ref.UID == "" {
		return core.Summary{}, false
	}
	if s := m.detailState.workflow.Summary; s.Ref == ref {
		return s, true
	}
	for _, it := range m.listState.items {
		if it.Ref == ref {
			return it, true
		}
	}
	return core.Summary{}, false
}

// bulkIntentMatches reports whether every request of a bulk intent targets
// a workflow of the open bulk pane. Only the active, confirmed pane may
// reach the executor.
func (m *Root) bulkIntentMatches(reqs []core.ActionRequest) bool {
	if m.actionView == nil || !m.actionView.Bulk() || m.actionView.State() != actions.StateSubmitting || len(reqs) == 0 {
		return false
	}
	known := map[core.Ref]bool{}
	for _, t := range m.actionView.Targets() {
		known[t.Ref] = true
	}
	for _, req := range reqs {
		if !known[req.Ref] || req.Action != m.actionView.Action() {
			return false
		}
	}
	return true
}
