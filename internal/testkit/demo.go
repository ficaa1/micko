package testkit

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// The demo dataset is synthetic, but its node maps follow the shapes the Argo
// controller really writes, because the views are built against those shapes:
//
//   - A steps template chains its groups. The Steps node's child is the
//     StepGroup "[0]", whose children are that group's steps, and every step
//     in a group lists the next StepGroup as its own child.
//   - A DAG task lists the tasks that depend on it as its children, so a
//     diamond reaches its join task twice. Every task's boundaryID is the DAG.
//   - A retried task is a Retry node whose children are its attempts; the
//     attempts carry nodeFlag.retried.
//   - An exit handler is a separate top-level tree named "<workflow>.onExit",
//     flagged as hooked.
//
// Everything is fabricated: names, UIDs, hosts and log text.

// DemoNamespace is the namespace the demo starts in; most demo workflows
// live in it.
const DemoNamespace = "demo"

// DemoMLNamespace is the demo's second namespace. It holds a few workflows so
// the namespace picker and the all-namespaces view have something to show
// beyond the namespace the session starts in.
const DemoMLNamespace = "demo-ml"

// demoBuilder assembles one demo workflow's node map with realistic IDs, pod
// names and timings.
type demoBuilder struct {
	wf   core.Workflow
	name string
	// start is when the workflow started; node offsets are relative to it.
	start time.Time
	// templates records the template of every node for the resource JSON.
	templates map[string]bool
}

func newDemo(name, phase string, created time.Time) *demoBuilder {
	return newDemoIn(DemoNamespace, name, phase, created)
}

// newDemoIn starts a demo workflow in namespace ns.
func newDemoIn(ns, name, phase string, created time.Time) *demoBuilder {
	wf := SyntheticWorkflow(ns, name, phase, created)
	wf.PodNameVersion = "v2"
	return &demoBuilder{wf: wf, name: name, start: created, templates: map[string]bool{}}
}

// demoNode is the declarative input for one node.
type demoNode struct {
	display  string
	typ      string
	phase    string
	template string
	// at and dur place the node on the workflow's clock. A negative dur means
	// the node is still running; an at of -1 means it has not started.
	at, dur time.Duration
	message string
	exit    string
	host    string
	in, out core.NodeIO
	retried bool
	hooked  bool
	// estimate is the controller's duration estimate from earlier runs.
	estimate time.Duration
}

const notStarted = time.Duration(-1)

// add creates a node under parent (by ID; "" for a root) and returns its ID.
// The node's full name is the parent's name plus the display name, the way
// Argo names nodes, and its ID is derived from that name.
func (b *demoBuilder) add(parent, boundary string, n demoNode) string {
	// A StepGroup is named after its steps template ("wf[1]"), whichever
	// step it hangs off; everything else extends its parent's name.
	fullName := b.name
	if parent != "" {
		if n.typ == "StepGroup" {
			fullName = b.wf.Nodes[boundary].Name + n.display
		} else {
			fullName = b.wf.Nodes[parent].Name + "." + n.display
		}
	}
	if n.hooked && parent == "" {
		fullName = b.name + "." + n.display
	}
	id := b.name
	if fullName != b.name {
		id = b.name + "-" + fmt.Sprint(fnv32(fullName))
	}
	node := core.Node{
		ID: id, Name: fullName, DisplayName: n.display, Type: n.typ, Phase: n.phase,
		Message: n.message, BoundaryID: boundary, TemplateName: n.template,
		HostNodeName: n.host, ExitCode: n.exit, Inputs: n.in, Outputs: n.out,
		Retried: n.retried, Hooked: n.hooked, EstimatedDuration: n.estimate,
	}
	if n.at != notStarted {
		node.StartedAt = ptrTime(b.start.Add(n.at))
		if n.dur >= 0 {
			node.FinishedAt = ptrTime(b.start.Add(n.at + n.dur))
			if n.typ == "Pod" {
				secs := int64(n.dur / time.Second)
				node.ResourcesDuration = map[string]int64{"cpu": secs/4 + 1, "memory": secs*3 + 1}
			}
		}
	}
	if n.typ == "Pod" {
		node.PodName = demoPodName(b.name, n.template, fullName)
		if node.Progress == "" {
			node.Progress = "0/1"
			if n.phase == "Succeeded" {
				node.Progress = "1/1"
			}
		}
		if node.HostNodeName == "" && node.StartedAt != nil {
			node.HostNodeName = "demo-worker-" + fmt.Sprint(fnv32(fullName)%4+1)
		}
	}
	if n.template != "" {
		b.templates[n.template] = true
	}
	b.wf.Nodes[id] = node
	if parent != "" {
		p := b.wf.Nodes[parent]
		p.Children = append(p.Children, id)
		b.wf.Nodes[parent] = p
	}
	return id
}

