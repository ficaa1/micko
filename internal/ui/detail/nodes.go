// Package detail implements the workflow detail view: summary, the node tree
// (no geometric DAG renderer) and the redacted resource viewer.
//
// The node tree is drawn the way a reader thinks about a pipeline, not the
// way the controller records it. Rules baked into this package:
//   - children lists are the controller's bookkeeping: they chain a steps
//     template's groups and point each DAG task at its dependents. They are
//     not the tree. A node is drawn under the node that owns it, which is
//     its Retry node for a retry attempt, its TaskGroup for a loop item,
//     its container set for a container, and its boundary otherwise.
//     outboundNodes are never tree edges.
//   - StepGroup nodes are not drawn. Their steps are drawn under the Steps
//     node in group order, the way `argo get` shows them.
//   - Every other node in the map appears exactly once in the tree or in an
//     explicit ungrouped section; cycles are cut; traversal is
//     linear apart from sorting, never exponential.
//   - Node IDs are never assumed to be pod names.
package detail

import (
	"container/heap"
	"sort"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
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
	// (docs/development.md: Pod/ContainerSet/HTTP/Plugin plus container-set
	// children). It does NOT imply a known pod name.
	HasPod bool
	// PodName is the pod backing this node. It is filled in only when the
	// server stated its pod-naming scheme on the workflow; empty means
	// "unknown", and the pane then offers no node-scoped logs rather than
	// guessing a pod that may belong to another workflow.
	PodName string
	// StartedAt/FinishedAt pass through for the selected-node pane; nil
	// means "not yet started" (an absent phase or timestamp is meaningful,
	// never rendered as empty success).
	StartedAt  *time.Time
	FinishedAt *time.Time
	// Template is the template the node ran: its own template name, or the
	// template named through a templateRef.
	Template string
	// ExitCode is the main container's exit code as the server sent it.
	ExitCode string
	// EstimatedDuration is the controller's estimate for this node.
	EstimatedDuration time.Duration
	// Deps are the display names of the DAG tasks this task waits for, in
	// tree order. The tree draws a task once, under its DAG, so a join that
	// waits for six tasks is one row that names them rather than six copies.
	Deps []string
	// After holds the IDs of the siblings this node waited for before it
	// could start: the tasks a DAG task depends on, every step of the step
	// group before a step's own, or the attempt before a retry attempt. A
	// node with none started when its parent did. The timeline reads it for
	// the time a node spent waiting and for the critical path.
	After []string
	// Retries is how many times a Retry node ran its template again after
	// the first attempt.
	Retries int
	// Role names what a top-level tree other than the workflow's own is for:
	// "exit handler" for the onExit tree, "hook" for another lifecycle hook.
	Role string
	// Seq is the row's position among its siblings in pipeline order: group
	// order under a Steps node, dependency order under a DAG, attempt order
	// under a Retry. The pipeline sort restores it after another order.
	Seq      int
	Children []OutlineRow
}

// Outline is the result of the deterministic outline build.
type Outline struct {
	Rows []OutlineRow
	// Available mirrors Workflow.NodesAvailable. false means the node map
	// is unusable (offloaded/unhydrated) and Rows is empty — the view
	// renders the explicit unavailable state with UnavailableReason.
	Available         bool
	UnavailableReason string
	// Dangling lists node IDs referenced by children/boundary grouping but
	// missing from the node map (explicit ungrouped section).
	Dangling []OutlineRow
	// Unreachable lists nodes present in the map that the tree could not
	// place (never silently dropped).
	Unreachable []OutlineRow
	// StepGroups is how many StepGroup nodes the tree drew as their steps
	// instead of as rows. They are the only nodes absent from both the tree
	// and the ungrouped sections.
	StepGroups int
}

// OutlineOptions tunes the outline build. The zero value is the default
// deterministic outline.
type OutlineOptions struct {
	// Noop placeholder for future view-level toggles; the deterministic
	// build itself takes no options today.
	_ bool
}

