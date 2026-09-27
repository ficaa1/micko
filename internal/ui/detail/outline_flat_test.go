package detail

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// gateWorkflow has a handful of nodes that ran, buried under a pile of
// skipped branches, with one Suspend node holding everything up.
func gateWorkflow() core.Workflow {
	node := func(id, name, typ, phase, boundary string, children ...string) core.Node {
		return core.Node{
			ID: id, Name: name, DisplayName: name, Type: typ, Phase: phase,
			BoundaryID: boundary, Children: children,
		}
	}
	nodes := map[string]core.Node{
		"root": node("root", "deploy", "Steps", "Running", "", "plan", "skip-a", "approval"),
		"plan": node("plan", "plan-prd", "Pod", "Succeeded", "root"),
		"skip-a": {
			ID: "skip-a", Name: "plan-dev", DisplayName: "plan-dev", Type: "Skipped",
			Phase: "Skipped", Message: "when 'false' evaluated false", BoundaryID: "root",
			Children: []string{"skip-b"},
		},
		"skip-b": {
			ID: "skip-b", Name: "apply-dev", DisplayName: "apply-dev", Type: "Skipped",
			Phase: "Skipped", Message: "when 'false' evaluated false", BoundaryID: "skip-a",
		},
		"approval": node("approval", "approval", "Steps", "Running", "root", "suspend"),
		"suspend":  node("suspend", "suspend", "Suspend", "Running", "approval"),
	}
	nodes["plan"] = core.Node{
		ID: "plan", Name: "deploy.plan-prd", DisplayName: "plan-prd", Type: "Pod",
		Phase: "Succeeded", BoundaryID: "root", PodName: "deploy-terraform-1234",
	}
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: "deploy", UID: "u1"}, Phase: "Running", Suspended: true},
		Nodes:          nodes,
		NodesAvailable: true,
	}
}

// Skipped branches are most of a deployment workflow and say nothing about
// its progress, so they are hidden by default — with the count stated.
func TestFlattenHidesSkippedSubtreesByDefault(t *testing.T) {
	out := BuildNodeOutline(gateWorkflow(), OutlineOptions{})

	shown, hidden := FlattenOutline(out, true)
	if hidden != 2 {
		t.Fatalf("hidden = %d, want 2 (the skipped node and its child)", hidden)
	}
	for _, r := range shown {
		if r.Skipped() {
			t.Fatalf("a skipped node survived hiding: %s", r.Row.Name)
		}
	}

	all, hidden := FlattenOutline(out, false)
	if hidden != 0 {
		t.Fatalf("hidden = %d with hiding off, want 0", hidden)
	}
	if len(all) != len(shown)+2 {
		t.Fatalf("showing skipped added %d rows, want 2", len(all)-len(shown))
	}
}

// The gate is the one row a reader is looking for, so it must be findable.
func TestFlattenMarksTheSuspendGate(t *testing.T) {
	rows, _ := FlattenOutline(BuildNodeOutline(gateWorkflow(), OutlineOptions{}), true)
	var gate *FlatRow
	for i := range rows {
		if rows[i].Suspended() {
			gate = &rows[i]
		}
	}
	if gate == nil {
		t.Fatal("no row reported itself as the suspend gate")
	}
	line := RenderFlatRow(*gate, 0, shared.NewTheme(true), false)
	if !strings.Contains(line, "AWAITING RESUME") {
		t.Fatalf("gate row does not say it is waiting: %q", line)
	}
}

// A child must be drawn under its parent, or the tree tells the reader
// nothing about the structure.
func TestFlattenDrawsTreeConnectors(t *testing.T) {
	rows, _ := FlattenOutline(BuildNodeOutline(gateWorkflow(), OutlineOptions{}), true)
	if rows[0].Prefix != "" {
		t.Fatalf("the root row must have no connector, got %q", rows[0].Prefix)
	}
	deep := false
	for _, r := range rows[1:] {
		if strings.Contains(r.Prefix, "├─ ") || strings.Contains(r.Prefix, "└─ ") {
			deep = true
		}
	}
	if !deep {
		t.Fatal("no child row carried a tree connector")
	}
}

// The cursor addresses the rows the view drew, and l only offers logs for a
// node that actually owns a pod.
func TestNodesTabCursorAndLogsIntent(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab") // summary -> nodes

	if _, ok := m.SelectedNode(); !ok {
		t.Fatal("the nodes tab must start with a row selected")
	}

	// Walk to the pod node and ask for its logs.
	var found bool
	for i := 0; i < 20; i++ {
		row, ok := m.SelectedNode()
		if ok && row.PodName != "" {
			found = true
			break
		}
		m.handleKey("j")
	}
	if !found {
		t.Fatal("never reached the pod node with j")
	}
	cmd := m.handleKey("l")
	if cmd == nil {
		t.Fatal("l on a pod node produced no logs intent")
	}
	intent, ok := cmd().(NodeLogsIntent)
	if !ok {
		t.Fatalf("intent type = %T", cmd())
	}
	if intent.PodName != "deploy-terraform-1234" {
		t.Fatalf("intent pod = %q", intent.PodName)
	}
}