// link adds child to parent's children without creating anything, for the
// second parent of a DAG join or the next StepGroup of a step.
func (b *demoBuilder) link(parent, child string) {
	p := b.wf.Nodes[parent]
	p.Children = append(p.Children, child)
	b.wf.Nodes[parent] = p
}

// outbound records the nodes that complete a template.
func (b *demoBuilder) outbound(id string, nodes ...string) {
	n := b.wf.Nodes[id]
	n.OutboundNodes = append(n.OutboundNodes, nodes...)
	b.wf.Nodes[id] = n
}

// finish sets the workflow's own timing and message, computes its progress
// from its pod nodes the way the controller does, and writes the resource
// JSON the resource tab renders.
func (b *demoBuilder) finish(dur time.Duration, message string, labels map[string]string) core.Workflow {
	wf := b.wf
	wf.Summary.StartedAt = ptrTime(b.start)
	if dur >= 0 && wf.Summary.Phase != "Running" && wf.Summary.Phase != "Pending" {
		wf.Summary.FinishedAt = ptrTime(b.start.Add(dur))
	}
	wf.Summary.Message = message
	for k, v := range labels {
		wf.Summary.Labels[k] = v
	}
	if wf.Summary.FinishedAt != nil {
		wf.Summary.Labels["workflows.argoproj.io/completed"] = "true"
	}
	// The adapter derives the gate marker from the node map; the demo
	// bypasses the adapter, so it derives it the same way here.
	for _, n := range wf.Nodes {
		if n.Type == "Suspend" && n.Phase == "Running" {
			wf.Summary.Suspended = true
		}
	}
	done, total := 0, 0
	for _, n := range wf.Nodes {
		if n.Type != "Pod" {
			continue
		}
		total++
		if n.Phase == "Succeeded" || n.Phase == "Failed" || n.Phase == "Error" {
			done++
		}
	}
	if total > 0 {
		wf.Summary.Progress = fmt.Sprintf("%d/%d", done, total)
	}
	wf.Resource = b.resourceJSON(wf)
	return wf
}

