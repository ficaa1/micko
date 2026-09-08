// Package detail implements the workflow detail view: summary, the
// deterministic node outline (no geometric DAG renderer, plan §8 C1) and
// the redacted resource viewer.
//
// Ownership rules baked into this package (plan §5 "Detail and node model"):
//   - children group template boundaries; they are NOT dependency edges and
//     outboundNodes are never rendered as tree children (DET-09).
//   - Every node in the map must be reachable in the outline or listed in
//     an explicit ungrouped section (DET-08); cycles are cut; traversal is
//     linear (visited set), never exponential.
//   - Node IDs are never assumed to be pod names (DET-11).
package detail

import (
	"sort"
	"time"

	"argo-tui/internal/core"
)

// OutlineRow is one node row in the hierarchical outline.
type OutlineRow struct {
	NodeID      string
	Name        string
	DisplayName string
	Type        string
	Phase       string
	Message     string
	// HasPod reports pod/log potential per the pinned node-type list
	// (docs/protocol.md §8: Pod/ContainerSet/HTTP/Plugin plus container-set
	// children). It does NOT imply a known pod name.
	HasPod bool
	// StartedAt/FinishedAt pass through for the selected-node pane; nil
	// means "not yet started" (protocol §2: absent phase/timestamps are
	// meaningful, never rendered as empty success).
	StartedAt  *time.Time
	FinishedAt *time.Time
	Children   []OutlineRow
}

// Outline is the result of the deterministic outline build.
type Outline struct {
	Rows []OutlineRow
	// Available mirrors Workflow.NodesAvailable. false means the node map
	// is unusable (offloaded/unhydrated) and Rows is empty — the view
	// renders the explicit unavailable state with UnavailableReason (DET-04).
	Available         bool
	UnavailableReason string
	// Dangling lists node IDs referenced by children/boundary grouping but
	// missing from the node map (DET-08: explicit ungrouped section).
	Dangling []OutlineRow
	// Unreachable lists nodes present in the map but referenced by no
	// parent grouping (DET-08: never silently dropped).
	Unreachable []OutlineRow
}

// OutlineOptions tunes the outline build. The zero value is the default
// deterministic outline.
type OutlineOptions struct {
	// Noop placeholder for future view-level toggles; the deterministic
	// build itself takes no options today.
	_ bool
}

