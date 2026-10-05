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
	// JournalErr is a failed journal append, reported but never acted on.
	JournalErr error
}

// bulkStepMsg is the result of one bulk request; Index ties it to that
// request.
type bulkStepMsg struct {
	genStamp
	Index      int
	Result     core.ActionResult
	Err        error
	JournalErr error
}

// bulkRun is a bulk action in progress. next only moves forward, so no
// request is sent twice.
type bulkRun struct {
	reqs  []core.ActionRequest
	items []actions.BulkItem
	next  int
}

// SetJournal installs the action journal; nil turns it off.
func (m *Root) SetJournal(j *journal.Journal) { m.deps.journal = j }

// actionsPermitted reports whether a mutation may start now.
func (m *Root) actionsPermitted() bool {
	return m.actionOpts.AllowActions && !m.actionOpts.ReadOnly && !m.actionOpts.Demo &&
		m.deps.actioner != nil && m.connectionReady && m.connectionFresh
}

// actionExec is copied out of the root before a command starts, so the
// command never reads model state from another goroutine.
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

// startAction runs one action as a cancelable command.
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
	m.setInflight("action", uint64(m.actionAttempt), cancel)
	exec := m.actionExec()
	return func() tea.Msg {
		result, err := exec.run(ctx, req)
		return actionResultMsg{genStamp: g, Result: result, Err: err, JournalErr: exec.record(req, result, err)}
	}
}

// run preflights one request, sends it exactly once and reads it back. It is
// a whole single action and one step of a bulk one.
func (x actionExec) run(ctx context.Context, req core.ActionRequest) (core.ActionResult, error) {
	// A failure before the send changed nothing, so the outcome is refused.
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
		// An ambiguous send is never repeated. The outcome stays UNKNOWN whatever
		// this read finds.
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
	// The server accepted the mutation, so a failed read-back cannot make the
	// outcome worse than ACCEPTED.
	ref := req.Ref
	if result.Affected != nil {
		ref = *result.Affected
	}
	wf, settled, readErr := readBack(ctx, x.reader, ref, req.Action)
	if readErr != nil {
		result.Outcome = core.ActionAccepted
		return result, readErr
	}
	// A read after a delete finds at most the archived copy, whose phase would
	// mislead.
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

// record appends the journal line for a finished attempt, with its settled
// outcome.
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

// settledOutcome is the outcome a result is reported with: an error turns
// anything but ACCEPTED or REFUSED into UNKNOWN.
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
// accepted Stop, Terminate or Delete is watched for its end state. The action
// pane is modal while it waits, so the budget is short; past it the outcome is
// ACCEPTED and the detail pane shows the phase settle. Tests shorten it.
var (
	stopObservationInterval = time.Second
	stopObservationAttempts = 5
)

// readBack observes the state after an accepted mutation. settled is false
// when the end state was not seen inside the budget: the mutation is applied
// but still in progress.
//
// The end state of each action:
//   - resume: the workflow no longer waits for a person.
//   - suspend: the workflow reports itself suspended; the phase stays Running.
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
		// Resubmit reads back the new workflow, so finding it is the end state.
		return wf, true, nil
	}
}

// observeUntil polls ref inside the observation budget: for a terminal phase
// when first is set, otherwise for a delete to read as not found. With the
// archive enabled a deleted workflow still reads, so the delete ends ACCEPTED.
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
			// Cancellation says nothing about the workflow.
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

// isNotFound reports whether the server said the workflow does not exist.
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
	// A finished action closes its pane and reports in the footer of the route
	// behind it. UNKNOWN stays on screen: the reader must see it.
	if msg.Result.Outcome == core.ActionUnknown {
		m.flash = warning
		return nil
	}
	m.flash = joinFlash(m.actionView.OutcomeLine(), warning)
	m.actionView.Close()
	// A deleted workflow has no detail left, so the reader returns to the list.
	if msg.Result.Action == core.ActionDelete && m.route == RouteDetail {
		m.actionFromMarks = false
		return m.back()
	}
	return m.afterActionPane()
}

// startBulk begins a confirmed bulk action. Requests run one at a time, so
// each result belongs to one request and a lost connection stops the rest
// before they are sent.
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
// reports the run. A lost connection, a stale snapshot or a stop ends the
// run; the requests not yet sent are reported REFUSED.
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

// afterActionPane drops the marks the action used, so a second a cannot
// resend them, and refetches the route behind the pane.
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

// journalWarning returns a footer line for the session's first journal
// failure and nothing for later ones.
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

// openDetailActions opens the action pane for the detail route's workflow.
// Without a loaded workflow or list row, the menu offers every action.
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

// openActions opens the action pane for targets, or its blocked state when
// the data behind it is stale.
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

// bulkIntentMatches reports whether every request targets a workflow of the
// open bulk pane.
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
