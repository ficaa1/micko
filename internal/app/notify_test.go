package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/notify"
)

// notifyRoot is a root with no watch transport that records each notice as
// "title: body".
func notifyRoot(f core.Reader, n config.Notify) (*Root, *[]string) {
	m := newRoot(f, "ns", time.Millisecond)
	m.deps.watcher = nil
	m.SetNotify(n)
	got := &[]string{}
	m.notifySend = func(n notify.Notice) error { *got = append(*got, n.Title+": "+n.Body); return nil }
	return m, got
}

// listed delivers a collected snapshot the way the list poll does, stamped
// with the current generation.
func listed(m *Root, items ...core.Summary) tea.Cmd {
	_, cmd := m.Update(listLoadedMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, Page: core.Page{Items: items}})
	return cmd
}

// A workflow that changes state between two snapshots notifies when it is
// watched with W, or when it becomes suspended and the config asks for that.
// A watched workflow the watch reports deleted notifies once.
func TestNotifications(t *testing.T) {
	running := summary("a")
	succeeded := running
	succeeded.Phase = "Succeeded"
	suspended := running
	suspended.Suspended = true
	watchedOnly := config.Notify{Watched: true, Via: []config.NotifyVia{config.NotifyBell}}
	suspendedOnly := config.Notify{Suspended: true, Via: []config.NotifyVia{config.NotifyBell}}
	for _, c := range []struct {
		name     string
		settings config.Notify
		watch    bool
		// first skips the snapshot before the change, as on a fresh list.
		first bool
		// via is how the change arrives: a watch event, a poll, or a watch
		// event deleting the workflow.
		via           string
		before, after core.Summary
		want          string
	}{
		{"watched by the watch", watchedOnly, true, false, "watch", running, succeeded, "a: Running → Succeeded"},
		{"watched by a poll", watchedOnly, true, false, "poll", running, suspended, "a: Running → Suspended"},
		{"watched, no change", watchedOnly, true, false, "watch", running, running, ""},
		{"watched, deleted", watchedOnly, true, false, "delete", running, running, "a: deleted; no longer watched"},
		{"not watched, deleted", watchedOnly, false, false, "delete", running, running, ""},
		{"not watched", watchedOnly, false, false, "watch", running, succeeded, ""},
		{"watching turned off", suspendedOnly, true, false, "watch", running, succeeded, ""},
		{"becomes suspended", suspendedOnly, false, false, "watch", running, suspended, "a: suspended, waiting for resume"},
		{"suspended by a poll", suspendedOnly, false, false, "poll", running, suspended, "a: suspended, waiting for resume"},
		{"suspended rule off", watchedOnly, false, false, "watch", running, suspended, ""},
		{"first list", suspendedOnly, false, true, "poll", running, suspended, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, got := notifyRoot(fixtureReader(), c.settings)
			if !c.first {
				runCmd(listed(m, c.before))
			}
			if c.watch {
				keys(m, "W")
			}
			var cmd tea.Cmd
			switch c.via {
			case "poll":
				cmd = listed(m, c.after)
			case "delete":
				_, cmd = m.Update(watchEventMsg{Event: core.WatchEvent{Type: core.WatchDeleted, Summary: c.after}})
			default:
				_, cmd = m.Update(watchEventMsg{Event: core.WatchEvent{Type: core.WatchModified, Summary: c.after}})
			}
			bells := 0
			for _, msg := range runCmd(cmd) {
				if raw, ok := msg.(tea.RawMsg); ok && raw.Msg == "\a" {
					bells++
				}
			}
			if strings.Join(*got, "|") != c.want {
				t.Fatalf("notified %q, want %q", *got, c.want)
			}
			if bells != len(*got) {
				t.Fatalf("%d bells for %d notices", bells, len(*got))
			}
			if c.via == "delete" && len(m.watched) != 0 {
				t.Fatalf("still watching %v after the deletion", m.watched)
			}
		})
	}
}

// readWatched runs the watched check the chain armed, its tick and its
// read, and returns the read's message undelivered; nil when none ran.
func readWatched(m *Root, armed tea.Cmd) tea.Msg {
	for _, msg := range runCmd(armed) {
		if tick, ok := msg.(watchedTickMsg); ok {
			_, read := m.Update(tick)
			for _, msg := range runCmd(read) {
				if loaded, ok := msg.(watchedLoadedMsg); ok {
					return loaded
				}
			}
		}
	}
	return nil
}

// deliverWatched delivers a check's read, sends its notices and returns the
// next check it arms, nil when it arms none.
func deliverWatched(m *Root, loaded tea.Msg) tea.Cmd {
	_, cmd := m.Update(loaded)
	var next tea.Cmd
	for _, msg := range runCmd(cmd) {
		if tick, ok := msg.(watchedTickMsg); ok {
			next = func() tea.Msg { return tick }
		}
	}
	return next
}

// A watch outlives a namespace switch: micko reads the watched workflows
// itself and notifies when one changes, or is gone, whether the server
// answers 404 or with the archive's copy. A profile switch ends every watch.
func TestWatchOutsideTheList(t *testing.T) {
	a, b := workflowFixture("a"), workflowFixture("b")
	f := fixtureReader(a, b)
	m, got := notifyRoot(f, config.Notify{Watched: true})
	runCmd(listed(m, a.Summary, b.Summary))
	keys(m, "space", "j", "space")
	check := keys(m, "W")
	m.switchNamespace("other")

	check = deliverWatched(m, readWatched(m, check))
	if len(*got) != 0 {
		t.Fatalf("notified %q before any change", *got)
	}
	if !strings.Contains(screen(m), "◉ 2 watched") {
		t.Fatalf("the other namespace's toolbar lacks the watches:\n%s", screen(m))
	}
	done := a
	done.Summary.Phase = "Succeeded"
	f.Workflows[a.Summary.Ref] = done
	check = deliverWatched(m, readWatched(m, check))
	archived := done
	archived.Summary.Labels = map[string]string{"workflows.argoproj.io/workflow-archiving-status": "Persisted"}
	f.Workflows[a.Summary.Ref] = archived
	delete(f.Workflows, b.Summary.Ref)
	check = deliverWatched(m, readWatched(m, check))
	if len(*got) > 1 {
		slices.Sort((*got)[1:])
	}
	if want := "a: Running → Succeeded|a: deleted; no longer watched|b: deleted; no longer watched"; strings.Join(*got, "|") != want {
		t.Fatalf("notified %q, want %q", *got, want)
	}
	if len(m.watched) != 0 || check != nil {
		t.Fatalf("watched %v, check armed %v after both were deleted", m.watched, check != nil)
	}

	m.watched = map[string]watch{a.Summary.Ref.UID: {last: a.Summary}}
	m.Adopt(&Connection{Reader: f, Namespace: "ns"})
	if len(m.watched) != 0 {
		t.Fatalf("watched %v after a profile switch, want none", m.watched)
	}
}

// A watched read that a newer snapshot overtook while it ran is dropped, so
// a workflow that finished never reads as running again.
func TestWatchedReadOvertaken(t *testing.T) {
	wf := workflowFixture("a")
	m, got := notifyRoot(fixtureReader(wf), config.Notify{Watched: true})
	runCmd(listed(m, wf.Summary))
	check := keys(m, "W")
	m.switchNamespace("other")
	stale := readWatched(m, check)

	m.switchNamespace("ns")
	done := wf.Summary
	done.Phase = "Succeeded"
	runCmd(listed(m, done))
	deliverWatched(m, stale)
	if want := "a: Running → Succeeded"; strings.Join(*got, "|") != want {
		t.Fatalf("notified %q, want %q", *got, want)
	}
}
