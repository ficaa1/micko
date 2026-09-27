// Package diagnose explains a workflow's outcome from what the workflow
// itself records: its phase and message, each node's phase, message, exit
// code and times, the order the pipeline ran in, and, when the caller has
// read it, the end of the failing pod's log. It is deterministic and local:
// the same input always gives the same findings, and nothing in it talks to
// a server or a model.
//
// Explain applies a fixed set of rules. Each rule that holds produces a
// Finding: a severity, a one-line headline, the evidence it rests on and a
// concrete next step. The findings come back most severe first, so the
// first one answers "what went wrong" and the rest add what followed from
// it.
package diagnose

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Severity is how much a finding matters to someone asking why a workflow
// did what it did.
type Severity int

const (
	// Info states what happened with nothing to fix.
	Info Severity = iota
	// Warning is something that went wrong without failing the run, or
	// that will if nobody acts.
	Warning
	// Error is a failure.
	Error
)

// String is the severity's word, which every view prints beside its glyph so
// the severity never rests on colour alone.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// Rule names, one per kind of finding. Views do not branch on them; tests
// and the report pin them.
const (
	RuleRootFailure       = "root-failure"
	RuleMoreFailures      = "more-failures"
	RuleValidation        = "validation"
	RuleWorkflowFailed    = "workflow-failed"
	RuleDeadline          = "deadline"
	RuleDependents        = "dependents"
	RuleExitHandler       = "exit-handler"
	RuleHook              = "hook"
	RuleSuspended         = "suspended"
	RuleRunning           = "running"
	RuleOverdue           = "overdue"
	RulePodPending        = "pod-pending"
	RuleNotStarted        = "not-started"
	RuleSucceeded         = "succeeded"
	RuleRetried           = "retried"
	RuleTolerated         = "tolerated"
	RuleLog               = "log"
	RuleNodesUnavailable  = "nodes-unavailable"
	RuleUnknownPhase      = "unknown-phase"
	RuleWorkflowSuspended = "workflow-suspended"
)

// Causes a failing node's finding can carry: what the node's exit code and
// message say stopped it.
const (
	CauseOOM        = "oom"
	CauseExitCode   = "exit-code"
	CauseImagePull  = "image-pull"
	CausePodPending = "pod-pending"
	CauseDeadline   = "deadline"
	CauseMessage    = "message"
)

// Finding is one conclusion and what it rests on.
type Finding struct {
	Rule     string
	Severity Severity
	// Cause is what stopped a failing node, one of the Cause constants;
	// empty for findings about something else.
	Cause    string
	Headline string
	// NodeID is the node the finding is about; empty for the workflow.
	NodeID   string
	Evidence []Evidence
	Next     string
}

// Evidence is one labelled fact. Text is the fact as a sentence or a list;
// Params carries parameter values separately, because they can hold secrets
// and the view decides whether to show them; Log carries picked log lines.
type Evidence struct {
	Label  string
	Text   string
	Params []core.Parameter
	Log    []LogLine
}

// Input is everything the rules read.
type Input struct {
	Workflow core.Workflow
	// Tree is the node tree in pipeline order, as the detail pane draws it:
	// retry attempts under their Retry node, steps under their Steps node,
	// the exit handler as a tree of its own.
	Tree []Branch
	// Start is when the run started, the earliest start of the workflow or
	// any node. Times in the findings are offsets from it.
	Start time.Time
	// Now is the clock the waiting and running times are measured to.
	Now time.Time
	// Log is the log read for the first failing pod. nil means no log is
	// read at all, as in a static render; a Log for another pod, or one
	// still loading, says the log is on its way.
	Log *Log
}

// Branch is one node of the tree and what it waited for.
type Branch struct {
	ID string
	// After holds the IDs of the siblings the node waited for before it
	// could start.
	After []string
	// Role names what a top-level tree other than the workflow's own is
	// for: "exit handler", or "hook" for another lifecycle hook.
	Role string
	// Ran reports that the node started. Start and End are when it ran;
	// a node still running ends now.
	Ran        bool
	Start, End time.Time
	Children   []Branch
}

// LogState is how far reading a log has got.
type LogState int

const (
	// LogLoading means the read is under way.
	LogLoading LogState = iota
	// LogRead means the read finished; Lines holds what came back.
	LogRead
	// LogFailed means the read failed; Err says why.
	LogFailed
)

// Log is the end of one pod's log as the caller read it.
type Log struct {
	NodeID    string
	PodName   string
	Container string
	State     LogState
	// Lines are the last lines the read returned, oldest first.
	Lines []string
	// Tail is how many lines were asked for.
	Tail int
	// Err is why the read failed, already safe to show.
	Err string
	// Gone reports that the server no longer has the pod or its log.
	Gone bool
}

// Report is Explain's answer.
type Report struct {
	Findings []Finding
	// LogNode and LogPod name the pod whose log the findings want as
	// evidence: the first failing pod. Both are empty when no pod failed or
	// its name is not known.
	LogNode, LogPod string
}

// Counts is how many findings of each severity a report holds.
func (r Report) Counts() (errors, warnings, infos int) {
	for _, f := range r.Findings {
		switch f.Severity {
		case Error:
			errors++
		case Warning:
			warnings++
		default:
			infos++
		}
	}
	return errors, warnings, infos
}

