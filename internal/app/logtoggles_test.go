package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/logs"
)

// requestLog records every log request a fake reader receives.
type requestLog struct {
	mu   sync.Mutex
	reqs []core.LogRequest
}

func (r *requestLog) hook(req core.LogRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
	return nil
}

func (r *requestLog) all() []core.LogRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.LogRequest(nil), r.reqs...)
}

// feed delivers every message cmd produces back into the root, once, and
// returns them. Log streams from the fake end on their own, so this
// terminates.
func feed(m *Root, cmd tea.Cmd) []tea.Msg {
	msgs := runCmd(cmd)
	for _, msg := range msgs {
		_, next := m.Update(msg)
		msgs = append(msgs, feed(m, next)...)
	}
	return msgs
}

// demoLogsRoot is a root over the demo dataset with the logs of the
// nightly report open, workflow-wide, and every request recorded.
func demoLogsRoot(t *testing.T) (*Root, *requestLog, core.Ref) {
	t.Helper()
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	rl := &requestLog{}
	f.StreamHook = rl.hook
	m := testRoot(t, f)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	var ref core.Ref
	for _, r := range f.Order {
		if r.Name == "demo-nightly-report" {
			ref = r
		}
	}
	_, cmd := m.Update(OpenLogsMsg{Ref: ref, Container: "main"})
	feed(m, cmd)
	m.View() // sizes the pane to the frame, as a real render does
	return m, rl, ref
}

// Workflow-wide logs are labelled by step: the root reads the workflow's
// node map once and hands the pane the pod-to-step names.
func TestWorkflowLogsAreLabelledBySteps(t *testing.T) {
	m, _, _ := demoLogsRoot(t)
	body := strings.Join(m.logsView.BodyLines(), "\n")
	for _, want := range []string{"transform(0)", "transform(2)", "onExit", "extract"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the pane does not label lines with %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "demo-nightly-report-transform-") {
		t.Fatalf("a line is still labelled by its pod name:\n%s", body)
	}
}

// A sources answer for a pane that has since been replaced is dropped.
func TestAStaleSourcesAnswerIsIgnored(t *testing.T) {
	m, _, ref := demoLogsRoot(t)
	wf, err := m.deps.reader.Get(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	renamed := map[string]string{}
	for pod := range podSources(wf.Nodes) {
		renamed[pod] = "renamed-step"
	}
	m.Update(logSourcesMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen - 1}, Ref: ref, Sources: renamed})
	if strings.Contains(strings.Join(m.logsView.BodyLines(), "\n"), "renamed-step") {
		t.Fatal("a stale sources answer was applied")
	}
	m.Update(logSourcesMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, Ref: ref, Sources: renamed})
	if !strings.Contains(strings.Join(m.logsView.BodyLines(), "\n"), "renamed-step") {
		t.Fatal("a current sources answer was not applied")
	}
}

// ctrl+t reopens the stream with timestamps: the new request carries the
// flag, the retained lines stay, and the switch point is marked.
func TestTimestampsReopenTheStream(t *testing.T) {
	m, rl, ref := demoLogsRoot(t)
	before := len(rl.all())
	if before != 1 || rl.all()[0].Timestamps {
		t.Fatalf("the first request: %+v", rl.all())
	}
	retained := len(m.logsView.RawLines())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	feed(m, cmd)
	reqs := rl.all()
	if len(reqs) != 2 {
		t.Fatalf("ctrl+t sent %d requests, want one more", len(reqs)-before)
	}
	if !reqs[1].Timestamps || reqs[1].Ref != ref || !reqs[1].Follow || reqs[1].Container != "main" {
		t.Fatalf("reopen request = %+v", reqs[1])
	}
	raw := m.logsView.RawLines()
	if len(raw) <= retained {
		t.Fatalf("the reopen dropped lines: %d, had %d", len(raw), retained)
	}
	if raw[retained] != "── server timestamps on: stream reopened from the start ──" {
		t.Fatalf("switch point = %q", raw[retained])
	}
	// The demo stamps the replayed lines the way the API does.
	if !strings.Contains(raw[retained+1], ".137000000Z ") {
		t.Fatalf("replayed line has no server timestamp: %q", raw[retained+1])
	}
	// A container switch keeps the choice.
	_, cmd = m.Update(OpenLogsMsg{Ref: ref, Container: "wait"})
	feed(m, cmd)
	if last := rl.all()[len(rl.all())-1]; !last.Timestamps {
		t.Fatal("switching container dropped the timestamps choice")
	}
}

// Late replies from the stream a reopen replaced are ignored: its batches
// would duplicate lines, and its cancellation would mark the live stream
// canceled.
func TestAReplacedStreamsRepliesAreIgnored(t *testing.T) {
	m, _, _ := demoLogsRoot(t)
	old := m.logState.streamID
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	// Start the reopen without draining it, so the new stream is live.
	intent := cmd()
	_, _ = m.Update(intent)
	if m.logState.streamID == old {
		t.Fatal("the reopen did not start a new stream")
	}
	lines := len(m.logsView.RawLines())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	m.Update(logRecordMsg{genStamp: g, RequestID: old, Records: []core.LogRecord{{Content: "late line"}}})
	m.Update(logRecordMsg{genStamp: g, RequestID: old, Done: true, Canceled: true})
	if got := len(m.logsView.RawLines()); got != lines {
		t.Fatalf("a replaced stream's batch was applied: %d lines, had %d", got, lines)
	}
	if m.logsView.Phase() == logs.PhaseCanceled || !m.logState.running {
		t.Fatal("a replaced stream's cancellation ended the live one")
	}
	m.Update(logRecordMsg{genStamp: g, RequestID: m.logState.streamID, Records: []core.LogRecord{{Content: "live line"}}})
	if got := len(m.logsView.RawLines()); got != lines+1 {
		t.Fatalf("the live stream's batch was not applied: %d lines", got)
	}
}

// With the & filter on, esc clears it and stays in the pane; the next esc
// leaves. q still quits and ? still opens help: the filter is not text
// entry.
func TestEscClearsTheLogFilterBeforeLeaving(t *testing.T) {
	m, _, _ := demoLogsRoot(t)
	for _, k := range []tea.KeyPressMsg{{Code: '/', Text: "/"}, {Code: 'E', Text: "E"}, {Code: 'R', Text: "R"},
		{Code: 'R', Text: "R"}, {Code: tea.KeyEnter}, {Code: '&', Text: "&"}} {
		m.Update(k)
	}
	if !m.logsView.Filtering() {
		t.Fatal("& did not filter")
	}
	if m.textEntryActive() {
		t.Fatal("the filter counts as text entry")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.route != RouteLogs || m.logsView.Filtering() {
		t.Fatalf("esc: route %v, filtering %v", m.route, m.logsView.Filtering())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.route == RouteLogs {
		t.Fatal("the second esc did not leave the pane")
	}
}
