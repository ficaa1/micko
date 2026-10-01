package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
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

// demoLogsRoot opens the nightly report's logs, recording every request.
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
	deliver(m, cmd)
	m.View() // sizes the pane to the frame, as a real render does
	return m, rl, ref
}

// Workflow-wide log lines are labelled by step, not pod.
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

// The stream follows; ctrl+t reopens it with timestamps and marks the switch.
func TestTimestampsReopenTheStream(t *testing.T) {
	m, rl, ref := demoLogsRoot(t)
	before := len(rl.all())
	if before != 1 || rl.all()[0].Timestamps || !rl.all()[0].Follow {
		t.Fatalf("the first request: %+v", rl.all())
	}
	retained := len(m.logsView.RawLines())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	deliver(m, cmd)
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
	deliver(m, cmd)
	if last := rl.all()[len(rl.all())-1]; !last.Timestamps {
		t.Fatal("switching container dropped the timestamps choice")
	}
}

// A replaced stream's late batches and cancellation are ignored.
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
	if strings.HasPrefix(m.logsView.PaneStatus(), "CANCELED") || !m.logState.running {
		t.Fatal("a replaced stream's cancellation ended the live one")
	}
	m.Update(logRecordMsg{genStamp: g, RequestID: m.logState.streamID, Records: []core.LogRecord{{Content: "live line"}}})
	if got := len(m.logsView.RawLines()); got != lines+1 {
		t.Fatalf("the live stream's batch was not applied: %d lines", got)
	}
}

// Logs opened without a container read "main".
func TestLogsDefaultToTheMainContainer(t *testing.T) {
	rl := &requestLog{}
	f := fixtureReader()
	f.StreamHook = rl.hook
	m := testRoot(t, f)
	_, cmd := m.Update(OpenLogsMsg{Ref: core.Ref{Namespace: "ns", Name: "wf-1"}})
	deliver(m, cmd)
	if reqs := rl.all(); len(reqs) != 1 || reqs[0].Container != "main" {
		t.Fatalf("requests = %+v, want one for main", reqs)
	}
}

// Every record reaches the pane and the title counts them.
func TestAStreamIsDeliveredWhole(t *testing.T) {
	f := &testkit.FakeReader{StreamSequence: make([]core.LogRecord, 1000)}
	for i := range f.StreamSequence {
		f.StreamSequence[i] = core.LogRecord{Content: "line", ReceivedAt: testkit.FixtureEpoch}
	}
	m := testRoot(t, f)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	_, cmd := m.Update(OpenLogsMsg{Ref: core.Ref{Namespace: "ns", Name: "wf-1"}, Container: "main"})
	deliver(m, cmd)
	if s := screen(m); !strings.Contains(s, "stream ended (1000 records)") {
		t.Fatalf("the title does not count 1000 records:\n%s", s)
	}
}

// A stream whose context is canceled ends as canceled, not as a failure.
func TestAStreamCancellationIsNotAFailure(t *testing.T) {
	f := &testkit.FakeReader{
		StreamDelay:    time.Hour,
		StreamSequence: []core.LogRecord{{Content: "only-one"}, {Content: "never"}},
	}
	m := testRoot(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := m.deps.streamLogsCmd(ctx, genStamp{Conn: 1, Sel: 1}, 1, core.LogRequest{Ref: core.Ref{Namespace: "ns", Name: "wf"}, Container: "main"})
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	for cmd != nil {
		lm, ok := cmd().(logRecordMsg)
		if !ok {
			break
		}
		if lm.Done {
			if !lm.Canceled || (lm.Err != nil && !errors.Is(lm.Err, context.Canceled)) {
				t.Fatalf("end = canceled %v, err %v", lm.Canceled, lm.Err)
			}
			return
		}
		cmd = lm.Next
	}
	t.Fatal("the stream never ended")
}

// Leaving the logs cancels the stream on the server side too.
func TestLeavingTheLogsCancelsTheStream(t *testing.T) {
	wf := workflowFixture("wf-1")
	f := fixtureReader(wf)
	f.StreamDelay = time.Hour
	f.StreamSequence = []core.LogRecord{{Content: "x"}, {Content: "y"}}
	m := testRoot(t, f)
	_, cmd := m.Update(OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"})
	go runCmd(cmd)
	m.Update(BackMsg{})
	if m.logState.running {
		t.Error("the stream is still marked running")
	}
	waitFor(t, "the stream to be canceled", func() bool { return f.StreamCancelCount() > 0 })
}
