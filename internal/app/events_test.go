package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// running is a command on its own goroutine, so a blocked drain can be resumed later.
type running struct{ out chan tea.Msg }

func start(cmd tea.Cmd) running {
	r := running{out: make(chan tea.Msg, 1)}
	go func() { r.out <- cmd() }()
	return r
}

// pump applies detail and section messages until only commands still blocked after wait remain.
func pump(m *Root, wait time.Duration, cmds ...tea.Cmd) (*Root, []running) {
	var queue []running
	for _, c := range cmds {
		if c != nil {
			queue = append(queue, start(c))
		}
	}
	var left []running
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		select {
		case msg := <-r.out:
			switch msg := msg.(type) {
			case nil:
			case tea.BatchMsg:
				for _, c := range msg {
					if c != nil {
						queue = append(queue, start(c))
					}
				}
			case detailLoadedMsg, eventsMsg, eventsDoneMsg, eventsRetryMsg, explainLogMsg, OpenWorkflowMsg:
				next, c := m.Update(msg)
				m = next.(*Root)
				if c != nil {
					queue = append(queue, start(c))
				}
			}
		case <-time.After(wait):
			left = append(left, r)
		}
	}
	return m, left
}

// resume waits for commands pump left waiting.
func resume(m *Root, wait time.Duration, left []running) (*Root, []running) {
	cmds := make([]tea.Cmd, 0, len(left))
	for _, r := range left {
		r := r
		cmds = append(cmds, func() tea.Msg { return <-r.out })
	}
	return pump(m, wait, cmds...)
}

// openOnEvents presses E on the named workflow and settles both streams' first delivery.
func openOnEvents(t *testing.T, m *Root, name string) (*Root, []running) {
	t.Helper()
	for i := 0; i < 20 && m.listView.SelectedRef().Name != name; i++ {
		typeKeys(m, "j")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'E', Text: "E"})
	m, left := pump(next.(*Root), 150*time.Millisecond, cmd)
	if m.route != RouteDetail || m.detailView.Section() != "events" {
		t.Fatalf("E opened route %v section %q", m.route, m.detailView.Section())
	}
	return m, left
}

// settledStarts is the fake's event stream count once it stops changing.
func settledStarts(f *testkit.FakeReader) int {
	n := f.EventStartCount()
	for {
		time.Sleep(30 * time.Millisecond)
		m := f.EventStartCount()
		if m == n {
			return n
		}
		n = m
	}
}

// E opens the Events section, streaming the workflow's and its pods' events.
func TestListEOpensTheEvents(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	f := m.deps.reader.(*testkit.FakeReader)
	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	reqs := f.EventRequestLog()
	if len(reqs) != 2 {
		t.Fatalf("event streams = %+v, want two", reqs)
	}
	selectors := map[string]bool{}
	for _, r := range reqs {
		if r.Namespace != "demo" || r.ResourceVersion != "" {
			t.Errorf("request = %+v", r)
		}
		selectors[r.FieldSelector] = true
	}
	if !selectors["involvedObject.kind=Workflow,involvedObject.name=demo-oom-backfill"] || !selectors["involvedObject.kind=Pod"] {
		t.Errorf("selectors = %v", selectors)
	}
	s := screen(m)
	for _, want := range []string{"[Events]", "▲ Warning  OOMKilling", "backfill-2026-q2", "WorkflowFailed", "events · 8 events · 3 warnings", "live"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "transform") {
		t.Errorf("another workflow's pod events are on screen:\n%s", s)
	}
}

func TestEventsStreamLive(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	f := m.deps.reader.(*testkit.FakeReader)
	m, left := openOnEvents(t, m, "demo-oom-backfill")
	var pod string
	for _, n := range m.detailState.workflow.Nodes {
		if n.PodName != "" {
			pod = n.PodName
		}
	}
	now := testkit.FixtureEpoch
	f.PublishEvent(core.Event{UID: "live-1", Namespace: "demo", Type: "Warning", Reason: "Evicted",
		Message: "The node was low on resource: memory.", ObjectKind: "Pod", ObjectName: pod, Count: 1,
		FirstSeen: now, LastSeen: now})
	m, _ = resume(m, 150*time.Millisecond, left)
	if s := screen(m); !strings.Contains(s, "Evicted") || !strings.Contains(s, "The node was low on resource: memory.") {
		t.Fatalf("the live event is not on screen:\n%s", s)
	}
}

