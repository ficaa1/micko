package detail

import (
	"strings"
)

// outline_flat.go turns the nested outline into the flat, scrollable,
// cursor-addressable row list the nodes pane actually renders.
//
// The nested form is the truth about structure; a terminal pane needs a list.
// Flattening once per change keeps rendering and key handling working on the
// same indices, so the cursor can never point at a row the view did not
// draw.

// FlatRow is one rendered line of the nodes outline.
type FlatRow struct {
	Row OutlineRow
	// Depth is the nesting level, 0 for a workflow-level node.
	Depth int
	// Prefix is the tree drawing for this row ("│  ├─ "), already built from
	// the ancestors' last-child flags.
	Prefix string
	// Indent is everything the row draws left of its phase glyph: Prefix
	// with the fold marker set into the row's own branch ("│  ├▾ "), or,
	// for a top-level row, the marker and a space. A top-level row without
	// children gets two spaces, so every top-level glyph lines up.
	Indent string
	// Section marks rows that belong to an ungrouped section rather than the
	// tree ("dangling" or "unreachable"); empty for ordinary nodes.
	Section string
	// Parent is the index of this row's parent in the same list, and -1
	// for a top-level row, a section row, or any row of a filtered list.
	Parent int
	// HasChildren reports that the row has children to show, which makes
	// it foldable. Children hidden as skipped do not count.
	HasChildren bool
	// Folded reports that the row's children are folded away, and
	// FoldedCount how many rows the fold hides.
	Folded      bool
	FoldedCount int
}

// Fold markers. A row with children shows which way it is open; a leaf has
// none, so the marker alone tells a reader where there is more to see.
const (
	markerOpen   = "▾"
	markerFolded = "▸"
)

// Suspended reports whether this row is the manual gate a Resume clears.
func (r FlatRow) Suspended() bool {
	return r.Row.Type == "Suspend" && r.Row.Phase == "Running"
}

// Skipped reports whether the node did not run because a condition excluded
// it. These rows dominate a large workflow and say nothing about its progress.
func (r FlatRow) Skipped() bool {
	return rowSkipped(r.Row)
}

// FlattenOptions says what the flat list leaves out.
type FlattenOptions struct {
	// HideSkipped drops a skipped node and its subtree when nothing in that
	// subtree ran.
	HideSkipped bool
	// Folded holds the node IDs whose children are folded away.
	Folded map[string]bool
}

// Flattened is the flat row list plus what it left out, so the pane can say
// what it is not showing instead of quietly shortening the tree.
type Flattened struct {
	Rows []FlatRow
	// HiddenSkipped counts the rows HideSkipped removed, inside folds too.
	HiddenSkipped int
	// Folds counts the folded rows drawn; FoldedNodes the rows they hide.
	Folds       int
	FoldedNodes int
}

// Flatten walks the outline depth-first and returns one row per visible
// node. A skipped subtree is hidden only when nothing in it ran: a skipped
// node can still hold a descendant that ran, and hiding the parent would hide
// the running work with it. A folded row is drawn and its descendants are
// not.
//
// The walk is linear: whether a subtree is entirely skipped is computed once
// per node, bottom-up, before the walk reads it.
func Flatten(out Outline, opts FlattenOptions) Flattened {
	var f Flattened
	// allSkipped holds the nodes that are skipped along with every
	// descendant. Only such a subtree is safe to hide: it says nothing about
	// the workflow's progress, and nothing inside it ran.
	var allSkipped map[string]bool
	if opts.HideSkipped {
		allSkipped = map[string]bool{}
		var mark func(r OutlineRow) bool
		mark = func(r OutlineRow) bool {
			all := rowSkipped(r)
			for _, c := range r.Children {
				if !mark(c) {
					all = false
				}
			}
			if all {
				allSkipped[r.NodeID] = true
			}
			return all
		}
		for _, r := range out.Rows {
			mark(r)
		}
	}
	hidden := func(r OutlineRow) bool { return allSkipped[r.NodeID] }

	// folded counts a folded subtree's rows, splitting off the skipped ones
	// the flat list would hide anyway.
	var folded func(src []OutlineRow) int
	folded = func(src []OutlineRow) int {
		n := 0
		for _, r := range src {
			if hidden(r) {
				f.HiddenSkipped += 1 + countRows(r.Children)
				continue
			}
			n += 1 + folded(r.Children)
		}
		return n
	}

	var walk func(src []OutlineRow, depth int, prefix string, parent int)
	walk = func(src []OutlineRow, depth int, prefix string, parent int) {
		// The connector for a row depends on whether it is the last visible
		// sibling, so decide visibility for the whole sibling group first.
		visible := make([]OutlineRow, 0, len(src))
		for _, r := range src {
			if hidden(r) {
				f.HiddenSkipped += 1 + countRows(r.Children)
				continue
			}
			visible = append(visible, r)
		}
		for i, r := range visible {
			last := i == len(visible)-1
			branch := "├─ "
			if last {
				branch = "└─ "
			}
			self := prefix + branch
			if depth == 0 {
				self = ""
			}
			fr := FlatRow{Row: r, Depth: depth, Prefix: self, Parent: parent}
			for _, c := range r.Children {
				if !hidden(c) {
					fr.HasChildren = true
					break
				}
			}
			marker := ""
			if fr.HasChildren {
				marker = markerOpen
				if opts.Folded[r.NodeID] {
					fr.Folded = true
					marker = markerFolded
				}
			}
			fr.Indent = indentFor(self, marker, depth)
			idx := len(f.Rows)
			if fr.Folded {
				fr.FoldedCount = folded(r.Children)
				f.Folds++
				f.FoldedNodes += fr.FoldedCount
			}
			f.Rows = append(f.Rows, fr)
			if fr.Folded {
				continue
			}
			childPrefix := prefix
			if depth > 0 {
				if last {
					childPrefix += "   "
				} else {
					childPrefix += "│  "
				}
			}
			walk(r.Children, depth+1, childPrefix, idx)
		}
	}
	walk(out.Rows, 0, "", -1)

	for _, d := range out.Dangling {
		f.Rows = append(f.Rows, FlatRow{Row: d, Section: "dangling", Parent: -1})
	}
	for _, u := range out.Unreachable {
		f.Rows = append(f.Rows, FlatRow{Row: u, Section: "unreachable", Parent: -1})
	}
	return f
}