// resourceJSON renders the workflow as the server would return it: metadata,
// a spec naming every template the nodes used, and the status with its node
// map. Parameter values are included, so the resource tab's redaction has
// something to redact.
func (b *demoBuilder) resourceJSON(wf core.Workflow) []byte {
	templates := make([]string, 0, len(b.templates))
	for t := range b.templates {
		templates = append(templates, t)
	}
	sort.Strings(templates)
	specTemplates := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		specTemplates = append(specTemplates, map[string]any{
			"name":      t,
			"container": map[string]any{"image": "registry.example/demo/" + t + ":1.4.2", "command": []string{"/bin/run", t}},
		})
	}
	nodes := map[string]any{}
	for id, n := range wf.Nodes {
		m := map[string]any{
			"id": id, "name": n.Name, "displayName": n.DisplayName, "type": n.Type,
			"phase": n.Phase, "templateName": n.TemplateName,
		}
		if n.BoundaryID != "" {
			m["boundaryID"] = n.BoundaryID
		}
		if len(n.Children) > 0 {
			m["children"] = n.Children
		}
		if n.Message != "" {
			m["message"] = n.Message
		}
		if n.StartedAt != nil {
			m["startedAt"] = n.StartedAt.UTC().Format(time.RFC3339)
		}
		if n.FinishedAt != nil {
			m["finishedAt"] = n.FinishedAt.UTC().Format(time.RFC3339)
		}
		if n.HostNodeName != "" {
			m["hostNodeName"] = n.HostNodeName
		}
		if ps := paramsJSON(n.Inputs.Parameters); ps != nil {
			m["inputs"] = map[string]any{"parameters": ps}
		}
		if n.ExitCode != "" || len(n.Outputs.Parameters) > 0 {
			out := map[string]any{}
			if n.ExitCode != "" {
				out["exitCode"] = n.ExitCode
			}
			if ps := paramsJSON(n.Outputs.Parameters); ps != nil {
				out["parameters"] = ps
			}
			m["outputs"] = out
		}
		nodes[id] = m
	}
	status := map[string]any{"phase": wf.Summary.Phase, "nodes": nodes}
	if wf.Summary.Progress != "" {
		status["progress"] = wf.Summary.Progress
	}
	if wf.Summary.Message != "" {
		status["message"] = wf.Summary.Message
	}
	if wf.Summary.StartedAt != nil {
		status["startedAt"] = wf.Summary.StartedAt.UTC().Format(time.RFC3339)
	}
	if wf.Summary.FinishedAt != nil {
		status["finishedAt"] = wf.Summary.FinishedAt.UTC().Format(time.RFC3339)
	}
	entry := b.name
	if root, ok := wf.Nodes[b.name]; ok && root.TemplateName != "" {
		entry = root.TemplateName
	}
	doc := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]any{
			"name": wf.Summary.Ref.Name, "namespace": wf.Summary.Ref.Namespace,
			"uid": wf.Summary.Ref.UID, "labels": wf.Summary.Labels,
			"annotations":       map[string]string{"workflows.argoproj.io/pod-name-format": "v2"},
			"creationTimestamp": wf.Summary.CreatedAt.UTC().Format(time.RFC3339),
		},
		"spec": map[string]any{
			"entrypoint":         entry,
			"serviceAccountName": "demo-runner",
			"templates":          specTemplates,
		},
		"status": status,
	}
	out, err := json.Marshal(doc)
	if err != nil {
		panic(err) // a map of strings and slices always marshals
	}
	return out
}

func paramsJSON(ps []core.Parameter) []map[string]string {
	if len(ps) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, map[string]string{"name": p.Name, "value": p.Value})
	}
	return out
}

// demoPodName follows Argo's v2 pod naming: workflow, template and the FNV-1a
// hash of the node name.
func demoPodName(wf, template, nodeName string) string {
	if template == "" {
		return wf + "-" + fmt.Sprint(fnv32(nodeName))
	}
	return wf + "-" + template + "-" + fmt.Sprint(fnv32(nodeName))
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func params(kv ...string) []core.Parameter {
	out := make([]core.Parameter, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, core.Parameter{Name: kv[i], Value: kv[i+1]})
	}
	return out
}

const running = time.Duration(-2)

