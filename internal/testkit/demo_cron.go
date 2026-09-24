package testkit

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// demoCronWorkflows are the demo's CronWorkflows, in the v3.6+ shape (the
// schedules list and the status counters). They cover what the cron view has
// to show: an hourly schedule whose runs are in the workflow list, a
// suspended weekly job, two schedules in a named time zone in the second
// namespace, and a schedule the controller refused.
//
// demo-etl-hourly owns the demo-etl-hourly-* runs through the
// workflows.argoproj.io/cron-workflow label the controller puts on every run
// it starts. Its schedule fires at the minute those runs started, so its
// last run and theirs agree.
func demoCronWorkflows(now time.Time, runs []core.Workflow) []core.CronWorkflow {
	var etlRuns []core.Workflow
	for _, wf := range runs {
		if wf.Summary.Labels["workflows.argoproj.io/cron-workflow"] == "demo-etl-hourly" {
			etlRuns = append(etlRuns, wf)
		}
	}
	var last *time.Time
	for _, wf := range etlRuns {
		if last == nil || wf.Summary.CreatedAt.After(*last) {
			last = ptrTime(wf.Summary.CreatedAt)
		}
	}
	etlMinute := 0
	if last != nil {
		etlMinute = last.UTC().Minute()
	}
	etlFailed, etlSucceeded := int64(0), int64(0)
	for _, wf := range etlRuns {
		if wf.Summary.Phase == "Failed" {
			etlFailed++
		} else {
			etlSucceeded++
		}
	}

	out := []core.CronWorkflow{
		demoCron(core.CronWorkflow{
			Namespace: DemoNamespace, Name: "demo-etl-hourly",
			CreatedAt:         now.Add(-41 * 24 * time.Hour),
			Schedules:         []string{fmt.Sprintf("%d * * * *", etlMinute)},
			ConcurrencyPolicy: "Forbid", StartingDeadlineSeconds: ptrInt(120),
			SuccessfulJobsHistoryLimit: ptrInt(3), FailedJobsHistoryLimit: ptrInt(3),
			Entrypoint: "etl",
			Arguments: []core.Argument{
				{Name: "source", Value: "warehouse", HasValue: true},
				{Name: "window", Value: "1h", HasValue: true},
				{Name: "api-token", Value: "demo-token-not-a-real-secret", HasValue: true},
			},
			LastScheduledTime: last, Phase: "Active",
			Succeeded: ptrInt(etlSucceeded), Failed: ptrInt(etlFailed),
		}),
		demoCron(core.CronWorkflow{
			Namespace: DemoNamespace, Name: "demo-weekly-compaction",
			CreatedAt: now.Add(-120 * 24 * time.Hour),
			Schedules: []string{"0 3 * * sun"}, Suspend: true,
			ConcurrencyPolicy: "Forbid", SuccessfulJobsHistoryLimit: ptrInt(1), FailedJobsHistoryLimit: ptrInt(1),
			Entrypoint: "compact",
			Arguments: []core.Argument{
				{Name: "retention-days", Value: "30", HasValue: true},
				{Name: "dry-run", Value: "false", HasValue: true},
			},
			LastScheduledTime: ptrTime(now.Add(-17 * 24 * time.Hour).Truncate(time.Hour)),
			Phase:             "Active", Succeeded: ptrInt(16), Failed: ptrInt(1),
		}),
		demoCron(core.CronWorkflow{
			Namespace: DemoNamespace, Name: "demo-cache-warmer",
			CreatedAt: now.Add(-2 * 24 * time.Hour),
			// Day of week 7 is Sunday in some cron dialects but out of
			// range for the controller's parser, which refuses the whole
			// expression and reports it as a condition.
			Schedules:         []string{"*/10 7-19 * * 1-7"},
			ConcurrencyPolicy: "Replace",
			Entrypoint:        "warm",
			Conditions: []core.Condition{{Type: "SpecError", Status: "True",
				Message: "cron schedule */10 7-19 * * 1-7 is malformed: end of range (7) above maximum (6): 1-7"}},
		}),
		demoCron(core.CronWorkflow{
			Namespace: DemoMLNamespace, Name: "demo-ml-retrain",
			CreatedAt: now.Add(-9 * 24 * time.Hour),
			Schedules: []string{"30 6 * * mon-fri", "0 22 * * sun"},
			Timezone:  "Europe/Berlin", ConcurrencyPolicy: "Allow",
			SuccessfulJobsHistoryLimit: ptrInt(5), FailedJobsHistoryLimit: ptrInt(2),
			When:                "{{= cronworkflow.failed < 5 }}",
			StopExpression:      "cronworkflow.failed >= 5",
			WorkflowTemplateRef: "hparam-sweep",
			Arguments: []core.Argument{
				{Name: "dataset", Value: "clicks-v7", HasValue: true},
				{Name: "trials", Value: "3", HasValue: true},
			},
			LastScheduledTime: ptrTime(now.Add(-30 * time.Hour).Truncate(time.Hour)),
			Phase:             "Active", Succeeded: ptrInt(6), Failed: ptrInt(0),
		}),
	}
	return out
}

