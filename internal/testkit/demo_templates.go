package testkit

import (
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// demoTemplates are the demo's WorkflowTemplates and its one
// ClusterWorkflowTemplate. Each is the template a demo workflow names in its
// workflows.argoproj.io/workflow-template (or cluster-workflow-template)
// label, and its templates are the ones that workflow's nodes ran, so the
// drill-down from a template lands on runs that match it.
func demoTemplates(now time.Time) (namespaced, cluster []core.WorkflowTemplate) {
	tmpl := func(name, typ string) core.TemplateInfo { return core.TemplateInfo{Name: name, Type: typ} }
	arg := func(name, value string) core.Argument { return core.Argument{Name: name, Value: value, HasValue: true} }

	namespaced = []core.WorkflowTemplate{
		demoTemplate(core.WorkflowTemplate{
			Namespace: DemoNamespace, Name: "nightly-report",
			CreatedAt:   now.Add(-64 * 24 * time.Hour),
			Description: "Extract the day's orders, transform them and load the report tables",
			Entrypoint:  "report",
			Arguments: []core.Argument{
				{Name: "date", Description: "the day to report on, YYYY-MM-DD"},
				{Name: "source", Value: "warehouse", HasValue: true, Enum: []string{"warehouse", "lake"}},
				arg("channel", "#data-alerts"),
			},
			Templates: []core.TemplateInfo{
				tmpl("report", "dag"), tmpl("extract", "container"), tmpl("transform", "script"),
				tmpl("load", "container"), tmpl("notify", "http"),
			},
		}),
		demoTemplate(core.WorkflowTemplate{
			Namespace: DemoNamespace, Name: "train",
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			Description: "Preprocess features and train six shards in parallel",
			Entrypoint:  "train",
			Arguments: []core.Argument{
				arg("epochs", "10"), arg("lr", "0.0003"), arg("shards", "6"),
				{Name: "registry-token", ValueFrom: "configmap train-config key registry-token"},
			},
			Templates: []core.TemplateInfo{
				tmpl("train", "dag"), tmpl("preprocess", "container"), tmpl("train-shard", "container"),
				tmpl("evaluate", "script"),
			},
		}),
		demoTemplate(core.WorkflowTemplate{
			Namespace: DemoNamespace, Name: "release",
			CreatedAt:   now.Add(-90 * 24 * time.Hour),
			Description: "Build, test, wait for approval, deploy",
			Entrypoint:  "release",
			Arguments: []core.Argument{
				{Name: "git-sha", Description: "the commit to release"},
				{Name: "target", Value: "linux/amd64", HasValue: true, Enum: []string{"linux/amd64", "linux/arm64"}},
			},
			Templates: []core.TemplateInfo{
				tmpl("release", "steps"), tmpl("build", "container"), tmpl("test", "container"),
				tmpl("approval", "suspend"), tmpl("deploy-prod", "resource"),
			},
		}),
		demoTemplate(core.WorkflowTemplate{
			Namespace: DemoNamespace, Name: "deploy",
			CreatedAt:  now.Add(-45 * 24 * time.Hour),
			Entrypoint: "deploy",
			Arguments: []core.Argument{
				arg("regions", `["eu-west"]`),
				{Name: "dry-run", Value: "false", HasValue: true, Enum: []string{"true", "false"}},
			},
			Templates: []core.TemplateInfo{
				tmpl("deploy", "dag"), tmpl("plan", "container"), tmpl("layer", "dag"), tmpl("apply", "containerSet"),
			},
		}),
		demoTemplate(core.WorkflowTemplate{
			Namespace: DemoMLNamespace, Name: "hparam-sweep",
			CreatedAt:   now.Add(-12 * 24 * time.Hour),
			Description: "One trial per learning rate, then keep the best",
			Entrypoint:  "sweep",
			Arguments: []core.Argument{
				arg("dataset", "clicks-v7"), arg("trials", "3"),
				{Name: "lrs", Value: `["0.001","0.0003","0.0001"]`, HasValue: true},
			},
			Templates: []core.TemplateInfo{
				tmpl("sweep", "dag"), tmpl("train-trial", "container"), tmpl("select-best", "plugin"),
			},
		}),
	}
	cluster = []core.WorkflowTemplate{
		demoTemplate(core.WorkflowTemplate{
			Name:        "whalesay",
			CreatedAt:   now.Add(-200 * 24 * time.Hour),
			Description: "Print a message, the classic first workflow",
			Entrypoint:  "whalesay",
			Arguments:   []core.Argument{{Name: "message", Default: "hello world", HasValue: false}},
			Templates:   []core.TemplateInfo{tmpl("whalesay", "container")},
		}),
	}
	return namespaced, cluster
}

// demoTemplate fills the identity and writes the manifest, with one spec
// template per listed name built from its type, so the raw view shows a
// real object.
func demoTemplate(t core.WorkflowTemplate) core.WorkflowTemplate {
	kind := "WorkflowTemplate"
	if t.Namespace == "" {
		kind = "ClusterWorkflowTemplate"
	}
	t.UID = "synthetic-uid-template-" + t.Name
	t.ServiceAccount = "demo-runner"
	t.Labels = map[string]string{"app.kubernetes.io/part-of": "argo-tui-demo"}
	spec := demoWorkflowSpec(t.Entrypoint, "", t.Arguments)
	spec["serviceAccountName"] = t.ServiceAccount
	var templates []map[string]any
	for _, tm := range t.Templates {
		templates = append(templates, map[string]any{"name": tm.Name, tm.Type: demoTemplateBody(tm)})
	}
	spec["templates"] = templates
	meta := demoMeta(t.Namespace, t.Name, t.UID, t.Labels, t.CreatedAt)
	if t.Description != "" {
		meta["annotations"] = map[string]string{"workflows.argoproj.io/description": t.Description}
	}
	t.Resource = mustJSON(map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       kind,
		"metadata":   meta,
		"spec":       spec,
	})
	return t
}

// demoTemplateBody is a plausible body for one template of the given type.
func demoTemplateBody(tm core.TemplateInfo) any {
	image := "registry.example/demo/" + tm.Name + ":1.4.2"
	switch tm.Type {
	case "container":
		return map[string]any{"image": image, "command": []string{"/bin/run", tm.Name}}
	case "script":
		return map[string]any{"image": "python:3.12-slim", "command": []string{"python"}, "source": "print('" + tm.Name + "')"}
	case "dag":
		return map[string]any{"tasks": []map[string]string{{"name": "first", "template": tm.Name + "-step"}}}
	case "steps":
		return [][]map[string]string{{{"name": "first", "template": tm.Name + "-step"}}}
	case "suspend":
		return map[string]any{}
	case "resource":
		return map[string]any{"action": "apply", "manifest": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + tm.Name + "\n"}
	case "http":
		return map[string]any{"url": "https://hooks.example/" + tm.Name, "method": "POST"}
	case "plugin":
		return map[string]any{"demo-select": map[string]string{"metric": "val_acc"}}
	case "containerSet":
		return map[string]any{"containers": []map[string]any{{"name": "main", "image": image}}}
	}
	return map[string]any{}
}
