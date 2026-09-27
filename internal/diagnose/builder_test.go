package diagnose_test

import (
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/detail"
)

// wfBuilder builds a synthetic DAG workflow named "wf" in the controller's
// shapes: a DAG root whose children are the tasks that wait for nothing,
// each task listing the tasks that wait for it as its children, retry
// attempts under their Retry node, and the exit handler as a tree of its
// own named "wf.onExit".
type wfBuilder struct {
	wf         core.Workflow
	start, now time.Time
}

// newWF starts a workflow in phase, begun an hour before the fixture epoch.
// A finished workflow ended ten minutes in; the clock reads the epoch.
func newWF(phase string) *wfBuilder {
	start := testkit.FixtureEpoch.Add(-time.Hour)
	b := &wfBuilder{start: start, now: testkit.FixtureEpoch}
	b.wf = core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: "wf", UID: "uid-wf"},
			Phase: phase, CreatedAt: start, StartedAt: &start,
		},
		Nodes:          map[string]core.Node{},
		NodesAvailable: true,
	}
	root := core.Node{ID: "wf", Name: "wf", DisplayName: "wf", Type: "DAG", Phase: phase, StartedAt: &start}
	switch phase {
	case "Succeeded", "Failed", "Error":
		end := start.Add(10 * time.Minute)
		b.wf.Summary.FinishedAt = &end
		root.FinishedAt = &end
	case "Pending":
		root.Phase = "Running"
	}
	b.wf.Nodes["wf"] = root
	return b
}

// opt sets one field of a node.
type opt func(*core.Node, *wfBuilder)

// ran places a node at seconds into the run for dur seconds; a negative dur
// means it is still running.
func ran(at, dur float64) opt {
	return func(n *core.Node, b *wfBuilder) {
		s := b.start.Add(time.Duration(at * float64(time.Second)))
		n.StartedAt = &s
		if dur >= 0 {
			e := s.Add(time.Duration(dur * float64(time.Second)))
			n.FinishedAt = &e
		}
	}
}

// notRun leaves a node without times.
func notRun() opt { return func(*core.Node, *wfBuilder) {} }

func exit(code string) opt { return func(n *core.Node, _ *wfBuilder) { n.ExitCode = code } }
func msg(m string) opt     { return func(n *core.Node, _ *wfBuilder) { n.Message = m } }
func typ(t string) opt     { return func(n *core.Node, _ *wfBuilder) { n.Type = t } }
func pod() opt             { return func(n *core.Node, _ *wfBuilder) { n.PodName = "wf-" + n.DisplayName } }

func inputs(kv ...string) opt {
	return func(n *core.Node, _ *wfBuilder) {
		for i := 0; i+1 < len(kv); i += 2 {
			n.Inputs.Parameters = append(n.Inputs.Parameters, core.Parameter{Name: kv[i], Value: kv[i+1]})
		}
	}
}

// after makes the node wait for the named tasks: each lists it as a child,
// the way a DAG task lists its dependents.
func after(deps ...string) opt {
	return func(n *core.Node, b *wfBuilder) {
		for _, d := range deps {
			p := b.wf.Nodes[d]
			p.Children = append(p.Children, n.ID)
			b.wf.Nodes[d] = p
		}
	}
}

// add creates a node with the given options.
func (b *wfBuilder) add(name, phase string, opts ...opt) core.Node {
	n := core.Node{ID: name, Name: "wf." + name, DisplayName: name, Type: "Pod", Phase: phase,
		BoundaryID: "wf", TemplateName: name}
	for _, o := range opts {
		o(&n, b)
	}
	b.wf.Nodes[name] = n
	return n
}

// task adds a DAG task. One that waits for nothing hangs off the root.
func (b *wfBuilder) task(name, phase string, opts ...opt) {
	n := b.add(name, phase, opts...)
	waits := false
	for _, other := range b.wf.Nodes {
		for _, c := range other.Children {
			if c == n.ID && other.ID != "wf" {
				waits = true
			}
		}
	}
	if !waits {
		root := b.wf.Nodes["wf"]
		root.Children = append(root.Children, n.ID)
		b.wf.Nodes["wf"] = root
	}
}

// attemptSpec is one attempt of a Retry node.
type attemptSpec struct {
	phase string
	opts  []opt
}

func attempt(phase string, opts ...opt) attemptSpec { return attemptSpec{phase, opts} }

// retry adds a Retry task whose attempts are name(0), name(1), and so on.
func (b *wfBuilder) retry(name, phase string, when opt, attempts ...attemptSpec) {
	b.task(name, phase, when, typ("Retry"))
	r := b.wf.Nodes[name]
	for i, a := range attempts {
		id := name + "(" + string(rune('0'+i)) + ")"
		n := b.add(id, a.phase, append([]opt{func(n *core.Node, _ *wfBuilder) { n.Retried = true; n.TemplateName = name }}, a.opts...)...)
		r.Children = append(r.Children, n.ID)
	}
	b.wf.Nodes[name] = r
}

// exitHandler adds the exit handler's pod.
func (b *wfBuilder) exitHandler(phase string, opts ...opt) {
	n := b.add("wf-onexit", phase, opts...)
	n.Name, n.DisplayName, n.BoundaryID, n.Hooked, n.TemplateName = "wf.onExit", "onExit", "", true, "notify"
	b.wf.Nodes[n.ID] = n
}

// hook adds a lifecycle hook's pod.
func (b *wfBuilder) hook(name, phase string, opts ...opt) {
	n := b.add(name, phase, opts...)
	n.Name, n.BoundaryID, n.Hooked = "wf.hooks."+name, "", true
	b.wf.Nodes[n.ID] = n
}

// input is the rules' input for the workflow at the builder's clock.
func (b *wfBuilder) input() diagnose.Input { return detail.ExplainInput(b.wf, b.now) }