// Event streams resume after a section change and start fresh after leaving the workflow.
func TestEventsStreamsAreCanceledWhenLeft(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	f := m.deps.reader.(*testkit.FakeReader)
	m, left := openOnEvents(t, m, "demo-oom-backfill")
	if !m.hasInflight("events-workflow") || !m.hasInflight("events-pods") {
		t.Fatal("the streams are not in flight")
	}
	var pod string
	for _, n := range m.detailState.workflow.Nodes {
		if n.PodName != "" {
			pod = n.PodName
		}
	}
	f.PublishEvent(core.Event{UID: "rv-w", Namespace: "demo", ObjectKind: "Workflow", ObjectName: "demo-oom-backfill", ResourceVersion: "701"})
	f.PublishEvent(core.Event{UID: "rv-p", Namespace: "demo", ObjectKind: "Pod", ObjectName: pod, ResourceVersion: "702"})
	m, _ = resume(m, 150*time.Millisecond, left)
	cursors := func(reqs []core.EventWatchRequest) map[string]string {
		out := map[string]string{}
		for _, r := range reqs {
			out[r.FieldSelector] = r.ResourceVersion
		}
		return out
	}
	typeKeys(m, "1")
	if m.hasInflight("events-workflow") || m.hasInflight("events-pods") || m.events.active {
		t.Fatal("leaving the section left a stream running")
	}
	waitFor(t, "both streams to end", func() bool { return f.EventCancelCount() == 2 })

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'E', Text: "E"})
	m, _ = pump(next.(*Root), 150*time.Millisecond, cmd)
	if f.EventStartCount() != 4 || !m.events.active {
		t.Fatalf("coming back opened %d streams in all", f.EventStartCount())
	}
	want := map[string]string{"involvedObject.kind=Workflow,involvedObject.name=demo-oom-backfill": "701", "involvedObject.kind=Pod": "702"}
	if got := cursors(f.EventRequestLog()[2:]); !reflect.DeepEqual(got, want) {
		t.Errorf("coming back to the section asked for %v, want the cursors %v", got, want)
	}
	if !strings.Contains(screen(m), "OOMKilling") {
		t.Errorf("the events are gone after coming back:\n%s", screen(m))
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteList || m.hasInflight("events-workflow") || m.hasInflight("events-pods") {
		t.Fatalf("esc: route %v, streams in flight", m.route)
	}
	waitFor(t, "the streams to end after esc", func() bool { return f.EventCancelCount() == 4 })

	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	for sel, rv := range cursors(f.EventRequestLog()[4:]) {
		if rv != "" {
			t.Errorf("reopening the workflow resumed %s from %q, want a fresh start", sel, rv)
		}
	}
}

func TestEventsDropStaleReplies(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	st := m.events.streams[eventStreamWorkflow]
	g := genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}
	stale := core.Event{UID: "stale", Namespace: "demo", Type: "Warning", Reason: "StaleReason", ObjectKind: "Workflow",
		ObjectName: "demo-oom-backfill", LastSeen: testkit.FixtureEpoch, Count: 1}
	for _, old := range []genStamp{
		{Conn: g.Conn, Sel: g.Sel, Attempt: g.Attempt - 1},
		{Conn: g.Conn, Sel: g.Sel - 1, Attempt: g.Attempt},
		{Conn: g.Conn - 1, Sel: g.Sel, Attempt: g.Attempt},
	} {
		next, _ := m.Update(eventsMsg{genStamp: old, Stream: eventStreamWorkflow, Events: []core.Event{stale}})
		m = next.(*Root)
		next, cmd := m.Update(eventsDoneMsg{genStamp: old, Stream: eventStreamWorkflow, Err: core.NewWatchError(core.WatchEnded, "eof", "", nil)})
		m = next.(*Root)
		if cmd != nil {
			t.Errorf("a stale end %+v scheduled a reconnect", old)
		}
	}
	if strings.Contains(screen(m), "StaleReason") || m.events.streams[eventStreamWorkflow].state != eventLive {
		t.Fatalf("a stale reply changed the section:\n%s", screen(m))
	}
	next, _ := m.Update(eventsMsg{genStamp: g, Stream: eventStreamWorkflow, Events: []core.Event{stale}})
	m = next.(*Root)
	if !strings.Contains(screen(m), "StaleReason") {
		t.Fatal("the current stream's event was dropped")
	}
}