// BuildNodeOutline builds the deterministic hierarchical outline from the
// workflow's node map.
//
// Determinism rules (DET-10): siblings order by (start time, node ID);
// nodes without a start time sort after timestamped siblings (documented
// rule for absent timestamps). The root rows are the workflow-level
// boundary nodes (nodes with no effective parent grouping), ordered by the
// same rule.
//
// Traversal is linear: every node is visited at most once via the visited
// set, so even adversarial cyclic references terminate in O(n+e) with no
// exponential expansion (DET-08, PERF-02).
func BuildNodeOutline(wf core.Workflow, _ OutlineOptions) Outline {
	if !wf.NodesAvailable {
		return Outline{
			Available:         false,
			UnavailableReason: wf.NodesUnavailableReason,
		}
	}

	// referenced collects every node ID that some other node claims as a
	// child. OutboundNodes are deliberately excluded: they are completion
	// markers, not grouping (DET-09).
	referenced := make(map[string]bool, len(wf.Nodes))
	for _, n := range wf.Nodes {
		for _, c := range n.Children {
			referenced[c] = true
		}
	}

	// Build child lists by boundaryID for nodes that are not direct
	// children of their boundary: Argo groups pods under their template
	// invocation boundary via boundaryID; DAG/Steps task nodes appear in
	// the boundary's children list. To avoid double-parenting (a node both
	// in a children list AND boundary-grouped elsewhere), children-list
	// membership wins and boundaryID fills the rest.
	boundaryChildren := make(map[string][]string, len(wf.Nodes))
	for id, n := range wf.Nodes {
		if id == n.BoundaryID || n.BoundaryID == "" {
			continue
		}
		b, ok := wf.Nodes[n.BoundaryID]
		if !ok {
			continue // missing boundary: node stays top-level/unreachable
		}
		if referenced[id] && containsNode(b.Children, id) {
			continue // already grouped as a direct child of its boundary
		}
		if referenced[id] {
			// Node is in someone's children list but its boundary claims
			// it too; keep the children-list parent (first-wins by node
			// ID scan order below would be nondeterministic, so we only
			// assign boundary grouping when there is no children-list
			// parent at all).
			continue
		}
		boundaryChildren[n.BoundaryID] = append(boundaryChildren[n.BoundaryID], id)
	}

	visited := make(map[string]bool, len(wf.Nodes))
	var build func(id string, depth int) (OutlineRow, bool)
	build = func(id string, depth int) (OutlineRow, bool) {
		// Cycle guard: a repeated visit is cut (returns not-ok and the
		// caller records it as a dangling edge — safer than duplicating).
		if visited[id] {
			return OutlineRow{}, false
		}
		n, ok := wf.Nodes[id]
		if !ok {
			return OutlineRow{}, false
		}
		visited[id] = true
		row := outlineRowOf(n)

		kids := append(append([]string(nil), n.Children...), boundaryChildren[id]...)
		rows := make([]OutlineRow, 0, len(kids))
		for _, kid := range kids {
			if kid == id {
				continue // self-reference
			}
			if visited[kid] {
				continue // cycle cut deterministically (first parent wins)
			}
			child, ok := build(kid, depth+1)
			if !ok {
				// Missing child node or cycle-cut: recorded for the
				// ungrouped section by the caller-level pass below.
				continue
			}
			rows = append(rows, child)
		}
		if len(rows) > 0 {
			sortOutlineRows(rows)
			row.Children = rows
		}
		return row, true
	}

	// Root candidates: nodes that are nobody's child and have no
	// resolvable boundary parent. Sorted deterministically first.
	var rootIDs []string
	for id := range wf.Nodes {
		if referenced[id] {
			continue
		}
		if b := wf.Nodes[id].BoundaryID; b != "" && b != id {
			if _, ok := wf.Nodes[b]; ok {
				// boundary exists: the node attaches to it via boundary
				// grouping, it is not a root.
				continue
			}
			// missing boundary: node falls back to top-level so it stays
			// visible (DET-08: never silently dropped).
		}
		rootIDs = append(rootIDs, id)
	}
	sort.Strings(rootIDs)

	out := Outline{Available: true}
	out.Rows = make([]OutlineRow, 0, len(rootIDs))
	for _, id := range rootIDs {
		if visited[id] {
			continue
		}
		row, ok := build(id, 0)
		if !ok {
			continue
		}
		out.Rows = append(out.Rows, row)
	}

	// Second pass: anything the recursion skipped because a child was
	// already visited (diamond/cycle) or missing must still surface exactly
	// once or in the ungrouped sections. Sweep every node ID in map order
	// (sorted) to guarantee full coverage.
	allIDs := make([]string, 0, len(wf.Nodes))
	for id := range wf.Nodes {
		allIDs = append(allIDs, id)
	}
	sort.Strings(allIDs)
	for _, id := range allIDs {
		if visited[id] {
			continue
		}
		row, ok := build(id, 0)
		if ok {
			out.Rows = append(out.Rows, row)
		}
	}
	if len(out.Rows) > 1 {
		sortOutlineRows(out.Rows)
	}

	// Cycle-cut sweep: nodes still unvisited at this point are only
	// reachable through a cycle (or a missing parent). Walk them once in
	// sorted order — boundary-less nodes first so a cycle's entry point is
	// its lexicographically-smallest member and the cut lands inside the
	// back-edge, not the root edge.
	var cycleIDs []string
	for _, id := range allIDs {
		if !visited[id] {
			cycleIDs = append(cycleIDs, id)
		}
	}
	sort.SliceStable(cycleIDs, func(i, j int) bool {
		bi, bj := wf.Nodes[cycleIDs[i]].BoundaryID == "", wf.Nodes[cycleIDs[j]].BoundaryID == ""
		if bi != bj {
			return bi // boundary-less nodes first
		}
		return cycleIDs[i] < cycleIDs[j]
	})
	for _, id := range cycleIDs {
		if visited[id] {
			continue
		}
		row, ok := build(id, 0)
		if ok {
			out.Rows = append(out.Rows, row)
		}
	}
	if len(out.Rows) > 1 {
		sortOutlineRows(out.Rows)
	}

	// Dangling: referenced IDs missing from the map (DET-08). Sorted for
	// determinism.
	for id := range referenced {
		if _, ok := wf.Nodes[id]; !ok {
			out.Dangling = append(out.Dangling, OutlineRow{
				NodeID: id, Name: id, DisplayName: id,
				Phase: "?", Message: "referenced but missing from node map",
			})
		}
	}
	sort.Slice(out.Dangling, func(i, j int) bool {
		return out.Dangling[i].NodeID < out.Dangling[j].NodeID
	})

	// Unreachable: present but never visited by the grouping walk. After
	// the full sweep above this can only contain nodes that are both
	// referenced and unvisitable — i.e. only reachable through a cycle we
	// cut. Enumerate deterministically.
	for _, id := range allIDs {
		if !visited[id] {
			n := wf.Nodes[id]
			row := outlineRowOf(n)
			row.Message = joinMessage(row.Message, "not attached to the outline (cycle or missing parent)")
			out.Unreachable = append(out.Unreachable, row)
		}
	}

	return out
}