// A node with no pod has no logs. The key must stay inert rather than open an
// empty stream that looks like a failure.
func TestLogsKeyIsInertOnANodeWithoutAPod(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab")
	for i := 0; i < 20; i++ {
		row, ok := m.SelectedNode()
		if ok && row.Type == "Suspend" {
			if cmd := m.handleKey("l"); cmd != nil {
				t.Fatal("a Suspend node has no pod, so l must do nothing")
			}
			return
		}
		m.handleKey("j")
	}
	t.Fatal("never reached the Suspend node")
}

// h answers "forty lines of skipped nodes"; the status line has to say what
// is hidden, or the tree looks truncated instead of filtered. Once nothing is
// hidden the line says nothing, because the footer already carries the key.
func TestHideSkippedToggleIsReported(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab")

	if !strings.Contains(m.tabStatusLine(), "skipped hidden") {
		t.Fatalf("status line does not report hidden rows: %q", m.tabStatusLine())
	}
	if strings.Contains(m.tabStatusLine(), "(h ") {
		t.Fatalf("status line repeats a key the footer already shows: %q", m.tabStatusLine())
	}
	m.handleKey("h")
	if strings.Contains(m.tabStatusLine(), "skipped") {
		t.Fatalf("nothing is hidden, so the line must not mention skipped rows: %q", m.tabStatusLine())
	}
}

// The phase filter is how a reader finds the one failed step in a workflow of
// forty. It must narrow the rows and say what it is holding back.
func TestNodePhaseFilterNarrowsTheTab(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab")
	all := len(m.nodes)

	m.handleKey("p") // All -> Failed
	if m.nodePhase != NodePhaseFailed {
		t.Fatalf("phase = %q, want Failed first in the rotation", m.nodePhase)
	}
	for _, r := range m.nodes {
		if !NodePhaseFailed.Matches(r) {
			t.Fatalf("row %q survived the Failed filter with phase %q", r.Row.Name, r.Row.Phase)
		}
	}
	if line := m.tabStatusLine(); !strings.Contains(line, "phase Failed") {
		t.Fatalf("status line does not name the filter: %q", line)
	}

	// Round the rotation back to All and the tree comes back whole.
	for i := 0; i < len(nodePhaseCycle)-1; i++ {
		m.handleKey("p")
	}
	if m.nodePhase != NodePhaseAll || len(m.nodes) != all {
		t.Fatalf("rotation did not return to the full tree: phase=%q rows=%d want %d", m.nodePhase, len(m.nodes), all)
	}
}

// A tall node tree must be reachable. Before this the pane drew only what fit
// and there was no way to move down.
func TestNodesTabScrollsToTheLastRow(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 5) // a pane far shorter than the tree
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab")
	m.handleKey("G")

	rows, _ := FlattenOutline(m.state.Outline, m.hideSkipped)
	if m.nodeCursor != len(rows)-1 {
		t.Fatalf("G left the cursor at %d, want %d", m.nodeCursor, len(rows)-1)
	}
	body := m.BodyLines()
	last := rows[len(rows)-1]
	if !strings.Contains(strings.Join(body, "\n"), rowDisplayName(last.Row)) {
		t.Fatalf("the last node is not on screen after G:\n%s", strings.Join(body, "\n"))
	}
}

// gg is two presses. A single g must not jump, and it must not swallow the
// key that follows it.
func TestGGPrefixDoesNotSwallowTheNextKey(t *testing.T) {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetWorkflow(gateWorkflow(), time.Now())
	m.handleKey("tab")
	m.handleKey("j")
	m.handleKey("j")
	at := m.nodeCursor

	m.handleKey("g") // arms only
	if m.nodeCursor != at {
		t.Fatalf("a single g moved the cursor to %d", m.nodeCursor)
	}
	m.handleKey("g")
	if m.nodeCursor != 0 {
		t.Fatalf("gg left the cursor at %d, want 0", m.nodeCursor)
	}

	m.handleKey("j")
	m.handleKey("g")
	m.handleKey("j") // disarms g, then moves
	if m.nodeCursor != 2 {
		t.Fatalf("g then j left the cursor at %d, want 2", m.nodeCursor)
	}
}

var _ = tea.KeyPressMsg{}
