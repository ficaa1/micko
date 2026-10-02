package app

import (
	"context"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/notify"
)

// notifyFailedMsg carries a delivery that failed, in whole or in part.
type notifyFailedMsg struct{ err error }

// watchedTickMsg starts a check of the watched workflows the list does not
// cover. gen is the connection the chain belongs to.
type watchedTickMsg struct{ gen int }

// watchedLoadedMsg carries the result of that check.
type watchedLoadedMsg struct {
	gen     int
	results []watchedResult
}

type watchedResult struct {
	ref     core.Ref
	rev     uint64
	summary core.Summary
	err     error
}

// watch is one watched workflow: the state last seen, and a revision every
// observation bumps, so a read that started before the latest observation
// is discarded instead of reporting a state the workflow has left.
type watch struct {
	last core.Summary
	rev  uint64
}

// SetNotify sets which workflow changes notify the reader and how. The zero
// value notifies about nothing.
func (m *Root) SetNotify(n config.Notify) { m.notify = n }

// toggleWatch is the list's W key: it watches the marked workflows and drops
// the marks, or watches or unwatches the selected one.
func (m *Root) toggleWatch() tea.Cmd {
	if !m.notify.Watched {
		m.flash = "watching is off: notifications.watched is false in the config"
		return nil
	}
	targets := m.listView.Marked()
	if len(targets) == 0 {
		sel := m.listView.Selected()
		if sel.Ref.UID == "" {
			m.flash = "no workflow selected"
			return nil
		}
		if _, ok := m.watched[sel.Ref.UID]; ok {
			delete(m.watched, sel.Ref.UID)
			m.syncWatched()
			m.flash = "stopped watching " + sel.Ref.Name
			return nil
		}
		targets = []core.Summary{sel}
	}
	if m.watched == nil {
		m.watched = map[string]watch{}
	}
	for _, s := range targets {
		m.watched[s.Ref.UID] = watch{last: s}
	}
	m.listView.ClearMarks()
	m.syncWatched()
	if len(targets) == 1 {
		m.flash = "watching " + targets[0].Ref.Name + " for phase changes; W again stops"
	} else {
		m.flash = "watching " + strconv.Itoa(len(targets)) + " workflows for phase changes"
	}
	return m.armWatchedCheck()
}

// syncWatched hands the list the watched UIDs to flag.
func (m *Root) syncWatched() {
	uids := make(map[string]bool, len(m.watched))
	for uid := range m.watched {
		uids[uid] = true
	}
	m.listView.SetWatched(uids)
}

// dropWatches forgets every watch. UIDs belong to one cluster, so a
// profile switch ends them, and any check in flight for them is discarded.
func (m *Root) dropWatches() {
	m.watched = nil
	m.watchedGen++
	m.watchedArmed = false
	m.syncWatched()
}

// armWatchedCheck starts the chain that checks watched workflows outside the
// list, unless one runs. One check runs per refresh interval.
func (m *Root) armWatchedCheck() tea.Cmd {
	if m.watchedArmed || len(m.watched) == 0 {
		return nil
	}
	m.watchedArmed = true
	gen := m.watchedGen
	tick := m.deps.tickCmd()
	return func() tea.Msg {
		tick()
		return watchedTickMsg{gen: gen}
	}
}

// listCovers reports whether the list keeps the workflow's state current:
// it is in the snapshot, and the watch is live or the list is polling.
func (m *Root) listCovers(uid string) bool {
	return m.inSnapshot(uid) && (m.watchLive() || m.route == RouteList)
}

// handleWatchedTick reads every watched workflow the list does not cover.
func (m *Root) handleWatchedTick(msg watchedTickMsg) tea.Cmd {
	if msg.gen != m.watchedGen {
		return nil
	}
	m.watchedArmed = false
	var reads []watchedResult
	for uid, w := range m.watched {
		if !m.listCovers(uid) {
			reads = append(reads, watchedResult{ref: w.last.Ref, rev: w.rev})
		}
	}
	if len(reads) == 0 || m.deps.reader == nil {
		return m.armWatchedCheck()
	}
	m.watchedArmed = true
	reader, gen := m.deps.reader, m.watchedGen
	return func() tea.Msg {
		for i := range reads {
			wf, err := reader.Get(context.Background(), reads[i].ref)
			reads[i].summary, reads[i].err = wf.Summary, err
		}
		return watchedLoadedMsg{gen: gen, results: reads}
	}
}