// BuildNodeOutline builds the display tree from the workflow's node map.
//
// Order is deterministic. Under a Steps node the steps come in group order
// and, inside a group, in the order the template lists them. Under a DAG the
// tasks come in dependency order, and tasks that are free at the same point
// order by start time. Under a Retry, TaskGroup or container set the members
// keep the order the controller created them in. Everything else, top-level
// trees included, orders by start time. Nodes without a start time come
// after timestamped ones, and remaining ties break by display name (numbers
// compared as numbers, so shard(10) follows shard(9)) and then by node ID.
//
// Traversal is linear: every node is visited at most once, so even
// adversarial cyclic references terminate in O(n+e) plus the sorting, with
// no exponential expansion.
func BuildNodeOutline(wf core.Workflow, _ OutlineOptions) Outline {
	if !wf.NodesAvailable {
		return Outline{
			Available:         false,
			UnavailableReason: wf.NodesUnavailableReason,
		}
	}
	b := newTreeBuilder(wf)
	return b.build()
}

// treeBuilder holds the indexes the display tree is built from. Every map is
// keyed by node ID.
type treeBuilder struct {
	nodes  map[string]core.Node
	wfName string
	// ids is every node ID, sorted, so each scan below is deterministic.
	ids []string
	// hidden marks the StepGroups drawn as their steps.
	hidden map[string]bool
	// owner is a node's display parent; a node without one is a root.
	owner map[string]string
	// owned lists each node's display children in ID order; order() sorts
	// them for display.
	owned map[string][]string
	// listPos is a member's position in the list that orders it: the
	// steps of every group of a Steps node, counted across the groups in
	// group order, or a Retry, TaskGroup or container set's children list.
	listPos map[string]int
	// ordered marks parents whose children keep listPos order.
	ordered map[string]bool
	// stepGroup is a step's group ordinal under its Steps node: 0 for the
	// first group drawn, 1 for the next, and so on.
	stepGroup map[string]int
	// deps is each DAG task's dependency tasks, deduplicated.
	deps    map[string][]string
	depSeen map[[2]string]bool
	// referenced is every ID some children list names.
	referenced map[string]bool
	visited    map[string]bool
	// entries are the nodes the tree starts from: the roots, then one node
	// of each ownership cycle, which is cut there.
	entries []string
}

func newTreeBuilder(wf core.Workflow) *treeBuilder {
	n := len(wf.Nodes)
	b := &treeBuilder{
		nodes:      wf.Nodes,
		wfName:     wf.Summary.Ref.Name,
		ids:        make([]string, 0, n),
		hidden:     map[string]bool{},
		owner:      make(map[string]string, n),
		owned:      make(map[string][]string, n),
		listPos:    map[string]int{},
		ordered:    map[string]bool{},
		stepGroup:  map[string]int{},
		deps:       map[string][]string{},
		depSeen:    map[[2]string]bool{},
		referenced: make(map[string]bool, n),
		visited:    make(map[string]bool, n),
	}
	for id := range wf.Nodes {
		b.ids = append(b.ids, id)
	}
	sort.Strings(b.ids)
	return b
}

func (b *treeBuilder) build() Outline {
	b.indexGroups()
	b.assignOwners()
	b.orderSteps()
	b.findEntries()

	out := Outline{Available: true, StepGroups: len(b.hidden)}
	for k := range b.visited {
		delete(b.visited, k)
	}
	out.Rows = make([]OutlineRow, 0, len(b.entries))
	for _, id := range b.entries {
		if b.visited[id] {
			continue
		}
		out.Rows = append(out.Rows, b.row(id))
	}
	b.orderRoots(out.Rows)

	// Dangling: referenced IDs missing from the map, sorted.
	for id := range b.referenced {
		if _, ok := b.nodes[id]; !ok {
			out.Dangling = append(out.Dangling, OutlineRow{
				NodeID: id, Name: id, DisplayName: id,
				Phase: "?", Message: "referenced but missing from node map",
			})
		}
	}
	sort.Slice(out.Dangling, func(i, j int) bool {
		return out.Dangling[i].NodeID < out.Dangling[j].NodeID
	})

	// Unreachable: present but placed nowhere. The entry sweep starts a
	// tree at every unvisited node, so this stays empty unless that rule
	// changes; it is the section that keeps such a change from dropping
	// nodes silently.
	for _, id := range b.ids {
		if !b.visited[id] && !b.hidden[id] {
			row := outlineRowOf(b.nodes[id])
			row.Message = joinMessage(row.Message, "not attached to the outline (cycle or missing parent)")
			out.Unreachable = append(out.Unreachable, row)
		}
	}
	return out
}

