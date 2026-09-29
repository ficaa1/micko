package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// recordStreams makes the demo backend record every log request it serves.
func recordStreams(m *Root) (*testkit.FakeReader, func() []core.LogRequest) {
	f := m.deps.reader.(*testkit.FakeReader)
	var mu sync.Mutex
	var reqs []core.LogRequest
	f.StreamHook = func(r core.LogRequest) error {
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, r)
		return nil
	}
	return f, func() []core.LogRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]core.LogRequest(nil), reqs...)
	}
}

// openOnExplain presses X on the named workflow and settles the log read.
func openOnExplain(t *testing.T, m *Root, name string) *Root {
	t.Helper()
	for i := 0; i < 20 && m.listView.SelectedRef().Name != name; i++ {
		typeKeys(m, "j")
	}
	if got := m.listView.SelectedRef().Name; got != name {
		t.Fatalf("precondition: cursor on %q, want %q", got, name)
	}
	settle(m, keys(m, "X"))
	if m.route != RouteDetail || m.detailView.Section() != "explain" {
		t.Fatalf("X opened route %v section %q", m.route, m.detailView.Section())
	}
	return m
}

// X opens the Explain section, which reads the failing pod's last 200 lines once.
func TestListXOpensTheExplanation(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	_, reqs := recordStreams(m)
	m = openOnExplain(t, m, "demo-nightly-report")
	got := reqs()
	if len(got) != 1 {
		t.Fatalf("log requests = %+v, want one", got)
	}
	r := got[0]
	wf := m.detailState.workflow
	var pod string
	for _, n := range wf.Nodes {
		if n.DisplayName == "transform(2)" {
			pod = n.PodName
		}
	}
	if r.PodName != pod || r.Container != "main" || r.TailLines != 200 || r.Follow || r.Ref != m.selection {
		t.Errorf("request = %+v, want pod %s, main, 200 lines, no follow", r, pod)
	}
	s := screen(m)
	for _, want := range []string{"[Explain]", "✗ ERROR  transform failed all 3 attempts", "SchemaViolation: revenue_eur", "explain · 1 error · 2 info"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if m.hasInflight("explain") {
		t.Error("the finished read is still in flight")
	}

	// A refresh of the same workflow reads nothing again.
	settle(m, keys(m, "r"))
	if n := len(reqs()); n != 1 {
		t.Errorf("a refresh read the log again: %d requests", n)
	}
}

// Leaving the section or the workflow cancels the read; coming back reads again.
func TestExplainReadIsCanceledWhenLeft(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openFromList(t, m, "demo-oom-backfill", tea.KeyPressMsg{Code: tea.KeyEnter})
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = next.(*Root)
	if m.detailView.Section() != "explain" || !m.hasInflight("explain") || cmd == nil {
		t.Fatalf("X in detail: section %q, in flight %v", m.detailView.Section(), m.hasInflight("explain"))
	}
	if !strings.Contains(screen(m), "reading the log of backfill-2026-q2…") {
		t.Errorf("the section does not say it is reading:\n%s", screen(m))
	}

	typeKeys(m, "1")
	if m.hasInflight("explain") {
		t.Fatal("leaving the section left the read running")
	}
	// The canceled read answers as canceled, and changes nothing.
	for _, msg := range runCmd(cmd) {
		lm, ok := msg.(explainLogMsg)
		if !ok {
			continue
		}
		if !lm.Canceled {
			t.Errorf("the read was not canceled: %+v", lm)
		}
		next, _ = m.Update(lm)
		m = next.(*Root)
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = next.(*Root)
	if !m.hasInflight("explain") || cmd == nil {
		t.Fatal("coming back to the section does not read again")
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = next.(*Root)
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(explainLogMsg); ok {
			continue
		}
		next, _ = m.Update(msg)
		m = next.(*Root)
		break
	}
	if m.route != RouteLogs || m.hasInflight("explain") {
		t.Fatalf("l on the section: route %v, read in flight %v", m.route, m.hasInflight("explain"))
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteDetail || !m.hasInflight("explain") {
		t.Fatalf("back from the log: route %v, read in flight %v", m.route, m.hasInflight("explain"))
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(*Root)
	if m.route != RouteList || m.hasInflight("explain") {
		t.Fatalf("esc: route %v, read in flight %v", m.route, m.hasInflight("explain"))
	}
}

func TestExplainDropsStaleReplies(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openFromList(t, m, "demo-oom-backfill", tea.KeyPressMsg{Code: tea.KeyEnter})
	next, _ := m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = next.(*Root)
	id := m.ids.next
	g := genStamp{Conn: m.connGen, Sel: m.selGen}

	apply := func(msg explainLogMsg) {
		next, _ := m.Update(msg)
		m = next.(*Root)
	}
	apply(explainLogMsg{genStamp: genStamp{Conn: g.Conn, Sel: g.Sel - 1}, RequestID: id, Lines: []string{"ERROR from another workflow"}})
	apply(explainLogMsg{genStamp: g, RequestID: id - 1, Lines: []string{"ERROR from an older request"}})
	if s := screen(m); strings.Contains(s, "from another workflow") || strings.Contains(s, "from an older request") {
		t.Fatalf("a stale reply reached the screen:\n%s", s)
	}
	apply(explainLogMsg{genStamp: g, RequestID: id, Lines: []string{"working", "Killed"}})
	if s := screen(m); !strings.Contains(s, "> 2  Killed") {
		t.Fatalf("the current reply did not reach the screen:\n%s", s)
	}
}

// A pod the server no longer has is a finding, not an empty card.
func TestExplainGoneLog(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	f := m.deps.reader.(*testkit.FakeReader)
	f.StreamErr = core.ErrNotFoundf("pods %q not found", "demo-oom-backfill-backfill-quarter-2778986266")
	m = openOnExplain(t, m, "demo-oom-backfill")
	s := screen(m)
	for _, want := range []string{"The log of backfill-2026-q2 is gone", "not found", "archiveLogs"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

// y on the section copies the plain-text report.
func TestExplainCopiesTheReport(t *testing.T) {
	m := resize(t, loadDemoList(t), 140, 40)
	m = openOnExplain(t, m, "demo-oom-backfill")
	got := m.copyText()
	if !strings.HasPrefix(got, "Explain demo-oom-backfill in demo: Failed after 17m00s\n1 error\n") ||
		!strings.Contains(got, "✗ ERROR  backfill-2026-q2 failed: out of memory") || strings.Contains(got, "\x1b") {
		t.Fatalf("copied:\n%s", got)
	}
	if label := m.copyLabel(); !strings.HasPrefix(label, "copied ") {
		t.Errorf("copy label %q", label)
	}
}

func TestExplainLogHelpers(t *testing.T) {
	if msg, gone := explainLogError(context.DeadlineExceeded); msg != "the read timed out after 20s" || gone {
		t.Errorf("timeout: %q %v", msg, gone)
	}
	if msg, gone := explainLogError(core.ErrNotFoundf("pods %q not found", "p")); !gone || !strings.Contains(msg, "not found") {
		t.Errorf("not found: %q %v", msg, gone)
	}
	if _, gone := explainLogError(errors.New("connection reset")); gone {
		t.Error("a reset connection marked the log gone")
	}
	tail := newLineTail(3)
	for _, l := range []string{"a", "b", "c", "d", "e"} {
		tail.add(l)
	}
	if got := strings.Join(tail.lines(), ","); got != "c,d,e" {
		t.Errorf("tail = %s", got)
	}
	tail.add(strings.Repeat("x", 10000))
	if got := tail.lines(); len(got[2]) > explainLineBytes+len("…") {
		t.Errorf("a long line kept %d bytes", len(got[2]))
	}
}
