package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/notify"
)

// A workflow that changes state between two snapshots notifies when it is
// watched with W, or when it becomes suspended and the config asks for that.
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
		first         bool
		viaList       bool
		before, after core.Summary
		want          string
	}{
		{"watched by the watch", watchedOnly, true, false, false, running, succeeded, "a: Running → Succeeded"},
		{"watched by a poll", watchedOnly, true, false, true, running, suspended, "a: Running → Suspended"},
		{"watched, no change", watchedOnly, true, false, false, running, running, ""},
		{"not watched", watchedOnly, false, false, false, running, succeeded, ""},
		{"watching turned off", suspendedOnly, true, false, false, running, succeeded, ""},
		{"becomes suspended", suspendedOnly, false, false, false, running, suspended, "a: suspended, waiting for resume"},
		{"suspended by a poll", suspendedOnly, false, false, true, running, suspended, "a: suspended, waiting for resume"},
		{"suspended rule off", watchedOnly, false, false, false, running, suspended, ""},
		{"first list", suspendedOnly, false, true, true, running, suspended, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newRoot(fixtureReader(), "ns", time.Millisecond)
			m.deps.watcher = nil
			m.SetNotify(c.settings)
			var got []string
			m.notifySend = func(n notify.Notice) error { got = append(got, n.Title+": "+n.Body); return nil }
			if !c.first {
				runCmd(m.handleListLoaded(listLoadedMsg{Page: core.Page{Items: []core.Summary{c.before}}}))
			}
			if c.watch {
				keys(m, "W")
			}
			var cmd tea.Cmd
			if c.viaList {
				cmd = m.handleListLoaded(listLoadedMsg{Page: core.Page{Items: []core.Summary{c.after}}})
			} else {
				_, cmd = m.Update(watchEventMsg{Event: core.WatchEvent{Type: core.WatchModified, Summary: c.after}})
			}
			bells := 0
			for _, msg := range runCmd(cmd) {
				if raw, ok := msg.(tea.RawMsg); ok && raw.Msg == "\a" {
					bells++
				}
			}
			if strings.Join(got, "|") != c.want {
				t.Fatalf("notified %q, want %q", got, c.want)
			}
			if bells != len(got) {
				t.Fatalf("%d bells for %d notices", bells, len(got))
			}
		})
	}
}

// A watch outlives a namespace switch: micko reads the watched workflow
// itself, notifies when it changes or is deleted, and a profile switch
// ends every watch.
func TestWatchOutsideTheList(t *testing.T) {
	wf := workflowFixture("a")
	f := fixtureReader(wf)
	m := newRoot(f, "ns", time.Millisecond)
	m.deps.watcher = nil
	m.SetNotify(config.Notify{Watched: true})
	var got []string
	m.notifySend = func(n notify.Notice) error { got = append(got, n.Title+": "+n.Body); return nil }
	runCmd(m.handleListLoaded(listLoadedMsg{Page: core.Page{Items: []core.Summary{wf.Summary}}}))
	check := keys(m, "W")
	m.switchNamespace("other")
	// step runs one check: the tick, the read, and the notices it sends.
	step := func() {
		t.Helper()
		var next tea.Cmd
		for _, msg := range runCmd(check) {
			if tick, ok := msg.(watchedTickMsg); ok {
				_, next = m.Update(tick)
			}
		}
		check = nil
		for _, msg := range runCmd(next) {
			if loaded, ok := msg.(watchedLoadedMsg); ok {
				var cmd tea.Cmd
				_, cmd = m.Update(loaded)
				for _, msg := range runCmd(cmd) {
					if tick, ok := msg.(watchedTickMsg); ok {
						check = func() tea.Msg { return tick }
					}
				}
			}
		}
	}

	step()
	if len(got) != 0 {
		t.Fatalf("notified %q before any change", got)
	}
	if !strings.Contains(screen(m), "◉ 1 watched") {
		t.Fatalf("the other namespace's toolbar lacks the watch:\n%s", screen(m))
	}
	done := wf
	done.Summary.Phase = "Succeeded"
	f.Workflows[wf.Summary.Ref] = done
	step()
	delete(f.Workflows, wf.Summary.Ref)
	step()
	if want := "a: Running → Succeeded|a: deleted; no longer watched"; strings.Join(got, "|") != want {
		t.Fatalf("notified %q, want %q", got, want)
	}
	if len(m.watched) != 0 || check != nil {
		t.Fatalf("watched %v, check armed %v after the workflow was deleted", m.watched, check != nil)
	}

	m.watched = map[string]core.Summary{wf.Summary.Ref.UID: wf.Summary}
	m.Adopt(&Connection{Reader: f, Namespace: "ns"})
	if len(m.watched) != 0 {
		t.Fatalf("watched %v after a profile switch, want none", m.watched)
	}
}