// DemoReader returns a FakeReader seeded with the synthetic demo dataset for
// `--demo`. The workflows cover the states a reader meets on a real cluster:
// a run parked on an approval gate, a failure behind exhausted retries, an
// out-of-memory kill, a fan-out still in progress, a validation error with no
// nodes at all, a deploy tree that is mostly skipped branches, and runs a
// CronWorkflow started. Two more runs live in a second namespace,
// DemoMLNamespace.
func DemoReader(clock *FakeClock) *FakeReader {
	now := clock.Now()
	f := &FakeReader{
		Workflows:    map[core.Ref]core.Workflow{},
		PageLimit:    4, // exercise pagination in demo
		PodLogs:      map[string][]core.LogRecord{},
		WorkflowLogs: map[string][]core.LogRecord{},
	}
	put := func(wf core.Workflow) {
		f.Workflows[wf.Summary.Ref] = wf
		f.Order = append(f.Order, wf.Summary.Ref)
		// The workflow-wide log is every pod's log in start order, the way
		// the server interleaves them by pod.
		pods := make([]core.Node, 0, len(wf.Nodes))
		for _, n := range wf.Nodes {
			if n.PodName != "" && n.StartedAt != nil {
				pods = append(pods, n)
			}
		}
		sort.Slice(pods, func(i, j int) bool {
			if !pods[i].StartedAt.Equal(*pods[j].StartedAt) {
				return pods[i].StartedAt.Before(*pods[j].StartedAt)
			}
			return pods[i].ID < pods[j].ID
		})
		for _, n := range pods {
			lines := demoLogs(n, now)
			f.PodLogs[n.PodName] = lines
			f.WorkflowLogs[wf.Summary.Ref.Name] = append(f.WorkflowLogs[wf.Summary.Ref.Name], lines...)
		}
	}

	put(demoHelloWorld(now.Add(-26 * time.Minute)))
	put(demoNightlyReport(now.Add(-2 * time.Hour)))
	put(demoTrainPipeline(now.Add(-4 * time.Minute)))
	put(demoDataPull(now.Add(-40 * time.Minute)))
	put(demoCleanup(now.Add(-1 * time.Minute)))
	put(demoReleaseGate(now.Add(-12 * time.Minute)))
	put(demoBackfill(now.Add(-5 * time.Hour)))
	put(demoDeploy(now.Add(-3 * time.Hour)))
	put(demoValidation(now.Add(-6 * time.Hour)))
	for i, age := range []time.Duration{65 * time.Minute, 125 * time.Minute, 185 * time.Minute} {
		put(demoETLRun(now.Add(-age), i))
	}
	put(demoHParamSweep(now.Add(-18 * time.Minute)))
	put(demoBatchInfer(now.Add(-7 * time.Hour)))
	return f
}

// demoHelloWorld is the smallest workflow: one pod, done.
func demoHelloWorld(start time.Time) core.Workflow {
	b := newDemo("demo-hello-world", "Succeeded", start)
	b.add("", "", demoNode{display: "demo-hello-world", typ: "Pod", phase: "Succeeded",
		template: "whalesay", dur: 4 * time.Minute, exit: "0",
		in: core.NodeIO{Parameters: params("message", "hello argo-tui")}})
	return b.finish(4*time.Minute, "", nil)
}

// demoNightlyReport is a DAG whose transform task exhausted its retries.
// The load task depends on it and is omitted; the exit handler still ran.
func demoNightlyReport(start time.Time) core.Workflow {
	b := newDemo("demo-nightly-report", "Failed", start)
	root := b.add("", "", demoNode{display: "demo-nightly-report", typ: "DAG", phase: "Failed",
		template: "report", dur: 4 * time.Minute, message: "child 'transform' failed"})
	extract := b.add(root, root, demoNode{display: "extract", typ: "Pod", phase: "Succeeded",
		template: "extract", at: 5 * time.Second, dur: 50 * time.Second, exit: "0",
		in:  core.NodeIO{Parameters: params("date", "2026-09-07", "source", "warehouse")},
		out: core.NodeIO{Parameters: params("rows", "184022"), Artifacts: []string{"raw-extract"}}})
	retry := b.add(extract, root, demoNode{display: "transform", typ: "Retry", phase: "Failed",
		template: "transform", at: 60 * time.Second, dur: 150 * time.Second,
		message: "No more retries left"})
	for i, d := range []time.Duration{40, 45, 50} {
		b.add(retry, root, demoNode{display: fmt.Sprintf("transform(%d)", i), typ: "Pod", phase: "Failed",
			template: "transform", at: 60*time.Second + time.Duration(i)*52*time.Second, dur: d * time.Second,
			message: "Error (exit code 1)", exit: "1", retried: true,
			in: core.NodeIO{Parameters: params("rows", "184022"), Artifacts: []string{"raw-extract"}}})
	}
	b.add(retry, root, demoNode{display: "load", typ: "Pod", phase: "Omitted", template: "load",
		at: notStarted, message: "omitted: depends condition not met"})
	b.outbound(root, retry)
	b.add("", "", demoNode{display: "onExit", typ: "Pod", phase: "Succeeded", template: "notify",
		at: 215 * time.Second, dur: 20 * time.Second, exit: "0", hooked: true,
		in: core.NodeIO{Parameters: params("status", "Failed", "channel", "#data-alerts")}})
	return b.finish(4*time.Minute, "child 'transform' failed", map[string]string{
		"workflows.argoproj.io/workflow-template": "nightly-report",
	})
}

