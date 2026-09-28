package testkit

import (
	"fmt"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// demoArchive fills the demo's workflow archive: runs older than every live
// workflow, the way a controller with archiving on copies a workflow to its
// database before the workflow's TTL removes it from the cluster. They carry
// the labels a real run would, so each belongs to a cron workflow or a
// template of the demo.
//
// The archived nightly report kept its logs (a workflow with archiveLogs
// set); every other archived run has none left, because its pods were
// deleted with it, and the log pane has to say so.
func demoArchive(f *FakeReader, now time.Time) {
	day := 24 * time.Hour
	runs := []core.Workflow{
		demoArchived(DemoNamespace, fmt.Sprintf("demo-etl-hourly-%d", 1790000000-3600*24), "Succeeded",
			now.Add(-day-65*time.Minute), 6*time.Minute, "",
			map[string]string{"workflows.argoproj.io/cron-workflow": "demo-etl-hourly"}, "ingest", "quality-check"),
		demoArchived(DemoNamespace, fmt.Sprintf("demo-etl-hourly-%d", 1790000000-3600*25), "Failed",
			now.Add(-day-125*time.Minute), 5*time.Minute, "child 'quality-check' failed",
			map[string]string{"workflows.argoproj.io/cron-workflow": "demo-etl-hourly"}, "ingest", "quality-check"),
		demoArchived(DemoNamespace, "demo-nightly-report-4fz9q", "Succeeded",
			now.Add(-2*day-2*time.Hour), 9*time.Minute, "",
			map[string]string{"workflows.argoproj.io/workflow-template": "nightly-report"}, "extract", "transform", "load"),
		demoArchived(DemoNamespace, "demo-release-gate-m2d8x", "Succeeded",
			now.Add(-3*day-5*time.Hour), 47*time.Minute, "",
			map[string]string{"workflows.argoproj.io/workflow-template": "release"}, "build", "test", "deploy-prod"),
		demoArchived(DemoNamespace, "demo-deploy-multi-layer-q8w1c", "Failed",
			now.Add(-4*day-3*time.Hour), 6*time.Minute, "child 'apply' failed",
			map[string]string{"workflows.argoproj.io/workflow-template": "deploy"}, "plan", "apply"),
		demoArchived(DemoNamespace, "demo-backfill-2026-q1", "Succeeded",
			now.Add(-6*day-4*time.Hour), 2*time.Hour+14*time.Minute, "", nil, "backfill-quarter"),
		demoArchived(DemoMLNamespace, "demo-ml-hparam-sweep-7tq4n", "Succeeded",
			now.Add(-5*day-7*time.Hour), 31*time.Minute, "",
			map[string]string{"workflows.argoproj.io/workflow-template": "hparam-sweep"}, "train-trial", "select-best"),
	}
	f.ArchivedWorkflows = runs
	kept := runs[2]
	for _, n := range kept.Nodes {
		if n.PodName == "" || n.StartedAt == nil {
			continue
		}
		lines := demoLogs(n, now)
		f.PodLogs[n.PodName] = lines
		f.WorkflowLogs[kept.Summary.Ref.Name] = append(f.WorkflowLogs[kept.Summary.Ref.Name], lines...)
	}
}

// demoArchived builds one finished run: a steps workflow whose pods ran one
// after another, the last one carrying the failure when the run failed.
func demoArchived(ns, name, phase string, start time.Time, dur time.Duration, message string, labels map[string]string, pods ...string) core.Workflow {
	b := newDemoIn(ns, name, phase, start)
	b.wf.Summary.Ref.UID = "synthetic-uid-archived-" + name
	root := b.add("", "", demoNode{display: name, typ: "Steps", phase: phase, template: "main", dur: dur, message: message})
	step := dur / time.Duration(len(pods))
	parent := root
	for i, p := range pods {
		ph, exit, msg := "Succeeded", "0", ""
		if phase == "Failed" && i == len(pods)-1 {
			ph, exit, msg = "Failed", "1", "Error (exit code 1)"
		}
		at := time.Duration(i)*step + 2*time.Second
		group := b.add(parent, root, demoNode{display: fmt.Sprintf("[%d]", i), typ: "StepGroup", phase: ph, at: at, dur: step - 3*time.Second})
		parent = b.add(group, root, demoNode{display: p, typ: "Pod", phase: ph, template: p, at: at, dur: step - 3*time.Second, exit: exit, message: msg})
	}
	return b.finish(dur, message, labels)
}
