package diagnose

import (
	"sort"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// wfBuilder supplies a node map and a branch tree for the rules.
type wfBuilder struct {
	wf         core.Workflow
	start, now time.Time
	after      map[string][]string
	roles      map[string]string
}

// newWF starts a workflow in phase, begun an hour before the fixture epoch.
// A finished workflow ended ten minutes in; the clock reads the epoch.
func newWF(phase string) *wfBuilder {
	start := testkit.FixtureEpoch.Add(-time.Hour)
	b := &wfBuilder{start: start, now: testkit.FixtureEpoch, after: map[string][]string{}, roles: map[string]string{}}
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

// after gives a task the dependencies supplied to the rules.
func after(deps ...string) opt {
	return func(n *core.Node, b *wfBuilder) { b.after[n.ID] = deps }
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

// task adds a task to the workflow's branch in pipeline order.
func (b *wfBuilder) task(name, phase string, opts ...opt) {
	n := b.add(name, phase, opts...)
	root := b.wf.Nodes["wf"]
	root.Children = append(root.Children, n.ID)
	b.wf.Nodes["wf"] = root
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
	b.roles[n.ID] = "exit handler"
}

// hook adds a lifecycle hook's pod.
func (b *wfBuilder) hook(name, phase string, opts ...opt) {
	n := b.add(name, phase, opts...)
	n.Name, n.BoundaryID, n.Hooked = "wf.hooks."+name, "", true
	b.wf.Nodes[n.ID] = n
	b.roles[n.ID] = "hook"
}

// input supplies the tree, dependencies, roles and times independently of the view.
func (b *wfBuilder) input() Input {
	var branch func(string) Branch
	branch = func(id string) Branch {
		n := b.wf.Nodes[id]
		out := Branch{ID: id, After: b.after[id], Role: b.roles[id]}
		if n.StartedAt != nil {
			out.Ran, out.Start, out.End = true, *n.StartedAt, b.now
			if n.FinishedAt != nil {
				out.End = *n.FinishedAt
			}
		}
		for _, child := range n.Children {
			out.Children = append(out.Children, branch(child))
		}
		return out
	}
	in := Input{Workflow: b.wf, Start: b.start, Now: b.now, Tree: []Branch{branch("wf")}}
	if _, ok := b.wf.Nodes["wf-onexit"]; ok {
		in.Tree = append(in.Tree, branch("wf-onexit"))
	}
	var hooks []string
	for id, role := range b.roles {
		if role == "hook" {
			hooks = append(hooks, id)
		}
	}
	sort.Strings(hooks)
	for _, id := range hooks {
		in.Tree = append(in.Tree, branch(id))
	}
	return in
}
