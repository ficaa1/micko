package app

import (
	"testing"

	"argo-tui/internal/core"
)

// The logs view owns the retained lines and bounds them by line count and
// by bytes. The root keeps only a count, so following a chatty pod cannot
// grow the heap for as long as the pane stays open.
func TestTheRootRetainsNoLogLines(t *testing.T) {
	m := testRoot(t, nil)
	m.logState = logState{ref: core.Ref{Namespace: "ns", Name: "wf"}, running: true}
	g := genStamp{Conn: m.connGen, Sel: m.selGen}

	for i := 0; i < 100; i++ {
		m.handleLogRecord(logRecordMsg{
			genStamp: g,
			Records: []core.LogRecord{
				{Content: "line"},
				{Content: "line"},
			},
		})
	}
	if m.logState.received != 200 {
		t.Fatalf("counted %d records, want 200", m.logState.received)
	}
}