// indexGroups records which nodes a Retry, TaskGroup or container set owns,
// which StepGroups are drawn as their steps, and every referenced ID.
func (b *treeBuilder) indexGroups() {
	for _, id := range b.ids {
		n := b.nodes[id]
		if n.Type == "StepGroup" && n.BoundaryID != "" && n.BoundaryID != id {
			if _, ok := b.nodes[n.BoundaryID]; ok {
				b.hidden[id] = true
			}
		}
	}
	for _, id := range b.ids {
		n := b.nodes[id]
		for i, c := range n.Children {
			b.referenced[c] = true
			if c == id || b.hidden[c] {
				continue
			}
			child, ok := b.nodes[c]
			if !ok || b.owner[c] != "" {
				continue
			}
			if isMember(n, child) {
				b.owner[c] = id
				b.listPos[c] = i
				b.ordered[id] = true
			}
		}
	}
}

// isMember reports whether child belongs to parent itself rather than being
// a dependent the controller chained onto it. A Retry node's children list
// holds its attempts, but in a DAG it can also hold the tasks that wait for
// it. Attempts carry the retried flag, and on servers that predate the flag
// they are still named after the Retry node with the attempt number in
// brackets; loop items are named the same way after their TaskGroup.
func isMember(parent, child core.Node) bool {
	switch {
	case child.Type == "Container":
		return true
	case parent.Type == "Retry":
		return child.Retried || strings.HasPrefix(child.Name, parent.Name+"(")
	case parent.Type == "TaskGroup":
		return strings.HasPrefix(child.Name, parent.Name+"(")
	default:
		return false
	}
}

// assignOwners gives every node not claimed by a group its display parent:
// its boundary when that exists, else the first node (by ID) that lists it
// as a child, else none, which makes it a root. A parent that is a hidden
// StepGroup stands for its Steps node.
func (b *treeBuilder) assignOwners() {
	firstParent := make(map[string]string, len(b.nodes))
	for _, id := range b.ids {
		for _, c := range b.nodes[id].Children {
			if c == id {
				continue
			}
			if _, ok := firstParent[c]; !ok {
				firstParent[c] = id
			}
		}
	}
	for _, id := range b.ids {
		if b.hidden[id] || b.owner[id] != "" {
			continue
		}
		n := b.nodes[id]
		var p string
		if n.BoundaryID != "" && n.BoundaryID != id {
			if _, ok := b.nodes[n.BoundaryID]; ok {
				p = n.BoundaryID
			}
		}
		if p == "" {
			p = firstParent[id]
		}
		if b.hidden[p] {
			p = b.nodes[p].BoundaryID
		}
		if p == "" || p == id {
			delete(b.owner, id)
			continue
		}
		b.owner[id] = p
	}
	for _, id := range b.ids {
		if p, ok := b.owner[id]; ok && p != "" {
			b.owned[p] = append(b.owned[p], id)
		} else {
			delete(b.owner, id)
		}
	}
}

// orderSteps numbers the steps of every Steps node across its groups, in
// group order. A group's index is the number in its display name ("[2]");
// groups whose name carries none order by start time.
func (b *treeBuilder) orderSteps() {
	groups := map[string][]string{}
	for _, id := range b.ids {
		if b.hidden[id] {
			s := b.nodes[id].BoundaryID
			groups[s] = append(groups[s], id)
		}
	}
	for s, gs := range groups {
		sort.SliceStable(gs, func(i, j int) bool {
			a, c := b.nodes[gs[i]], b.nodes[gs[j]]
			ai, aok := groupIndex(a)
			ci, cok := groupIndex(c)
			if aok && cok && ai != ci {
				return ai < ci
			}
			if aok != cok {
				return aok
			}
			return b.lessByStart(gs[i], gs[j])
		})
		pos := 0
		for gi, g := range gs {
			for _, c := range b.nodes[g].Children {
				if b.owner[c] != s {
					continue
				}
				if _, done := b.listPos[c]; done {
					continue
				}
				b.listPos[c] = pos
				b.stepGroup[c] = gi
				pos++
			}
		}
		b.ordered[s] = true
	}
}