// demoTrainPipeline is a DAG still running: preprocessing done, a fan-out of
// training shards part way through, evaluation waiting on all of them.
func demoTrainPipeline(start time.Time) core.Workflow {
	b := newDemo("demo-train-pipeline", "Running", start)
	root := b.add("", "", demoNode{display: "demo-train-pipeline", typ: "DAG", phase: "Running",
		template: "train", dur: running, estimate: 9 * time.Minute})
	prep := b.add(root, root, demoNode{display: "preprocess", typ: "Pod", phase: "Succeeded",
		template: "preprocess", at: 4 * time.Second, dur: 70 * time.Second, exit: "0",
		out: core.NodeIO{Artifacts: []string{"features"}}})
	eval := ""
	phases := []string{"Succeeded", "Succeeded", "Running", "Running", "Running", "Pending"}
	for i, ph := range phases {
		dur := running
		exit := ""
		if ph == "Succeeded" {
			dur, exit = time.Duration(95+10*i)*time.Second, "0"
		}
		at := 80 * time.Second
		if ph == "Pending" {
			at = notStarted
		}
		shard := b.add(prep, root, demoNode{display: fmt.Sprintf("train-shard(%d:%d)", i, i), typ: "Pod",
			phase: ph, template: "train-shard", at: at, dur: dur, exit: exit, estimate: 2 * time.Minute,
			in: core.NodeIO{Parameters: params("shard", fmt.Sprint(i), "epochs", "10", "lr", "0.0003")}})
		if eval == "" {
			eval = b.add(shard, root, demoNode{display: "evaluate", typ: "Pod", phase: "Pending",
				template: "evaluate", at: notStarted})
		} else {
			b.link(shard, eval)
		}
	}
	b.outbound(root, eval)
	return b.finish(running, "", map[string]string{"workflows.argoproj.io/workflow-template": "train"})
}

// demoDataPull is a steps workflow on its third group, with a conditional
// step that was skipped and two steps running in parallel.
func demoDataPull(start time.Time) core.Workflow {
	b := newDemo("demo-data-pull", "Running", start)
	root := b.add("", "", demoNode{display: "demo-data-pull", typ: "Steps", phase: "Running",
		template: "pull", dur: running, estimate: 55 * time.Minute})
	g0 := b.add(root, root, demoNode{display: "[0]", typ: "StepGroup", phase: "Succeeded",
		at: 3 * time.Second, dur: 8 * time.Minute})
	list := b.add(g0, root, demoNode{display: "list-sources", typ: "Pod", phase: "Succeeded",
		template: "list-sources", at: 3 * time.Second, dur: 8 * time.Minute, exit: "0",
		out: core.NodeIO{Parameters: params("sources", `["eu","us","apac"]`)}})
	g1 := b.add(list, root, demoNode{display: "[1]", typ: "StepGroup", phase: "Succeeded",
		at: 8*time.Minute + 5*time.Second, dur: 20 * time.Minute})
	full := b.add(g1, root, demoNode{display: "full-sync", typ: "Skipped", phase: "Skipped",
		template: "full-sync", at: 8*time.Minute + 5*time.Second, dur: 0,
		message: "when 'false' evaluated false"})
	inc := b.add(g1, root, demoNode{display: "incremental-sync", typ: "Pod", phase: "Succeeded",
		template: "incremental-sync", at: 8*time.Minute + 6*time.Second, dur: 20 * time.Minute, exit: "0"})
	g2 := b.add(full, root, demoNode{display: "[2]", typ: "StepGroup", phase: "Running",
		at: 28*time.Minute + 10*time.Second, dur: running})
	b.link(inc, g2)
	for _, r := range []string{"eu", "us", "apac"} {
		ph, dur, exit := "Running", running, ""
		if r == "eu" {
			ph, dur, exit = "Succeeded", 9*time.Minute, "0"
		}
		b.add(g2, root, demoNode{display: "pull(" + r + ")", typ: "Pod", phase: ph, template: "pull-region",
			at: 28*time.Minute + 11*time.Second, dur: dur, exit: exit,
			in: core.NodeIO{Parameters: params("region", r)}})
	}
	return b.finish(running, "", nil)
}

