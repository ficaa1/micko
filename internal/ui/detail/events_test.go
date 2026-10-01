package detail

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// eventsModel is the Events section of a demo workflow holding the demo's events.
func eventsModel(t *testing.T, name string, w, h int) *Model {
	t.Helper()
	m := sectionModel(t, name, "events", w, h)
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	m.ApplyEvents(r.Events)
	m.SetEventsStatus("live", false)
	return m
}

func ev(uid, kind, name, typ, reason string, ago time.Duration) core.Event {
	at := testkit.FixtureEpoch.Add(-ago)
	return core.Event{UID: uid, Namespace: "demo", ObjectKind: kind, ObjectName: name, Type: typ, Reason: reason,
		Message: reason + " happened", Count: 1, FirstSeen: at, LastSeen: at}
}

func rowObjects(m *Model) []string {
	var out []string
	for _, r := range m.eventRows() {
		out = append(out, r.object+"/"+r.ev.Reason)
	}
	return out
}

// The table holds the workflow's events and its named pods', replaces a resent event and drops a deleted one.
func TestEventsMatchTheWorkflowAndItsPods(t *testing.T) {
	m := demoModel(t, "demo-oom-backfill", 136, 30)
	m.SetSection("events")
	wf := demoWorkflow(t, "demo-oom-backfill")
	var pod string
	for _, n := range wf.Nodes {
		if n.PodName != "" {
			pod = n.PodName
		}
	}
	m.ApplyEvents([]core.Event{
		ev("1", "Workflow", "demo-oom-backfill", "Normal", "WorkflowRunning", 10*time.Minute),
		ev("2", "Pod", pod, "Warning", "OOMKilling", 5*time.Minute),
		ev("3", "Workflow", "demo-oom-backfill-2", "Warning", "WorkflowFailed", time.Minute),
		ev("4", "Pod", "other-workflow-main-1", "Normal", "Scheduled", time.Minute),
		ev("5", "Pod", "demo-oom-backfill-next-77", "Normal", "Scheduled", 2*time.Minute),
		ev("6", "Node", "demo-worker-2", "Warning", "OOMKilling", time.Minute),
	})
	if got := strings.Join(rowObjects(m), " "); got != "backfill-2026-q2/OOMKilling workflow/WorkflowRunning" {
		t.Fatalf("rows = %s", got)
	}
	if len(m.ev.byUID) != 3 {
		t.Errorf("kept %d events, want the workflow's, its pod's and the pod not yet named", len(m.ev.byUID))
	}

	n := wf.Nodes
	n["next"] = core.Node{ID: "next", Name: "demo-oom-backfill.next", DisplayName: "next", Type: "Pod", Phase: "Pending",
		PodName: "demo-oom-backfill-next-77"}
	m.SetWorkflow(wf, testkit.FixtureEpoch)
	if got := strings.Join(rowObjects(m), " "); got != "next/Scheduled backfill-2026-q2/OOMKilling workflow/WorkflowRunning" {
		t.Fatalf("after the node arrived, rows = %s", got)
	}

	again := ev("2", "Pod", pod, "Warning", "OOMKilling", 30*time.Second)
	again.Count = 4
	gone := ev("1", "Workflow", "demo-oom-backfill", "Normal", "WorkflowRunning", 10*time.Minute)
	gone.Deleted = true
	m.ApplyEvents([]core.Event{again, gone})
	rows := m.eventRows()
	if len(rows) != 2 || rows[0].ev.Count != 4 || rows[0].ev.Reason != "OOMKilling" {
		t.Fatalf("after a resend and a delete, rows = %v", rowObjects(m))
	}

	m.SetWorkflow(demoWorkflow(t, "demo-nightly-report"), testkit.FixtureEpoch)
	if len(m.ev.byUID) != 0 {
		t.Error("another workflow kept the previous one's events")
	}
}