// groupIndex reads the group number from a StepGroup's name: "[3]" in its
// display name, or at the end of its full name.
func groupIndex(n core.Node) (int, bool) {
	for _, s := range []string{n.DisplayName, n.Name} {
		if !strings.HasSuffix(s, "]") {
			continue
		}
		open := strings.LastIndexByte(s, '[')
		if open < 0 {
			continue
		}
		digits := s[open+1 : len(s)-1]
		if digits == "" {
			continue
		}
		v := 0
		ok := true
		for _, r := range digits {
			if r < '0' || r > '9' || v > 1<<20 {
				ok = false
				break
			}
			v = v*10 + int(r-'0')
		}
		if ok {
			return v, true
		}
	}
	return 0, false
}

// findEntries walks the ownership forest once from its roots, then from one
// node of every ownership cycle, and records the dependencies of DAG tasks
// on the way.
//
// A DAG task lists the tasks that wait for it as children, from its own
// node or from the nodes that finish it: the last attempt of a retry, the
// leaves of a nested template. So an edge x → c means "c waits for the task
// of c's DAG that contains x". The walk keeps the path from the root on a
// stack, which makes that task the stack entry just above the DAG: one
// lookup per edge, whatever the nesting.
func (b *treeBuilder) findEntries() {
	var stack []string
	pos := map[string]int{}
	var visit func(id string)
	visit = func(id string) {
		b.visited[id] = true
		pos[id] = len(stack)
		stack = append(stack, id)
		for _, c := range b.nodes[id].Children {
			d, ok := b.owner[c]
			if !ok || c == id || b.nodes[d].Type != "DAG" {
				continue
			}
			p, onPath := pos[d]
			if !onPath || p+1 >= len(stack) {
				continue
			}
			if task := stack[p+1]; task != c {
				b.addDep(c, task)
			}
		}
		for _, k := range b.owned[id] {
			if !b.visited[k] {
				visit(k)
			}
		}
		stack = stack[:len(stack)-1]
		delete(pos, id)
	}
	for _, id := range b.ids {
		if _, has := b.owner[id]; !has && !b.hidden[id] {
			b.entries = append(b.entries, id)
			visit(id)
		}
	}
	// Whatever is still unvisited sits on an ownership cycle or hangs off
	// one. Start from boundary-less nodes first, so the cut lands on the
	// back edge rather than on the edge from the real root.
	var rest []string
	for _, id := range b.ids {
		if !b.visited[id] && !b.hidden[id] {
			rest = append(rest, id)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		bi, bj := b.nodes[rest[i]].BoundaryID == "", b.nodes[rest[j]].BoundaryID == ""
		if bi != bj {
			return bi
		}
		return rest[i] < rest[j]
	})
	for _, id := range rest {
		if !b.visited[id] {
			b.entries = append(b.entries, id)
			visit(id)
		}
	}
}

func (b *treeBuilder) addDep(task, dep string) {
	k := [2]string{task, dep}
	if b.depSeen[k] {
		return
	}
	b.depSeen[k] = true
	b.deps[task] = append(b.deps[task], dep)
}