// An ended stream reconnects from its cursor after a back-off, and stops on permission errors, a missing stream or too many failures.
func TestEventsReconnectRules(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	f := m.deps.reader.(*testkit.FakeReader)
	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	done := func(i int, err error) tea.Cmd {
		st := m.events.streams[i]
		next, cmd := m.Update(eventsDoneMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}, Stream: i, Err: err})
		m = next.(*Root)
		return cmd
	}
	retry := func(i int) {
		st := m.events.streams[i]
		next, cmd := m.Update(eventsRetryMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}, Stream: i})
		m, _ = pump(next.(*Root), 100*time.Millisecond, cmd)
	}
	last := func() core.EventWatchRequest {
		log := f.EventRequestLog()
		return log[len(log)-1]
	}

	if cmd := done(eventStreamPods, core.NewWatchError(core.WatchEnded, "eof", "777", nil)); cmd == nil {
		t.Fatal("an ended stream schedules no reconnect")
	}
	if s := screen(m); !strings.Contains(s, "reconnecting in 1s") {
		t.Errorf("the status does not say it reconnects:\n%s", s)
	}
	retry(eventStreamPods)
	if r := last(); r.FieldSelector != "involvedObject.kind=Pod" || r.ResourceVersion != "777" {
		t.Errorf("reconnect request = %+v, want the pod stream from 777", r)
	}
	if !strings.Contains(screen(m), "live") {
		t.Errorf("reconnected stream not live:\n%s", screen(m))
	}

	cmd := done(eventStreamPods, core.NewWatchError(core.WatchExpired, "too old", "777", nil))
	m, _ = pump(m, 100*time.Millisecond, cmd)
	if r := last(); r.ResourceVersion != "" {
		t.Errorf("an expired cursor was resumed: %+v", r)
	}

	if cmd := done(eventStreamWorkflow, core.NewWatchError(core.WatchEnded, "denied", "", core.NewAPIError(core.ErrForbidden, 403, "events is forbidden"))); cmd != nil {
		t.Error("a permission error schedules a reconnect")
	}
	if s := screen(m); !strings.Contains(s, "not allowed to watch events in demo") {
		t.Errorf("the status does not say why it stopped:\n%s", s)
	}

	// Reconnects that deliver nothing count toward the limit.
	for i := 0; i <= maxWatchRetries; i++ {
		if cmd := done(eventStreamPods, core.NewWatchError(core.WatchEnded, "eof", "", nil)); cmd == nil {
			break
		}
		st := m.events.streams[eventStreamPods]
		next, _ := m.Update(eventsRetryMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen, Attempt: st.attempt}, Stream: eventStreamPods})
		m = next.(*Root)
	}
	if st := m.events.streams[eventStreamPods]; st.state != eventStopped || !strings.Contains(st.reason, "failed reconnects (r retries)") {
		t.Fatalf("after %d failures: state %v reason %q", maxWatchRetries+1, st.state, st.reason)
	}

	before := settledStarts(f)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m, _ = pump(next.(*Root), 150*time.Millisecond, cmd)
	if n := settledStarts(f) - before; n != 2 || m.events.streams[eventStreamPods].state != eventLive ||
		m.events.streams[eventStreamWorkflow].state != eventLive {
		t.Fatalf("r did not start both streams over: %d starts", n)
	}

	if cmd := done(eventStreamWorkflow, core.NewWatchError(core.WatchUnsupported, "Not Found", "", core.NewAPIError(core.ErrNotFound, 404, "Not Found"))); cmd != nil {
		t.Error("a server without the stream schedules a reconnect")
	}
	if s := screen(m); !strings.Contains(s, "this Argo server does not stream events") {
		t.Errorf("the status does not say the server has no stream:\n%s", s)
	}
}

// A backend without an event stream says so instead of spinning.
func TestEventsWithoutAWatcher(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m.deps.eventWatcher = nil
	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	if s := screen(m); !strings.Contains(s, "this backend has no event stream") {
		t.Fatalf("screen:\n%s", s)
	}
}

// y on the section copies the whole table as text.
func TestEventsCopy(t *testing.T) {
	m := resize(t, loadDemoList(t), 100, 40)
	m, _ = openOnEvents(t, m, "demo-oom-backfill")
	got := m.copyText()
	if !strings.HasPrefix(got, "AGE") || !strings.Contains(got, "Memory cgroup out of memory: Killed process 4127 (python) total-vm:2621440kB, anon-rss:2097152kB") {
		t.Fatalf("copied:\n%s", got)
	}
}
