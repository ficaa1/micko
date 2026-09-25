package testkit

import (
	"context"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
)

// The archive pages by offset, filters by namespace and label, and answers
// a UID; the demo's archived runs are older than every live workflow and
// carry their owners' labels.
func TestDemoArchive(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	p1, err := f.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Namespace: DemoNamespace, Limit: 4})
	if err != nil || len(p1.Items) != 4 || p1.Continue != "4" {
		t.Fatalf("page 1 = %d items, continue %q, %v", len(p1.Items), p1.Continue, err)
	}
	p2, _ := f.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Namespace: DemoNamespace, Limit: 4, Continue: p1.Continue})
	if len(p2.Items) != 2 || p2.Continue != "" {
		t.Fatalf("page 2 = %d items, continue %q", len(p2.Items), p2.Continue)
	}
	owned, _ := f.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{
		LabelSelector: "workflows.argoproj.io/cron-workflow=demo-etl-hourly"})
	if len(owned.Items) != 2 {
		t.Fatalf("archived ETL runs = %d", len(owned.Items))
	}
	oldestLive := FixtureEpoch
	for _, wf := range f.Workflows {
		if wf.Summary.CreatedAt.Before(oldestLive) {
			oldestLive = wf.Summary.CreatedAt
		}
	}
	for _, wf := range f.ArchivedWorkflows {
		if !wf.Summary.CreatedAt.Before(oldestLive) || wf.Summary.FinishedAt == nil || len(wf.Nodes) == 0 {
			t.Errorf("%s is not an old finished run with nodes", wf.Summary.Ref.Name)
		}
		if _, live := f.Workflows[wf.Summary.Ref]; live {
			t.Errorf("%s is both live and archived", wf.Summary.Ref.Name)
		}
		got, err := f.GetArchivedWorkflow(context.Background(), wf.Summary.Ref.UID)
		if err != nil || got.Summary.Ref != wf.Summary.Ref {
			t.Errorf("get %s: %v", wf.Summary.Ref.UID, err)
		}
	}
	if _, err := f.GetArchivedWorkflow(context.Background(), "no-such-uid"); core.AsAPIError(err) == nil || core.AsAPIError(err).Kind != core.ErrNotFound {
		t.Fatalf("unknown uid err = %v", err)
	}
	if _, err := f.ListArchivedWorkflows(context.Background(), core.ArchiveQuery{Continue: "x"}); err == nil {
		t.Fatal("a bad token was accepted")
	}
}