// s puts warnings first; / filters as it is typed, and esc clears the filter before it leaves.
func TestEventsOrderAndFilter(t *testing.T) {
	m := eventsModel(t, "demo-nightly-report", 136, 40)
	first := func() string { return m.eventRows()[0].ev.Reason }
	if first() != "WorkflowFailed" {
		t.Fatalf("newest first starts with %s", first())
	}
	press(m, "s")
	rows := m.eventRows()
	seenNormal := false
	for _, r := range rows {
		if r.ev.Type != "Warning" {
			seenNormal = true
		} else if seenNormal {
			t.Fatalf("a warning after a normal event with warnings first: %v", rowObjects(m))
		}
	}
	if !strings.Contains(m.eventsStatusLine(), "warnings first") || !strings.Contains(m.Hints(), "s newest first") {
		t.Errorf("status %q hints %q", m.eventsStatusLine(), m.Hints())
	}
	press(m, "s")

	press(m, "/")
	if !m.TextEntry() || !m.EscapeConsumed() {
		t.Fatal("the open filter is not text entry")
	}
	typeText(m, "backq")
	if m.ev.filter != "backq" {
		t.Fatalf("filter = %q", m.ev.filter)
	}
	press(m, "backspace")
	press(m, "enter")
	if m.TextEntry() || m.ev.filter != "back" {
		t.Fatalf("enter: editing %v filter %q", m.ev.editing, m.ev.filter)
	}
	for _, r := range m.eventRows() {
		if r.ev.Reason != "BackOff" {
			t.Errorf("/back kept %s", r.ev.Reason)
		}
	}
	if len(m.eventRows()) != 3 || !strings.Contains(m.eventsStatusLine(), "3 events · /back") {
		t.Errorf("status %q", m.eventsStatusLine())
	}
	if cmd := press(m, "esc"); cmd != nil || m.ev.filter != "" {
		t.Fatal("esc left the workflow before clearing the filter")
	}
	if m.EscapeConsumed() {
		t.Fatal("esc is still the pane's with no filter")
	}
	press(m, "/")
	press(m, "z")
	press(m, "esc")
	if m.ev.filter != "" || m.ev.editing {
		t.Fatal("esc in the open filter did not drop it")
	}
}

// The type is a glyph and a word, and keeps its glyph on a narrow pane.
func TestEventsTypeKeepsItsGlyph(t *testing.T) {
	wide := strings.Join(eventsModel(t, "demo-oom-backfill", 136, 30).BodyLines(), "\n")
	if !strings.Contains(wide, "▲ Warning  OOMKilling") || !strings.Contains(wide, "◇ Normal") {
		t.Errorf("the wide table lacks the glyph and word:\n%s", wide)
	}
	narrow := eventsModel(t, "demo-oom-backfill", 56, 30).BodyLines()
	if !strings.HasPrefix(narrow[2], "AGE     T  REASON") || !strings.Contains(strings.Join(narrow, "\n"), "▲  OOMKilling") {
		t.Errorf("the narrow table:\n%s", strings.Join(narrow, "\n"))
	}
}

// The status line counts events and says how the stream is doing; an empty table says why.
func TestEventsStatusAndEmpty(t *testing.T) {
	m := eventsModel(t, "demo-oom-backfill", 136, 40)
	if s := m.eventsStatusLine(); s != "events · 8 events · 3 warnings · newest first · live" {
		t.Errorf("status %q", s)
	}
	m.SetEventsStatus("reconnecting in 2s", true)
	if s := m.eventsStatusLine(); !strings.HasSuffix(s, "reconnecting in 2s") {
		t.Errorf("status %q", s)
	}

	e := demoModel(t, "demo-cleanup", 136, 40)
	e.SetSection("events")
	if got := e.eventsLines(0, 1); got[0] != "(connecting to the event stream…)" {
		t.Errorf("before any status: %q", got[0])
	}
	e.SetEventsStatus("live", false)
	if got := e.eventsLines(0, 1); !strings.Contains(got[0], "Kubernetes keeps events for about an hour") {
		t.Errorf("live and empty: %q", got[0])
	}
	e.SetEventsStatus("not allowed to watch events in demo", true)
	if got := e.eventsLines(0, 1); got[0] != "(no events: not allowed to watch events in demo)" {
		t.Errorf("stopped: %q", got[0])
	}

	wf := demoWorkflow(t, "demo-oom-backfill")
	for id, n := range wf.Nodes {
		n.PodName = ""
		wf.Nodes[id] = n
	}
	u := New()
	u.SetTheme(shared.NewTheme(true))
	u.SetSize(136, 40)
	u.SetWorkflow(wf, testkit.FixtureEpoch)
	u.SetSection("events")
	if s := u.eventsStatusLine(); !strings.Contains(s, "pod events hidden: the server did not name the pods") {
		t.Errorf("unnamed pods: %q", s)
	}
}