// demoCleanup was accepted but the controller has not started it: no nodes.
func demoCleanup(start time.Time) core.Workflow {
	b := newDemo("demo-cleanup", "Pending", start)
	wf := b.finish(running, "", nil)
	wf.Summary.StartedAt = nil
	wf.Resource = b.resourceJSON(wf)
	return wf
}

// demoReleaseGate is parked on a manual approval between test and deploy.
func demoReleaseGate(start time.Time) core.Workflow {
	b := newDemo("demo-release-gate", "Running", start)
	root := b.add("", "", demoNode{display: "demo-release-gate", typ: "Steps", phase: "Running",
		template: "release", dur: running})
	g0 := b.add(root, root, demoNode{display: "[0]", typ: "StepGroup", phase: "Succeeded", at: 2 * time.Second, dur: 3 * time.Minute})
	build := b.add(g0, root, demoNode{display: "build", typ: "Pod", phase: "Succeeded", template: "build",
		at: 2 * time.Second, dur: 3 * time.Minute, exit: "0",
		in:  core.NodeIO{Parameters: params("git-sha", "4f1c2e9", "target", "linux/amd64")},
		out: core.NodeIO{Parameters: params("image", "registry.example/app:4f1c2e9"), Artifacts: []string{"sbom"}}})
	g1 := b.add(build, root, demoNode{display: "[1]", typ: "StepGroup", phase: "Succeeded", at: 3*time.Minute + 5*time.Second, dur: 4 * time.Minute})
	test := b.add(g1, root, demoNode{display: "integration-test", typ: "Pod", phase: "Succeeded", template: "test",
		at: 3*time.Minute + 5*time.Second, dur: 4 * time.Minute, exit: "0"})
	g2 := b.add(test, root, demoNode{display: "[2]", typ: "StepGroup", phase: "Running", at: 7*time.Minute + 10*time.Second, dur: running})
	approve := b.add(g2, root, demoNode{display: "approve-production", typ: "Suspend", phase: "Running",
		template: "approval", at: 7*time.Minute + 10*time.Second, dur: running})
	g3 := b.add(approve, root, demoNode{display: "[3]", typ: "StepGroup", phase: "Pending", at: notStarted})
	_ = g3
	return b.finish(running, "", map[string]string{"workflows.argoproj.io/workflow-template": "release"})
}

// demoBackfill ran out of memory: exit code 137 on its only worker pod.
func demoBackfill(start time.Time) core.Workflow {
	b := newDemo("demo-oom-backfill", "Failed", start)
	root := b.add("", "", demoNode{display: "demo-oom-backfill", typ: "Steps", phase: "Failed",
		template: "backfill", dur: 17 * time.Minute, message: "child 'backfill-2026-q2' failed"})
	g0 := b.add(root, root, demoNode{display: "[0]", typ: "StepGroup", phase: "Failed", at: 2 * time.Second, dur: 17 * time.Minute})
	b.add(g0, root, demoNode{display: "backfill-2026-q2", typ: "Pod", phase: "Failed", template: "backfill-quarter",
		at: 2 * time.Second, dur: 17 * time.Minute, exit: "137",
		message: "OOMKilled (exit code 137)", host: "demo-worker-2",
		in: core.NodeIO{Parameters: params("quarter", "2026-Q2", "memory", "2Gi")}})
	return b.finish(17*time.Minute, "child 'backfill-2026-q2' failed", nil)
}