// indentFor sets the fold marker into a row's own branch: "├─ " becomes
// "├▾ ". A top-level row has no branch, so it gets a two-cell slot for the
// marker instead, and its children's connectors start under that marker.
func indentFor(prefix, marker string, depth int) string {
	if depth == 0 {
		if marker == "" {
			return "  "
		}
		return marker + " "
	}
	if marker == "" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "─ ") + marker + " "
}

// FlattenOutline is Flatten with no folds: every visible row, and how many
// rows hideSkipped removed.
func FlattenOutline(out Outline, hideSkipped bool) (rows []FlatRow, hidden int) {
	f := Flatten(out, FlattenOptions{HideSkipped: hideSkipped})
	return f.Rows, f.HiddenSkipped
}

func rowSkipped(r OutlineRow) bool {
	return r.Phase == "Skipped" || r.Type == "Skipped"
}

func countRows(rows []OutlineRow) int {
	n := 0
	for _, r := range rows {
		n += 1 + countRows(r.Children)
	}
	return n
}

// NodePhase is the nodes tab's phase bucket. A large workflow hides its one
// failed step among forty successful ones, so the tab can narrow to a single
// phase.
type NodePhase string

const (
	NodePhaseAll       NodePhase = "All"
	NodePhaseFailed    NodePhase = "Failed"
	NodePhaseRunning   NodePhase = "Running"
	NodePhasePending   NodePhase = "Pending"
	NodePhaseSucceeded NodePhase = "Succeeded"
	NodePhaseSkipped   NodePhase = "Skipped"
)

// nodePhaseCycle is the rotation order of the p key. Failed comes first
// because it is the phase a reader opens the tab to find.
var nodePhaseCycle = []NodePhase{
	NodePhaseAll, NodePhaseFailed, NodePhaseRunning, NodePhasePending,
	NodePhaseSucceeded, NodePhaseSkipped,
}

// Next advances the bucket.
func (f NodePhase) Next() NodePhase {
	for i, p := range nodePhaseCycle {
		if p == f {
			return nodePhaseCycle[(i+1)%len(nodePhaseCycle)]
		}
	}
	return NodePhaseAll
}

// Matches reports whether a row belongs to this bucket. Failed also collects
// Error and Omitted: all three mean the step did not deliver, and a reader
// hunting a failure needs every one of them in the same list.
func (f NodePhase) Matches(r FlatRow) bool {
	phase := r.Row.Phase
	switch f {
	case NodePhaseAll:
		return true
	case NodePhaseFailed:
		return phase == "Failed" || phase == "Error" || phase == "Omitted"
	case NodePhaseSkipped:
		return r.Skipped()
	default:
		return phase == string(f)
	}
}

// FilterFlatRows keeps the rows of one phase and drops the tree drawing.
//
// The tree is a statement about structure. Once a filter removes the parents,
// a connector would draw a branch to a row that is not on screen, so the
// filtered list is flat and says so by its own shape. Folds belong to the
// tree too, so a filtered row carries no marker.
func FilterFlatRows(rows []FlatRow, f NodePhase) []FlatRow {
	if f == NodePhaseAll {
		return rows
	}
	out := make([]FlatRow, 0, len(rows))
	for _, r := range rows {
		if !f.Matches(r) {
			continue
		}
		r.Prefix, r.Indent, r.Depth, r.Parent = "", "", 0, -1
		r.HasChildren, r.Folded, r.FoldedCount = false, false, 0
		out = append(out, r)
	}
	return out
}