// Past the cap the oldest events go.
func TestEventsAreCapped(t *testing.T) {
	m := demoModel(t, "demo-oom-backfill", 136, 30)
	m.SetSection("events")
	var evs []core.Event
	for i := 0; i < eventsCap+50; i++ {
		evs = append(evs, ev(fmt.Sprint(i), "Workflow", "demo-oom-backfill", "Normal", "Tick", time.Duration(eventsCap+50-i)*time.Second))
	}
	m.ApplyEvents(evs)
	if len(m.ev.byUID) != eventsCap {
		t.Fatalf("kept %d events", len(m.ev.byUID))
	}
	if _, ok := m.ev.byUID["0"]; ok {
		t.Fatal("the oldest event survived the cap")
	}
	if _, ok := m.ev.byUID[fmt.Sprint(eventsCap+49)]; !ok {
		t.Fatal("the newest event was dropped")
	}
}

// A frame draws only the rows on screen, so it costs the same with eventsCap
// events kept as with a screenful. Each streamed batch redraws the frame, and
// keys wait behind it.
func TestEventsFrameCostDoesNotGrowWithEvents(t *testing.T) {
	frame := func(n int) float64 {
		m := demoModel(t, "demo-oom-backfill", 136, 30)
		m.SetSection("events")
		var evs []core.Event
		for i := 0; i < n; i++ {
			evs = append(evs, ev(fmt.Sprint(i), "Workflow", "demo-oom-backfill", "Normal", "Tick", time.Duration(i)*time.Second))
		}
		m.ApplyEvents(evs)
		m.BodyLines()
		return testing.AllocsPerRun(5, func() { m.BodyLines() })
	}
	screenful, full := frame(30), frame(eventsCap)
	if full > 2*screenful {
		t.Errorf("a frame allocates %.0f times with %d events and %.0f with 30, want about the same", full, eventsCap, screenful)
	}
}

// The copied table is plain text with every column and message whole.
func TestEventsRawLines(t *testing.T) {
	m := eventsModel(t, "demo-oom-backfill", 56, 30)
	m.SetTheme(skin(t))
	raw := m.RawLines()
	if !strings.HasPrefix(raw[0], "AGE     TYPE       REASON") || len(raw) != 9 {
		t.Fatalf("raw:\n%s", strings.Join(raw, "\n"))
	}
	joined := strings.Join(raw, "\n")
	if strings.Contains(joined, "\x1b") || strings.Contains(joined, "…") ||
		!strings.Contains(joined, "Memory cgroup out of memory: Killed process 4127 (python) total-vm:2621440kB, anon-rss:2097152kB") {
		t.Fatalf("raw is styled or cut:\n%s", joined)
	}
	if _, ok := m.SelectedNode(); ok {
		t.Error("the section selects a node, so y would copy a pod name")
	}
}

// EventsWanted asks for the workflow on screen, and only on the section.
func TestEventsWanted(t *testing.T) {
	m := demoModel(t, "demo-oom-backfill", 136, 30)
	if _, ok := m.EventsWanted(); ok {
		t.Fatal("the nodes tab wants events")
	}
	press(m, "E")
	in, ok := m.EventsWanted()
	if !ok || in.Ref.Name != "demo-oom-backfill" || m.Section() != "events" {
		t.Fatalf("E: section %q wanted %+v %v", m.Section(), in, ok)
	}
}

// The Events section at 140 and 80 columns, with the demo's events.
func TestEventsGolden(t *testing.T) {
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range []string{"demo-nightly-report", "demo-oom-backfill", "demo-train-pipeline", "demo-param-check"} {
			m := eventsModel(t, name, width-4, 36)
			b.WriteString("== " + name + "\n")
			b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		}
		m := eventsModel(t, "demo-nightly-report", width-4, 36)
		press(m, "s")
		press(m, "/")
		typeText(m, "backoff")
		press(m, "enter")
		b.WriteString("== demo-nightly-report, warnings first, /backoff\n")
		b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		golden(t, "events-"+strconv.Itoa(width), b.String())
	}
}