// demoDeploy is a multi-layer deploy in which only one region was selected,
// so most of the tree is skipped branches. It is the case the skipped-node
// toggle exists for.
func demoDeploy(start time.Time) core.Workflow {
	b := newDemo("demo-deploy-multi-layer", "Succeeded", start)
	root := b.add("", "", demoNode{display: "demo-deploy-multi-layer", typ: "DAG", phase: "Succeeded",
		template: "deploy", dur: 11 * time.Minute})
	plan := b.add(root, root, demoNode{display: "plan", typ: "Pod", phase: "Succeeded", template: "plan",
		at: 3 * time.Second, dur: 90 * time.Second, exit: "0",
		out: core.NodeIO{Parameters: params("regions", `["eu-west"]`)}})
	at := 95 * time.Second
	for _, layer := range []string{"network", "database", "compute", "edge"} {
		layerNode := b.add(plan, root, demoNode{display: layer, typ: "DAG", phase: "Succeeded", template: "layer",
			at: at, dur: 2 * time.Minute, in: core.NodeIO{Parameters: params("layer", layer)}})
		for _, region := range []string{"eu-west", "us-east", "us-west", "ap-south", "ap-east"} {
			if region == "eu-west" {
				b.add(layerNode, layerNode, demoNode{display: "apply-" + region, typ: "Pod", phase: "Succeeded",
					template: "apply", at: at + 2*time.Second, dur: 110 * time.Second, exit: "0",
					in: core.NodeIO{Parameters: params("layer", layer, "region", region)}})
				continue
			}
			b.add(layerNode, layerNode, demoNode{display: "apply-" + region, typ: "Skipped", phase: "Skipped",
				template: "apply", at: at + time.Second, dur: 0,
				message: "when '" + region + " in [\"eu-west\"]' evaluated false"})
		}
		at += 2*time.Minute + 5*time.Second
	}
	return b.finish(11*time.Minute, "", map[string]string{"workflows.argoproj.io/workflow-template": "deploy"})
}

// demoValidation never ran: the spec failed validation, so there are no nodes.
func demoValidation(start time.Time) core.Workflow {
	b := newDemo("demo-param-check", "Error", start)
	return b.finish(0, "invalid spec: templates.main.steps[0].fetch failed to resolve {{inputs.parameters.bucket}}", nil)
}

// demoETLRun is one run a CronWorkflow started. The newest run failed a
// quality check; the older two succeeded.
func demoETLRun(start time.Time, i int) core.Workflow {
	name := fmt.Sprintf("demo-etl-hourly-%d", 1790000000-3600*i)
	phase := "Succeeded"
	if i == 0 {
		phase = "Failed"
	}
	b := newDemo(name, phase, start)
	root := b.add("", "", demoNode{display: name, typ: "Steps", phase: phase, template: "etl", dur: 6 * time.Minute})
	g0 := b.add(root, root, demoNode{display: "[0]", typ: "StepGroup", phase: "Succeeded", at: 2 * time.Second, dur: 4 * time.Minute})
	ingest := b.add(g0, root, demoNode{display: "ingest", typ: "Pod", phase: "Succeeded", template: "ingest",
		at: 2 * time.Second, dur: 4 * time.Minute, exit: "0"})
	g1 := b.add(ingest, root, demoNode{display: "[1]", typ: "StepGroup", phase: phase, at: 4*time.Minute + 5*time.Second, dur: 2 * time.Minute})
	check := demoNode{display: "quality-check", typ: "Pod", phase: phase, template: "quality-check",
		at: 4*time.Minute + 5*time.Second, dur: 2 * time.Minute, exit: "0"}
	msg := ""
	if phase == "Failed" {
		check.exit, check.message = "2", "Error (exit code 2)"
		msg = "child 'quality-check' failed"
	}
	b.add(g1, root, check)
	return b.finish(6*time.Minute, msg, map[string]string{
		"workflows.argoproj.io/cron-workflow": "demo-etl-hourly",
	})
}

// demoHParamSweep is a hyperparameter sweep in the second namespace: a DAG
// fanning out one trial per learning rate, one still running, with the
// selection step waiting on all of them.
func demoHParamSweep(start time.Time) core.Workflow {
	b := newDemoIn(DemoMLNamespace, "demo-ml-hparam-sweep", "Running", start)
	root := b.add("", "", demoNode{display: "demo-ml-hparam-sweep", typ: "DAG", phase: "Running",
		template: "sweep", dur: running, estimate: 25 * time.Minute})
	pick := ""
	for i, lr := range []string{"0.001", "0.0003", "0.0001"} {
		ph, dur, exit := "Succeeded", time.Duration(9+2*i)*time.Minute, "0"
		if i == 2 {
			ph, dur, exit = "Running", running, ""
		}
		trial := b.add(root, root, demoNode{display: fmt.Sprintf("train-trial(%d:%s)", i, lr), typ: "Pod",
			phase: ph, template: "train-trial", at: 3 * time.Second, dur: dur, exit: exit,
			estimate: 12 * time.Minute,
			in:       core.NodeIO{Parameters: params("lr", lr, "epochs", "10")}})
		if pick == "" {
			pick = b.add(trial, root, demoNode{display: "select-best", typ: "Pod", phase: "Pending",
				template: "select-best", at: notStarted})
		} else {
			b.link(trial, pick)
		}
	}
	b.outbound(root, pick)
	return b.finish(running, "", map[string]string{"workflows.argoproj.io/workflow-template": "hparam-sweep"})
}