// referencedByChildren reports whether parent lists id in its children.
func referencedByChildren(parent, id string, wf core.Workflow) bool {
	p, ok := wf.Nodes[parent]
	if !ok {
		return false
	}
	return containsNode(p.Children, id)
}

func containsNode(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// outlineRowOf maps a core.Node to an OutlineRow. HasPod follows the pinned
// node-type list; PodName is intentionally not surfaced here (DET-11: node
// ID is never a pod-name assumption).
func outlineRowOf(n core.Node) OutlineRow {
	name := n.Name
	if name == "" {
		name = n.ID
	}
	display := n.DisplayName
	if display == "" {
		display = name
	}
	msg := n.Message
	if msg == "" && n.Phase == "" {
		// Absent phase ⇒ "not yet started" (docs/protocol.md §2); keep the
		// phase empty for the view to label, but never invent a message.
		msg = ""
	}
	return OutlineRow{
		NodeID:      n.ID,
		Name:        name,
		DisplayName: display,
		Type:        n.Type,
		Phase:       n.Phase,
		Message:     msg,
		HasPod:      nodeTypeHasPod(n.Type),
		StartedAt:   n.StartedAt,
		FinishedAt:  n.FinishedAt,
	}
}

// nodeTypeHasPod encodes the pinned log-capable node types
// (docs/protocol.md §8): Pod, ContainerSet, HTTP, Plugin and container-set
// children. Everything else renders structurally.
func nodeTypeHasPod(t string) bool {
	switch t {
	case "Pod", "ContainerSet", "HTTP", "Plugin":
		return true
	default:
		return false
	}
}

// sortOutlineRows orders rows by (started-at, node ID) with the documented
// absent-timestamp rule: timestampless rows sort last, ties by node ID.
func sortOutlineRows(rows []OutlineRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch {
		case a.StartedAt != nil && b.StartedAt != nil:
			if !a.StartedAt.Equal(*b.StartedAt) {
				return a.StartedAt.Before(*b.StartedAt)
			}
			return a.NodeID < b.NodeID
		case a.StartedAt != nil:
			return true // timestamped first
		case b.StartedAt != nil:
			return false
		default:
			return a.NodeID < b.NodeID
		}
	})
}

func joinMessage(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