// row builds the display row for id and, recursively, its subtree. A child
// already placed is skipped, which is where an ownership cycle is cut.
func (b *treeBuilder) row(id string) OutlineRow {
	b.visited[id] = true
	n := b.nodes[id]
	r := outlineRowOf(n)
	kids := make([]string, 0, len(b.owned[id]))
	for _, k := range b.owned[id] {
		if !b.visited[k] {
			kids = append(kids, k)
		}
	}
	if len(kids) == 0 {
		return r
	}
	kids = b.order(id, kids)
	seq := make(map[string]int, len(kids))
	for i, k := range kids {
		seq[k] = i
	}
	r.Children = make([]OutlineRow, 0, len(kids))
	members := 0
	prevGroup := b.previousStepGroups(id, kids)
	lastMember := ""
	for i, k := range kids {
		if b.visited[k] {
			continue
		}
		child := b.row(k)
		child.Seq = i
		if ds := b.deps[k]; len(ds) > 0 {
			ordered := make([]string, 0, len(ds))
			for _, d := range ds {
				if _, ok := seq[d]; ok {
					ordered = append(ordered, d)
				}
			}
			sort.Slice(ordered, func(x, y int) bool { return seq[ordered[x]] < seq[ordered[y]] })
			for _, d := range ordered {
				child.Deps = append(child.Deps, nodeDisplay(b.nodes[d]))
			}
			child.After = ordered
		}
		if prev, ok := prevGroup[k]; ok {
			child.After = prev
		}
		_, member := b.listPos[k]
		member = member && b.nodes[k].Type != "Container"
		if member {
			members++
			if n.Type == "Retry" && lastMember != "" {
				child.After = []string{lastMember}
			}
			lastMember = k
		}
		r.Children = append(r.Children, child)
	}
	if n.Type == "Retry" && members > 1 {
		r.Retries = members - 1
	}
	return r
}

// previousStepGroups maps each step of a Steps node to the steps of the
// group drawn before its own, which it waited for. Steps of the first group
// waited for nothing and are absent. kids is the Steps node's display
// children; anything else returns nil.
func (b *treeBuilder) previousStepGroups(id string, kids []string) map[string][]string {
	if b.nodes[id].Type != "Steps" {
		return nil
	}
	byGroup := map[int][]string{}
	var groups []int
	for _, k := range kids {
		g, ok := b.stepGroup[k]
		if !ok || b.owner[k] != id {
			continue
		}
		if _, seen := byGroup[g]; !seen {
			groups = append(groups, g)
		}
		byGroup[g] = append(byGroup[g], k)
	}
	sort.Ints(groups)
	out := map[string][]string{}
	for i := 1; i < len(groups); i++ {
		for _, k := range byGroup[groups[i]] {
			out[k] = byGroup[groups[i-1]]
		}
	}
	return out
}

// order sorts one parent's display children. See BuildNodeOutline for the
// rules.
func (b *treeBuilder) order(parent string, kids []string) []string {
	if b.ordered[parent] {
		sort.SliceStable(kids, func(i, j int) bool {
			pi, iok := b.listPos[kids[i]]
			pj, jok := b.listPos[kids[j]]
			switch {
			case iok && jok:
				if pi != pj {
					return pi < pj
				}
			case iok != jok:
				return iok
			}
			return b.lessByStart(kids[i], kids[j])
		})
		return kids
	}
	sort.SliceStable(kids, func(i, j int) bool { return b.lessByStart(kids[i], kids[j]) })
	if b.nodes[parent].Type != "DAG" {
		return kids
	}
	return b.topo(kids)
}

// topo orders a DAG's tasks so every task follows the tasks it waits for.
// kids arrives sorted by start time, and among the tasks that are free to go
// next the earliest in that order goes first. A dependency cycle cannot
// happen in a valid DAG; if the data holds one anyway, the earliest waiting
// task is placed next and the walk carries on, so no task is lost.
func (b *treeBuilder) topo(kids []string) []string {
	rank := make(map[string]int, len(kids))
	for i, k := range kids {
		rank[k] = i
	}
	indeg := make([]int, len(kids))
	succ := make([][]int, len(kids))
	for i, k := range kids {
		for _, d := range b.deps[k] {
			if j, ok := rank[d]; ok && j != i {
				indeg[i]++
				succ[j] = append(succ[j], i)
			}
		}
	}
	ready := &intHeap{}
	for i := range kids {
		if indeg[i] == 0 {
			heap.Push(ready, i)
		}
	}
	out := make([]string, 0, len(kids))
	done := make([]bool, len(kids))
	next := 0
	for len(out) < len(kids) {
		if ready.Len() == 0 {
			for done[next] {
				next++
			}
			indeg[next] = 0
			heap.Push(ready, next)
		}
		i := heap.Pop(ready).(int)
		if done[i] {
			continue
		}
		done[i] = true
		out = append(out, kids[i])
		for _, s := range succ[i] {
			indeg[s]--
			if indeg[s] == 0 && !done[s] {
				heap.Push(ready, s)
			}
		}
	}
	return out
}