// demoBatchInfer is a finished batch inference run in the second namespace.
func demoBatchInfer(start time.Time) core.Workflow {
	b := newDemoIn(DemoMLNamespace, "demo-ml-batch-infer", "Succeeded", start)
	root := b.add("", "", demoNode{display: "demo-ml-batch-infer", typ: "Steps", phase: "Succeeded",
		template: "infer", dur: 14 * time.Minute})
	g0 := b.add(root, root, demoNode{display: "[0]", typ: "StepGroup", phase: "Succeeded",
		at: 2 * time.Second, dur: 14 * time.Minute})
	b.add(g0, root, demoNode{display: "score", typ: "Pod", phase: "Succeeded", template: "score",
		at: 2 * time.Second, dur: 14 * time.Minute, exit: "0",
		in:  core.NodeIO{Parameters: params("model", "resnet-lite:7", "batch", "2026-09-06")},
		out: core.NodeIO{Parameters: params("scored", "1250000"), Artifacts: []string{"predictions"}}})
	return b.finish(14*time.Minute, "", nil)
}

// demoLogs fabricates a pod's log for the demo. The text follows the node's
// outcome, so a failed pod's log ends in the error that failed it.
func demoLogs(n core.Node, now time.Time) []core.LogRecord {
	var lines []string
	tmpl := n.TemplateName
	switch {
	case strings.HasPrefix(tmpl, "train"):
		lines = append(lines, "loading features from /artifacts/features", "model: resnet-lite  params=11.2M")
		for e := 1; e <= 10; e++ {
			if n.Phase == "Running" && e > 6 {
				break
			}
			lines = append(lines, fmt.Sprintf("epoch %d/10  loss=%.3f  val_acc=%.3f", e, 0.9/float64(e+1), 0.55+0.04*float64(e)))
		}
	case tmpl == "backfill-quarter":
		for p := 1; p <= 8; p++ {
			lines = append(lines, fmt.Sprintf("partition %d/12 written (%d rows)", p, 250000*p))
		}
		lines = append(lines, "WARN heap usage 1.9Gi of 2Gi limit", "Killed")
	case tmpl == "transform":
		lines = append(lines, "reading raw-extract (184022 rows)", "applying 14 transforms",
			"ERROR column 'revenue_eur' has 312 nulls; schema requires non-null",
			"Traceback (most recent call last):", "  File \"/app/transform.py\", line 88, in <module>",
			"SchemaViolation: revenue_eur: expected non-null", "exit status 1")
	case tmpl == "quality-check":
		lines = append(lines, "checking 6 expectations")
		if n.Phase == "Failed" {
			lines = append(lines, "FAIL expect_row_count_between(1e5, 5e5): got 41210", "1 of 6 expectations failed")
		} else {
			lines = append(lines, "all 6 expectations passed")
		}
	default:
		lines = append(lines, "starting "+tmpl, "config loaded", "working...")
		if n.Phase == "Succeeded" {
			lines = append(lines, tmpl+" finished")
		}
	}
	at := now
	if n.StartedAt != nil {
		at = *n.StartedAt
	}
	out := make([]core.LogRecord, 0, len(lines))
	for i, l := range lines {
		ts := at.Add(time.Duration(i) * 3 * time.Second).UTC().Format(time.RFC3339)
		out = append(out, core.LogRecord{PodName: n.PodName, Container: "main", Content: ts + " " + l, ReceivedAt: now})
	}
	return out
}
