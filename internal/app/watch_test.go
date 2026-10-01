package app

import (
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// summary is a running workflow in "ns" with the given name.
func summary(name string) core.Summary {
	return core.Summary{Ref: core.Ref{Namespace: "ns", Name: name, UID: "uid-" + name}, Phase: "Running"}
}

// A watch event updates the snapshot by UID within the cap.
func TestWatchEvents(t *testing.T) {
	succeeded := summary("a")
	succeeded.Phase = "Succeeded"
	for _, c := range []struct {
		name       string
		cap        int
		attempt    uint64
		event      core.WatchEvent
		phases     string
		rv         string
		incomplete bool
	}{
		{"modified", 5000, 0, core.WatchEvent{Type: core.WatchModified, Summary: succeeded, ResourceVersion: "rv-2"}, "a=Succeeded,b=Running", "rv-2", false},
		{"bookmark", 5000, 0, core.WatchEvent{Type: core.WatchBookmark, ResourceVersion: "rv-3"}, "a=Running,b=Running", "rv-3", false},
		{"replaced watch", 5000, 1, core.WatchEvent{Type: core.WatchModified, Summary: succeeded, ResourceVersion: "rv-2"}, "a=Running,b=Running", "", false},
		{"added past the cap", 2, 0, core.WatchEvent{Type: core.WatchAdded, Summary: summary("c")}, "a=Running,b=Running", "", true},
		{"updated at the cap", 2, 0, core.WatchEvent{Type: core.WatchModified, Summary: succeeded}, "a=Succeeded,b=Running", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader())
			m.deps.snapshotCap = c.cap
			m.listState.items = []core.Summary{summary("a"), summary("b")}
			m.watchAttempt = 2
			if c.attempt == 0 {
				c.attempt = m.watchAttempt
			}
			m.Update(watchEventMsg{genStamp: genStamp{Attempt: c.attempt}, Event: c.event})
			var got []string
			for _, s := range m.listState.items {
				got = append(got, s.Ref.Name+"="+s.Phase)
			}
			if strings.Join(got, ",") != c.phases || m.watchRV != c.rv || m.listState.incomplete != c.incomplete {
				t.Fatalf("snapshot %v rv %q incomplete %v, want %s rv %q incomplete %v",
					got, m.watchRV, m.listState.incomplete, c.phases, c.rv, c.incomplete)
			}
		})
	}
}

// A permission error stops the watch; a rate limit delays the relist.
func TestWatchEndings(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   *core.APIError
		retry bool
		mode  string
	}{
		{"unauthorized", core.NewAPIError(core.ErrUnauthenticated, 401, "unauthorized"), false, "authentication"},
		{"rate limited", core.NewAPIError(core.ErrRateLimited, 429, "slow down"), true, "rate limited"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader())
			_, cmd := m.Update(watchDoneMsg{Err: core.NewWatchError(core.WatchEnded, c.err.Message, "", c.err)})
			if (cmd != nil) != c.retry || !strings.Contains(m.watchMode, c.mode) {
				t.Fatalf("retry scheduled %v, mode %q", cmd != nil, m.watchMode)
			}
		})
	}
}

// Nothing queued restarts a stopped watch.
func TestNothingQueuedRestartsAStoppedWatch(t *testing.T) {
	const denied = "authentication/permission required"
	for _, c := range []struct {
		name string
		mode string
		msg  func(m *Root) any
	}{
		{"tick", denied, func(*Root) any { return tickMsg{} }},
		{"list reply", denied, func(*Root) any { return listLoadedMsg{Page: core.Page{Items: []core.Summary{summary("wf")}}} }},
		{"replaced watch's retry", "rate limited", func(m *Root) any { return watchRetryMsg{genStamp: genStamp{Attempt: m.watchAttempt - 1}} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testRoot(t, fixtureReader())
			// A watcher, so that a restart would be a command.
			m.deps.watcher = &recordingWatcher{req: make(chan core.WatchRequest, 1)}
			m.watchMode = c.mode
			m.watchAttempt = 2
			if _, cmd := m.Update(c.msg(m)); cmd != nil {
				t.Fatal("a queued message restarted the watch")
			}
		})
	}
}

// r clears the stopped state and collects a new list.
func TestRefreshClearsTerminalWatchState(t *testing.T) {
	m := testRoot(t, fixtureReader())
	m.watchMode = "authentication/permission required"
	cmd := keys(m, "r")
	if cmd == nil {
		t.Fatal("r produced nothing")
	}
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if !m.listState.loading || m.watchMode != "" {
		t.Fatalf("loading %v, watch mode %q", m.listState.loading, m.watchMode)
	}
}

// Retry delays back off, are capped, and honour Retry-After.
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

// A watch event for the workflow open in the detail view refetches it. The
// watch belongs to the list's scope, so opening the workflow does not make
// its events stale.
func TestWatchEventRefetchesTheOpenWorkflow(t *testing.T) {
	m := testRoot(t, fixtureReader(workflowFixture("a")))
	m.listState.items = []core.Summary{summary("a")}
	m.watchAttempt = 2
	stamp := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: m.watchAttempt}
	m.openWorkflow(summary("a").Ref)
	m.cancelInflight("detail")
	m.detailState.loading = false

	m.Update(watchEventMsg{genStamp: stamp, Event: core.WatchEvent{Type: core.WatchModified, Summary: summary("a")}})
	if !m.hasInflight("detail") {
		t.Fatal("a change to the open workflow started no detail fetch")
	}
}
