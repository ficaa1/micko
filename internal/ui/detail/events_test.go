package detail

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// eventsModel is the Events section of one demo workflow in the plain
// theme, holding the demo backend's events for the namespace.
func eventsModel(t *testing.T, name string, w, h int) *Model {
	t.Helper()
	m := demoModel(t, name, w, h)
	if !m.SetSection("events") {
		t.Fatal("no events section")
	}
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

// The table holds the workflow's own events and those of the pods its node
// map names, by the node's display name. Another workflow's events are
// dropped; a pod the map does not name yet is kept and shown once it does.
// A resent event replaces itself and a deleted one goes.
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

// s puts warnings first and back; / narrows the rows as it is typed, enter
// keeps the filter, and esc clears it before it leaves the workflow. While
// the filter is open every letter types.
func TestEventsOrderAndFilter(t *testing.T) {
	m := eventsModel(t, "demo-nightly-report", 136, 40)
	first := func() string { return m.eventRows()[0].ev.Reason }
	if first() != "WorkflowFailed" {
		t.Fatalf("newest first starts with %s", first())
	}
	m.handleKey("s")
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
	m.handleKey("s")

	m.handleKey("/")
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
	if cmd := m.handleKey("esc"); cmd != nil || m.ev.filter != "" {
		t.Fatal("esc left the workflow before clearing the filter")
	}
	if m.EscapeConsumed() {
		t.Fatal("esc is still the pane's with no filter")
	}
	m.handleKey("/")
	press(m, "z")
	press(m, "esc")
	if m.ev.filter != "" || m.ev.editing {
		t.Fatal("esc in the open filter did not drop it")
	}
}

// Every line stays inside the pane at every width, in the plain theme and a
// truecolor skin, and the type keeps its glyph when the word gives way.
func TestEventsLinesFitThePane(t *testing.T) {
	for _, name := range []string{"demo-nightly-report", "demo-oom-backfill", "demo-train-pipeline", "demo-param-check"} {
		for _, w := range []int{136, 116, 76, 56, 36} {
			for _, skin := range []string{"plain", "nord"} {
				m := eventsModel(t, name, w, 30)
				if skin != "plain" {
					th, _ := shared.SkinTheme(skin, false)
					m.SetTheme(th)
				}
				for _, l := range m.BodyLines() {
					if cw := ansi.StringWidth(l); cw > w {
						t.Errorf("%s at %d (%s): line is %d cells: %q", name, w, skin, cw, ansi.Strip(l))
					}
				}
			}
		}
	}
	wide := strings.Join(eventsModel(t, "demo-oom-backfill", 136, 30).BodyLines(), "\n")
	if !strings.Contains(wide, "▲ Warning  OOMKilling") || !strings.Contains(wide, "◇ Normal") {
		t.Errorf("the wide table lacks the glyph and word:\n%s", wide)
	}
	narrow := eventsModel(t, "demo-oom-backfill", 56, 30).BodyLines()
	if !strings.HasPrefix(narrow[2], "AGE     T  REASON") || !strings.Contains(strings.Join(narrow, "\n"), "▲  OOMKilling") {
		t.Errorf("the narrow table:\n%s", strings.Join(narrow, "\n"))
	}
}

// The status line counts the events and warnings and says how the stream
// is doing, in the warning style when it needs attention; the empty table
// says why it is empty; pod events are said to be hidden when the server
// did not name the pods.
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
	if got := e.eventsLines(); got[0] != "(connecting to the event stream…)" {
		t.Errorf("before any status: %q", got[0])
	}
	e.SetEventsStatus("live", false)
	if got := e.eventsLines(); !strings.Contains(got[0], "Kubernetes keeps events for about an hour") {
		t.Errorf("live and empty: %q", got[0])
	}
	e.SetEventsStatus("not allowed to watch events in demo", true)
	if got := e.eventsLines(); got[0] != "(no events: not allowed to watch events in demo)" {
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

// The copied table is plain text with every column and message whole.
func TestEventsRawLines(t *testing.T) {
	m := eventsModel(t, "demo-oom-backfill", 56, 30)
	th, _ := shared.SkinTheme("nord", false)
	m.SetTheme(th)
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
	m.handleKey("E")
	in, ok := m.EventsWanted()
	if !ok || in.Ref.Name != "demo-oom-backfill" || m.Section() != "events" {
		t.Fatalf("E: section %q wanted %+v %v", m.Section(), in, ok)
	}
}

// TestEventsGolden pins the section on the pane of a 140- and an 80-column
// terminal in the plain theme, with the demo's events.
func TestEventsGolden(t *testing.T) {
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range []string{"demo-nightly-report", "demo-oom-backfill", "demo-train-pipeline", "demo-param-check"} {
			m := eventsModel(t, name, width-4, 36)
			b.WriteString("== " + name + "\n")
			b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		}
		m := eventsModel(t, "demo-nightly-report", width-4, 36)
		m.handleKey("s")
		m.handleKey("/")
		typeText(m, "backoff")
		press(m, "enter")
		b.WriteString("== demo-nightly-report, warnings first, /backoff\n")
		b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		compareGolden(t, filepath.Join("testdata", "events-"+itoaDetail(width)+".golden"), b.String())
	}
}
