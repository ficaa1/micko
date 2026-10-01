package app

import (
	"context"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// Stream drains continue without nesting batches.
func TestStreamDrainsDoNotNest(t *testing.T) {
	const items = 300
	g := genStamp{Conn: 1, Sel: 1, Attempt: 1}
	feed := func(item func(i int) any, end any) chan any {
		ch := make(chan any, items+1)
		for i := 0; i < items; i++ {
			ch <- item(i)
		}
		ch <- end
		close(ch)
		return ch
	}
	records := make([]core.LogRecord, items)
	for i := range records {
		records[i] = core.LogRecord{Content: fmt.Sprint(i)}
	}
	m := testRoot(t, &testkit.FakeReader{StreamSequence: records})
	cases := []struct {
		name  string
		first tea.Cmd
		next  func(tea.Msg) (tea.Cmd, bool)
	}{
		{"logs", m.deps.streamLogsCmd(context.Background(), g, 1, core.LogRequest{Ref: core.Ref{Namespace: "ns", Name: "wf"}, Container: "main"}),
			func(msg tea.Msg) (tea.Cmd, bool) { lm, ok := msg.(logRecordMsg); return lm.Next, ok && !lm.Done }},
		{"events", drainEventsCmd(context.Background(), g, 0, feed(func(i int) any { return core.Event{UID: fmt.Sprint(i)} }, eventsDoneMsg{genStamp: g})),
			func(msg tea.Msg) (tea.Cmd, bool) { em, ok := msg.(eventsMsg); return em.Next, ok }},
		{"watch", drainWatchCmd(context.Background(), g, feed(func(i int) any { return core.WatchEvent{ResourceVersion: fmt.Sprint(i)} }, watchDoneMsg{genStamp: g})),
			func(msg tea.Msg) (tea.Cmd, bool) { wm, ok := msg.(watchEventMsg); return wm.Next, ok }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			steps := 0
			for cmd := c.first; cmd != nil; steps++ {
				msg := cmd()
				if _, nested := msg.(tea.BatchMsg); nested {
					t.Fatalf("step %d returned a tea.BatchMsg", steps)
				}
				next, more := c.next(msg)
				if !more {
					break
				}
				cmd = next
			}
			if steps < 2 {
				t.Fatalf("the stream ended after %d steps, want it to chain", steps)
			}
		})
	}
}