// handleWatchedLoaded notifies about the watched workflows the check read.
// A read that an observation overtook while it ran is dropped. One that
// finds the workflow gone notifies once and ends the watch; any other error
// leaves the watch for the next check.
func (m *Root) handleWatchedLoaded(msg watchedLoadedMsg) tea.Cmd {
	if msg.gen != m.watchedGen {
		return nil
	}
	m.watchedArmed = false
	var cmds []tea.Cmd
	for _, r := range msg.results {
		w, ok := m.watched[r.ref.UID]
		if !ok || w.rev != r.rev {
			continue
		}
		if ae := core.AsAPIError(r.err); ae != nil && ae.Kind == core.ErrNotFound {
			cmds = append(cmds, m.unwatchGone(r.ref))
			continue
		}
		if r.err != nil {
			continue
		}
		cmds = append(cmds, m.observe(w.last, r.summary))
	}
	return tea.Batch(append(cmds, m.armWatchedCheck())...)
}

// unwatchGone ends the watch on a workflow that no longer exists and tells
// the reader, once.
func (m *Root) unwatchGone(ref core.Ref) tea.Cmd {
	if _, ok := m.watched[ref.UID]; !ok {
		return nil
	}
	delete(m.watched, ref.UID)
	m.syncWatched()
	return m.deliver(notify.Gone(ref))
}

// notifySnapshot notifies about each workflow whose state changed in a new
// snapshot. A watched workflow is compared with its last seen state, wherever
// that was seen; any other with its row in the snapshot before. A workflow
// that appears says nothing, or the first list and every namespace switch
// would announce every row.
func (m *Root) notifySnapshot(before, after []core.Summary) tea.Cmd {
	prev := make(map[string]core.Summary, len(before))
	for _, s := range before {
		prev[s.Ref.UID] = s
	}
	var cmds []tea.Cmd
	for _, s := range after {
		if p, ok := prev[s.Ref.UID]; ok {
			cmds = append(cmds, m.observe(p, s))
		} else if _, ok := m.watched[s.Ref.UID]; ok {
			cmds = append(cmds, m.observe(s, s))
		}
	}
	return tea.Batch(cmds...)
}

// observe records a workflow's new state and returns the command that
// notifies the reader about the change, nil when there is nothing to say. A
// watched workflow seen only as its archived copy has been deleted.
func (m *Root) observe(before, after core.Summary) tea.Cmd {
	w, watched := m.watched[after.Ref.UID]
	if watched {
		if notify.Archived(after) {
			return m.unwatchGone(after.Ref)
		}
		before = w.last
		m.watched[after.Ref.UID] = watch{last: after, rev: w.rev + 1}
	}
	n, ok := notify.Change(before, after, watched, m.notify)
	if !ok {
		return nil
	}
	return m.deliver(n)
}

// deliver sends n by every method the settings name.
func (m *Root) deliver(n notify.Notice) tea.Cmd {
	var cmds []tea.Cmd
	if slices.Contains(m.notify.Via, config.NotifyBell) {
		cmds = append(cmds, tea.Raw("\a"))
	}
	if slices.Contains(m.notify.Via, config.NotifyTerminal) {
		cmds = append(cmds, tea.Raw(notify.OSC9(n)))
	}
	send := m.notifySend
	if send == nil {
		settings := m.notify
		send = func(n notify.Notice) error { return notify.Deliver(n, settings) }
	}
	cmds = append(cmds, func() tea.Msg {
		if err := send(n); err != nil {
			return notifyFailedMsg{err}
		}
		return nil
	})
	return tea.Batch(cmds...)
}

// handleNotifyFailed reports the first failed delivery of the session; one
// broken notifier would otherwise take the footer on every change.
func (m *Root) handleNotifyFailed(msg notifyFailedMsg) {
	if m.notifyWarned {
		return
	}
	m.notifyWarned = true
	m.flash = "notification: " + msg.err.Error()
}