func ptrInt(n int64) *int64 { return &n }

// demoCron fills the identity and writes the object as the server would
// return it, so the raw view has a real manifest to render and redact.
func demoCron(cw core.CronWorkflow) core.CronWorkflow {
	cw.UID = "synthetic-uid-cron-" + cw.Name
	cw.Labels = map[string]string{"app.kubernetes.io/part-of": "argo-tui-demo"}
	spec := map[string]any{
		"schedules":         cw.Schedules,
		"concurrencyPolicy": cw.ConcurrencyPolicy,
		"workflowSpec":      demoWorkflowSpec(cw.Entrypoint, cw.WorkflowTemplateRef, cw.Arguments),
	}
	if cw.Timezone != "" {
		spec["timezone"] = cw.Timezone
	}
	if cw.Suspend {
		spec["suspend"] = true
	}
	if cw.StartingDeadlineSeconds != nil {
		spec["startingDeadlineSeconds"] = *cw.StartingDeadlineSeconds
	}
	if cw.SuccessfulJobsHistoryLimit != nil {
		spec["successfulJobsHistoryLimit"] = *cw.SuccessfulJobsHistoryLimit
	}
	if cw.FailedJobsHistoryLimit != nil {
		spec["failedJobsHistoryLimit"] = *cw.FailedJobsHistoryLimit
	}
	if cw.When != "" {
		spec["when"] = cw.When
	}
	if cw.StopExpression != "" {
		spec["stopStrategy"] = map[string]any{"expression": cw.StopExpression}
	}
	status := map[string]any{}
	if cw.Phase != "" {
		status["phase"] = cw.Phase
	}
	if cw.LastScheduledTime != nil {
		status["lastScheduledTime"] = cw.LastScheduledTime.UTC().Format(time.RFC3339)
	}
	if cw.Succeeded != nil {
		status["succeeded"] = *cw.Succeeded
	}
	if cw.Failed != nil {
		status["failed"] = *cw.Failed
	}
	if len(cw.Conditions) > 0 {
		var conds []map[string]string
		for _, c := range cw.Conditions {
			conds = append(conds, map[string]string{"type": c.Type, "status": c.Status, "message": c.Message})
		}
		status["conditions"] = conds
	}
	cw.Resource = mustJSON(map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "CronWorkflow",
		"metadata":   demoMeta(cw.Namespace, cw.Name, cw.UID, cw.Labels, cw.CreatedAt),
		"spec":       spec,
		"status":     status,
	})
	return cw
}

// demoWorkflowSpec is the embedded workflow spec of a demo object: the
// entrypoint or the template it runs, and its arguments with their values,
// so the redaction in the raw view has values to hide.
func demoWorkflowSpec(entrypoint, templateRef string, args []core.Argument) map[string]any {
	ws := map[string]any{}
	if entrypoint != "" {
		ws["entrypoint"] = entrypoint
	}
	if templateRef != "" {
		ws["workflowTemplateRef"] = map[string]any{"name": templateRef}
	}
	if len(args) > 0 {
		var ps []map[string]any
		for _, a := range args {
			p := map[string]any{"name": a.Name}
			if a.HasValue {
				p["value"] = a.Value
			}
			if a.Default != "" {
				p["default"] = a.Default
			}
			if len(a.Enum) > 0 {
				p["enum"] = a.Enum
			}
			if a.Description != "" {
				p["description"] = a.Description
			}
			ps = append(ps, p)
		}
		ws["arguments"] = map[string]any{"parameters": ps}
	}
	return ws
}

func demoMeta(ns, name, uid string, labels map[string]string, created time.Time) map[string]any {
	m := map[string]any{
		"name": name, "uid": uid, "labels": labels,
		"creationTimestamp": created.UTC().Format(time.RFC3339),
	}
	if ns != "" {
		m["namespace"] = ns
	}
	return m
}

func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err) // maps of strings, numbers and slices always marshal
	}
	return out
}
