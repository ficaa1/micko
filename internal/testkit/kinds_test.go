package testkit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/cronexpr"
)

// Workflow lists apply equality selectors and reject unsupported set selectors.
func TestFakeReaderListLabels(t *testing.T) {
	wf := SyntheticWorkflow("ns", "run", "Running", FixtureEpoch)
	wf.Summary.Labels = map[string]string{"owner": "etl", "workflows.argoproj.io/completed": "true"}
	f := &FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	cases := []struct {
		name, selector string
		want           bool
	}{
		{"empty", "", true}, {"equality", "owner=etl", true}, {"double equality", "owner==etl", true},
		{"different value", "owner=other", false}, {"equal excluded", "owner!=etl", false}, {"different excluded", "owner!=other", true},
		{"present", "owner", true}, {"present excluded", "!owner", false}, {"absent required", "missing", false},
		{"absent excluded", "!missing", true}, {"absent inequality", "missing!=x", true},
		{"all terms required", "owner=etl,missing", false}, {"trimmed conjunction", "owner=etl, workflows.argoproj.io/completed=true", true},
		{"unsupported set", "owner in (etl)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page, err := f.List(context.Background(), core.Query{LabelSelector: c.selector})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if c.want {
				want = 1
			}
			if len(page.Items) != want {
				t.Fatalf("items = %d, want %d for %q", len(page.Items), want, c.selector)
			}
		})
	}
}

// Demo cron schedules and manifests agree with their owned workflows.
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

// Demo workflows reference complete templates in their own namespace or cluster scope.
func TestDemoTemplatesMatchTheRuns(t *testing.T) {
	f := DemoReader(NewFakeClock(FixtureEpoch))
	templates, err := f.ListWorkflowTemplates(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, wt := range templates {
		have[wt.Namespace+"/"+wt.Name] = true
		if !json.Valid(wt.Resource) || len(wt.Templates) == 0 || wt.Entrypoint == "" {
			t.Errorf("%s is incomplete: %+v", wt.Name, wt)
		}
	}
	named := 0
	for ref, wf := range f.Workflows {
		if name := wf.Summary.Labels["workflows.argoproj.io/workflow-template"]; name != "" {
			named++
			if !have[ref.Namespace+"/"+name] {
				t.Errorf("%s names template %s, which the demo does not have", ref.Name, name)
			}
		}
	}
	if named == 0 {
		t.Fatal("no demo workflow names a template")
	}
	cluster, err := f.ListClusterWorkflowTemplates(context.Background())
	if err != nil || len(cluster) != 1 || cluster[0].Namespace != "" {
		t.Fatalf("cluster templates = %+v, %v", cluster, err)
	}
	page, _ := f.List(context.Background(), core.Query{Namespace: DemoNamespace,
		LabelSelector: "workflows.argoproj.io/cluster-workflow-template=" + cluster[0].Name})
	if len(page.Items) != 1 {
		t.Fatalf("the cluster template owns %d runs, want 1", len(page.Items))
	}
	demo, _ := f.ListWorkflowTemplates(context.Background(), DemoNamespace)
	if len(demo) != 4 {
		t.Fatalf("demo namespace holds %d templates, want 4", len(demo))
	}
}
