package detail

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// cellWidth measures a string in terminal cells, so a wide rune in a node
// name cannot shift the type and phase columns out of line.
func cellWidth(s string) int { return ansi.StringWidth(s) }

// outline_flat.go turns the nested outline into the flat, scrollable,
// cursor-addressable row list the nodes pane actually renders.
//
// The nested form is the truth about structure; a terminal pane needs a list.
// Flattening once per workflow keeps rendering and key handling working on
// the same indices, so the cursor can never point at a row the view did not
// draw.

// FlatRow is one rendered line of the nodes outline.
type FlatRow struct {
	Row OutlineRow
	// Depth is the nesting level, 0 for a workflow-level node.
	Depth int
	// Prefix is the tree drawing for this row ("│  ├─ "), already built from
	// the ancestors' last-child flags.
	Prefix string
	// Section marks rows that belong to an ungrouped section rather than the
	// tree ("dangling" or "unreachable"); empty for ordinary nodes.
	Section string
}

// Suspended reports whether this row is the manual gate a Resume clears.
func (r FlatRow) Suspended() bool {
	return r.Row.Type == "Suspend" && r.Row.Phase == "Running"
}

// Skipped reports whether the node did not run because a condition excluded
// it. These rows dominate a large workflow and say nothing about its progress.
func (r FlatRow) Skipped() bool {
	return r.Row.Phase == "Skipped" || r.Row.Type == "Skipped"
}

// FlattenOutline walks the outline depth-first and returns one row per
// visible node. hideSkipped drops a skipped node and its subtree only when
// nothing in that subtree ran. A skipped node can still hold a descendant
// that ran, and hiding the parent would hide the running work with it.
//
// The returned count is the number of rows hideSkipped removed, so the pane
// can say what it is not showing instead of quietly shortening the tree.
func FlattenOutline(out Outline, hideSkipped bool) (rows []FlatRow, hidden int) {
	var walk func(src []OutlineRow, depth int, prefix string)
	walk = func(src []OutlineRow, depth int, prefix string) {
		// The connector for a row depends on whether it is the last visible
		// sibling, so decide visibility for the whole sibling group first.
		visible := make([]OutlineRow, 0, len(src))
		for _, r := range src {
			if hideSkipped && subtreeAllSkipped(r) {
				hidden += 1 + countRows(r.Children)
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
			rows = append(rows, FlatRow{Row: r, Depth: depth, Prefix: self})
			childPrefix := prefix
			if depth > 0 {
				if last {
					childPrefix += "   "
				} else {
					childPrefix += "│  "
				}
			}
			walk(r.Children, depth+1, childPrefix)
		}
	}
	walk(out.Rows, 0, "")

	for _, d := range out.Dangling {
		rows = append(rows, FlatRow{Row: d, Section: "dangling"})
	}
	for _, u := range out.Unreachable {
		rows = append(rows, FlatRow{Row: u, Section: "unreachable"})
	}
	return rows, hidden
}

func rowSkipped(r OutlineRow) bool {
	return r.Phase == "Skipped" || r.Type == "Skipped"
}

// subtreeAllSkipped reports whether r and every descendant is skipped. Only
// such a subtree is safe to hide: it says nothing about the workflow's
// progress, and nothing inside it ran.
func subtreeAllSkipped(r OutlineRow) bool {
	if !rowSkipped(r) {
		return false
	}
	for _, c := range r.Children {
		if !subtreeAllSkipped(c) {
			return false
		}
	}
	return true
}

func countRows(rows []OutlineRow) int {
	n := 0
	for _, r := range rows {
		n += 1 + countRows(r.Children)
	}
	return n
}

// RenderFlatRow draws one node line: tree prefix, phase glyph, name, then the
// type and phase words in fixed columns.
//
// Text carries every fact. The glyph and the colour repeat the phase, they
// never replace the word, so a mono terminal loses nothing (UI-03/07).
func RenderFlatRow(r FlatRow, width int, theme shared.Theme, selected bool) string {
	phase := rowPhase(r.Row)
	glyph := shared.PhaseSymbol(phase)
	if r.Suspended() {
		glyph = shared.PhaseSymbol("Suspended")
	}
	name := shared.Sanitize(rowDisplayName(r.Row))

	left := r.Prefix + glyph + " " + name
	if r.Section != "" {
		left = "! " + name
	}

	// The type and phase columns are what a reader scans down, so they keep
	// fixed widths even when the name has to be cut to make room.
	const typeW, phaseW = 11, 10
	right := padCell(shared.Sanitize(rowType(r.Row)), typeW) + " " + padCell(shared.Sanitize(phase), phaseW)
	if r.Suspended() {
		right += " " + "AWAITING RESUME"
	} else if msg := shared.Sanitize(r.Row.Message); msg != "" && !r.Skipped() {
		right += " " + msg
	} else if r.Section != "" {
		right += " " + shared.Sanitize(r.Row.Message)
	}

	line := left
	if width > 0 {
		nameRoom := width - cellWidth(right) - 2
		if nameRoom < 12 {
			nameRoom = 12
		}
		line = padCell(truncCell(left, nameRoom), nameRoom) + "  " + right
	} else {
		line = left + "  " + right
	}

	switch {
	case selected:
		return theme.Selected.Render(line)
	case r.Suspended():
		return theme.Warning.Render(line)
	case r.Skipped():
		return theme.Dim.Render(line)
	default:
		return theme.PhaseStyle(phase).Render(line)
	}
}

// padCell pads s with spaces to exactly n display cells.
func padCell(s string, n int) string {
	if w := cellWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// truncCell clips s to n display cells, marking the cut.
func truncCell(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cellWidth(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := cellWidth(string(r))
		if w+rw > n-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

var _ = core.Node{}

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
// filtered list is flat and says so by its own shape.
func FilterFlatRows(rows []FlatRow, f NodePhase) []FlatRow {
	if f == NodePhaseAll {
		return rows
	}
	out := make([]FlatRow, 0, len(rows))
	for _, r := range rows {
		if !f.Matches(r) {
			continue
		}
		r.Prefix, r.Depth = "", 0
		out = append(out, r)
	}
	return out
}