// Explain applies every rule to the input and returns the findings, most
// severe first. Findings of the same severity keep the order the rules
// produced them in: the root failure before what followed from it.
func Explain(in Input) Report {
	x := newIndex(in)
	var r Report
	add := func(fs ...Finding) { r.Findings = append(r.Findings, fs...) }

	wf := in.Workflow
	switch {
	case !wf.NodesAvailable:
		add(nodesUnavailable(x))
		if f, ok := validation(x); ok {
			add(f)
		}
	case len(wf.Nodes) == 0:
		if f, ok := validation(x); ok {
			add(f)
		} else if f, ok := notStarted(x); ok {
			add(f)
		}
	default:
		roots := x.rootFailures()
		for i, u := range roots {
			if i == maxRootCards {
				add(moreFailures(x, roots[i:]))
				break
			}
			f := x.failureFinding(u, i == 0, len(roots) > 1)
			if i == 0 {
				r.LogNode, r.LogPod = x.logTarget(u)
				f = x.withLog(f, r.LogNode, r.LogPod)
			}
			add(f)
		}
		if len(roots) == 0 && failedPhase(wf.Summary.Phase) {
			add(workflowFailed(x))
		}
		if f, ok := x.deadline(roots); ok {
			add(f)
		}
		if f, ok := x.dependents(roots); ok {
			add(f)
		}
		add(x.exitHandlers()...)
		add(x.podPending()...)
		add(x.retriedThenSucceeded()...)
		if f, ok := x.tolerated(); ok {
			add(f)
		}
		add(x.suspended()...)
		if f, ok := x.running(); ok {
			add(f)
		}
		if f, ok := x.succeeded(); ok {
			add(f)
		}
		if f, ok := x.logFinding(r.LogNode, r.LogPod); ok {
			add(f)
		}
	}
	if f, ok := x.workflowSuspended(); ok {
		add(f)
	}
	if len(r.Findings) == 0 {
		add(unknownPhase(x))
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return r.Findings[i].Severity > r.Findings[j].Severity
	})
	return r
}

// maxRootCards is how many failing nodes get a card of their own. A fan-out
// where fifty shards failed the same way needs three examples and a count,
// not fifty cards.
const maxRootCards = 3

// index is the tree the rules walk, keyed by node ID.
type index struct {
	in     Input
	nodes  map[string]core.Node
	branch map[string]*Branch
	parent map[string]string
	// role is the role of the top-level tree each node sits in.
	role map[string]string
	// order is every node in the tree, parents before children, in
	// pipeline order.
	order []string
	// failedBelow marks the nodes with a failed node somewhere under them.
	failedBelow map[string]bool
	spec        workflowSpec
}

// workflowSpec is what the rules read from the workflow's own spec.
type workflowSpec struct {
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds"`
	Suspend               *bool  `json:"suspend"`
}

func newIndex(in Input) *index {
	x := &index{
		in:          in,
		nodes:       in.Workflow.Nodes,
		branch:      map[string]*Branch{},
		parent:      map[string]string{},
		role:        map[string]string{},
		failedBelow: map[string]bool{},
	}
	var walk func(b *Branch, parent, role string)
	walk = func(b *Branch, parent, role string) {
		if _, seen := x.branch[b.ID]; seen {
			return
		}
		x.branch[b.ID] = b
		x.parent[b.ID] = parent
		x.role[b.ID] = role
		x.order = append(x.order, b.ID)
		for i := range b.Children {
			walk(&b.Children[i], b.ID, role)
		}
	}
	for i := range in.Tree {
		walk(&in.Tree[i], "", in.Tree[i].Role)
	}
	for i := len(x.order) - 1; i >= 0; i-- {
		id := x.order[i]
		for _, c := range x.branch[id].Children {
			if failedPhase(x.nodes[c.ID].Phase) || x.failedBelow[c.ID] {
				x.failedBelow[id] = true
				break
			}
		}
	}
	if len(in.Workflow.Resource) > 0 {
		var doc struct {
			Spec workflowSpec `json:"spec"`
		}
		if json.Unmarshal(in.Workflow.Resource, &doc) == nil {
			x.spec = doc.Spec
		}
	}
	return x
}

// failedPhase reports whether a phase is a failure.
func failedPhase(p string) bool { return p == "Failed" || p == "Error" }

// finishedPhase reports whether a phase is final.
func finishedPhase(p string) bool {
	switch p {
	case "Succeeded", "Failed", "Error", "Skipped", "Omitted":
		return true
	}
	return false
}

// name is the node's display name, falling back to its full name and ID.
func (x *index) name(id string) string {
	n, ok := x.nodes[id]
	switch {
	case !ok:
		return id
	case n.DisplayName != "":
		return n.DisplayName
	case n.Name != "":
		return n.Name
	default:
		return id
	}
}

// template is the template a node ran.
func (x *index) template(id string) string {
	n := x.nodes[id]
	if n.TemplateName != "" {
		return n.TemplateName
	}
	return n.TemplateRefTemplate
}

// offset is how far into the run t falls, as text.
func (x *index) offset(t time.Time) string {
	if x.in.Start.IsZero() || t.IsZero() {
		return ""
	}
	return shared.ShortDuration(t.Sub(x.in.Start))
}

// ran reports when a node ran, and false for one that never started.
func (x *index) ran(id string) (start, end time.Time, ok bool) {
	b := x.branch[id]
	if b == nil || !b.Ran {
		return time.Time{}, time.Time{}, false
	}
	return b.Start, b.End, true
}

// path is the chain of display names from the top of the tree to the node.
func (x *index) path(id string) []string {
	var out []string
	for cur := id; cur != ""; cur = x.parent[cur] {
		out = append(out, x.name(cur))
		if len(out) > 64 {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// under reports whether id sits in the subtree of top, top included.
func (x *index) under(id, top string) bool {
	for cur := id; cur != ""; cur = x.parent[cur] {
		if cur == top {
			return true
		}
	}
	return false
}