type intHeap []int

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *intHeap) Push(x any)        { *h = append(*h, x.(int)) }
func (h *intHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// lessByStart is the default order: start time (untimed last), then display
// name in natural order, then node ID.
func (b *treeBuilder) lessByStart(i, j string) bool {
	a, c := b.nodes[i], b.nodes[j]
	switch {
	case a.StartedAt != nil && c.StartedAt != nil:
		if !a.StartedAt.Equal(*c.StartedAt) {
			return a.StartedAt.Before(*c.StartedAt)
		}
	case a.StartedAt != nil:
		return true
	case c.StartedAt != nil:
		return false
	}
	if n := naturalCompare(nodeDisplay(a), nodeDisplay(c)); n != 0 {
		return n < 0
	}
	return i < j
}

// orderRoots sorts the top-level trees: the workflow's own tree first, then
// the exit handler and other hooks, then anything else, each by start time.
func (b *treeBuilder) orderRoots(rows []OutlineRow) {
	rank := func(r OutlineRow) int {
		switch {
		case r.Name == b.wfName || r.NodeID == b.wfName:
			return 0
		case r.Role != "":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rank(rows[i]), rank(rows[j])
		if ri != rj {
			return ri < rj
		}
		return b.lessByStart(rows[i].NodeID, rows[j].NodeID)
	})
	for i := range rows {
		rows[i].Seq = i
	}
}

func nodeDisplay(n core.Node) string {
	switch {
	case n.DisplayName != "":
		return n.DisplayName
	case n.Name != "":
		return n.Name
	default:
		return n.ID
	}
}

// naturalCompare compares two names case-insensitively, reading runs of
// digits as numbers, so "shard(2)" sorts before "shard(10)".
func naturalCompare(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(a)-i < len(b)-j:
		return -1
	case len(a)-i > len(b)-j:
		return 1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// outlineRowOf maps a core.Node to an OutlineRow. HasPod follows the pinned
// node-type list. PodName is carried through exactly as the adapter resolved
// it: empty unless the server declared the workflow's pod-name format, so a
// node ID is still never treated as a pod name.
func outlineRowOf(n core.Node) OutlineRow {
	name := n.Name
	if name == "" {
		name = n.ID
	}
	display := n.DisplayName
	if display == "" {
		display = name
	}
	template := n.TemplateName
	if template == "" {
		template = n.TemplateRefTemplate
	}
	return OutlineRow{
		NodeID:            n.ID,
		Name:              name,
		DisplayName:       display,
		Type:              n.Type,
		Phase:             n.Phase,
		Message:           n.Message,
		HasPod:            nodeTypeHasPod(n.Type),
		PodName:           n.PodName,
		StartedAt:         n.StartedAt,
		FinishedAt:        n.FinishedAt,
		Template:          template,
		ExitCode:          n.ExitCode,
		EstimatedDuration: n.EstimatedDuration,
		Role:              nodeRole(n),
	}
}

// nodeRole names the purpose of a tree the workflow runs beside its own:
// the exit handler, which Argo names "<workflow>.onExit", or another
// lifecycle hook. Only a top-level row shows it.
func nodeRole(n core.Node) string {
	switch {
	case strings.HasSuffix(n.Name, ".onExit"):
		return "exit handler"
	case n.Hooked && n.BoundaryID == "":
		return "hook"
	default:
		return ""
	}
}

// nodeTypeHasPod encodes the pinned log-capable node types
// (docs/development.md): Pod, ContainerSet, HTTP, Plugin and container-set
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
