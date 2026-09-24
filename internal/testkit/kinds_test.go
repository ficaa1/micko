package testkit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/cronexpr"
)

// The equality-based selector forms the drill-down and the gate scan send
// are understood; a set-based one matches nothing rather than everything.
func TestMatchLabels(t *testing.T) {
	labels := map[string]string{"owner": "etl", "workflows.argoproj.io/completed": "true"}
	cases := map[string]bool{
		"":                  true,
		"owner=etl":         true,
		"owner==etl":        true,
		"owner=other":       false,
		"owner!=etl":        false,
		"owner!=other":      true,
		"owner":             true,
		"!owner":            false,
		"missing":           false,
		"!missing":          true,
		"missing!=x":        true,
		"owner=etl,missing": false,
		"owner=etl, workflows.argoproj.io/completed=true": true,
		"owner in (etl)": false,
	}
	for sel, want := range cases {
		if got := MatchLabels(sel, labels); got != want {
			t.Errorf("MatchLabels(%q) = %v, want %v", sel, got, want)
		}
	}
}

// List honours the label selector, which is how the demo's drill-down
// finds a cron workflow's runs.
func TestDemoListByOwnerLabel(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	f.PageLimit = 0
	page, err := f.List(context.Background(), core.Query{
		Namespace: DemoNamespace, LabelSelector: "workflows.argoproj.io/cron-workflow=demo-etl-hourly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("demo-etl-hourly owns %d runs, want 3", len(page.Items))
	}
}

// The demo's cron workflows: the hourly ETL owns runs that exist and its
// last run is the newest of them, one is suspended, one has two schedules in
// a named zone in the second namespace, and one has a schedule the
// controller refuses. Every schedule but that one parses, and each carries a
// manifest.
func TestDemoCronWorkflows(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	all, err := f.ListCronWorkflows(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]core.CronWorkflow{}
	for _, cw := range all {
		byName[cw.Name] = cw
		if len(cw.Resource) == 0 || !json.Valid(cw.Resource) {
			t.Errorf("%s has no valid manifest", cw.Name)
		}
		loc, err := cronexpr.LoadZone(cw.Timezone)
		if err != nil {
			t.Errorf("%s: %v", cw.Name, err)
			continue
		}
		for _, s := range cw.Schedules {
			_, err := cronexpr.Parse(s, loc)
			if (err != nil) != (cw.Name == "demo-cache-warmer") {
				t.Errorf("%s: schedule %q parse error %v", cw.Name, s, err)
			}
		}
	}
	etl, ok := byName["demo-etl-hourly"]
	if !ok || etl.Suspend || etl.LastScheduledTime == nil {
		t.Fatalf("demo-etl-hourly = %+v", etl)
	}
	var newest time.Time
	for _, wf := range f.Workflows {
		if wf.Summary.Labels["workflows.argoproj.io/cron-workflow"] == "demo-etl-hourly" && wf.Summary.CreatedAt.After(newest) {
			newest = wf.Summary.CreatedAt
		}
	}
	if !etl.LastScheduledTime.Equal(newest) {
		t.Fatalf("last run %s, newest run %s", etl.LastScheduledTime, newest)
	}
	if !byName["demo-weekly-compaction"].Suspend {
		t.Fatal("demo-weekly-compaction is not suspended")
	}
	retrain := byName["demo-ml-retrain"]
	if retrain.Namespace != DemoMLNamespace || len(retrain.Schedules) != 2 || retrain.Timezone == "" {
		t.Fatalf("demo-ml-retrain = %+v", retrain)
	}
	if len(byName["demo-cache-warmer"].Conditions) == 0 {
		t.Fatal("the refused schedule carries no condition")
	}
	demo, _ := f.ListCronWorkflows(context.Background(), DemoNamespace)
	if len(demo) != 3 {
		t.Fatalf("demo namespace holds %d cron workflows, want 3", len(demo))
	}
}
