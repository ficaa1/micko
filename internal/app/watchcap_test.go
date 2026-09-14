package app

import (
	"testing"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

// The snapshot cap bounds how many workflows the list holds. A watch that
// added past it grew the snapshot without limit while the view still
// claimed to hold the whole namespace.
func TestTheWatchHonoursTheSnapshotCap(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.deps.snapshotCap = 2
	m.listState.items = []core.Summary{
		{Ref: core.Ref{Namespace: "ns", Name: "a", UID: "uid-a"}, Phase: "Running"},
		{Ref: core.Ref{Namespace: "ns", Name: "b", UID: "uid-b"}, Phase: "Running"},
	}

	m.Update(watchEventMsg{
		genStamp: genStamp{},
		Event: core.WatchEvent{
			Type:    core.WatchAdded,
			Summary: core.Summary{Ref: core.Ref{Namespace: "ns", Name: "c", UID: "uid-c"}, Phase: "Running"},
		},
	})

	if n := len(m.listState.items); n != 2 {
		t.Fatalf("the snapshot holds %d workflows, cap is 2", n)
	}
	if !m.listState.incomplete {
		t.Fatal("the snapshot is capped but does not say so")
	}
}

// An update to a workflow already in the snapshot replaces it in place, so
// the cap must not block it: dropping it would freeze that row's phase.
func TestTheWatchStillUpdatesAtTheCap(t *testing.T) {
	m := testRoot(t, &testkit.FakeReader{})
	m.deps.snapshotCap = 1
	m.listState.items = []core.Summary{
		{Ref: core.Ref{Namespace: "ns", Name: "a", UID: "uid-a"}, Phase: "Running"},
	}

	m.Update(watchEventMsg{
		genStamp: genStamp{},
		Event: core.WatchEvent{
			Type:    core.WatchModified,
			Summary: core.Summary{Ref: core.Ref{Namespace: "ns", Name: "a", UID: "uid-a"}, Phase: "Succeeded"},
		},
	})

	if got := m.listState.items[0].Phase; got != "Succeeded" {
		t.Fatalf("phase = %q, want Succeeded", got)
	}
	if m.listState.incomplete {
		t.Fatal("an in-place update must not mark the snapshot incomplete")
	}
}
