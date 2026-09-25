package detail

import (
	"sort"
	"strings"
)

// NodeSort selects the sibling order in the nodes tree. It reorders siblings
// inside each parent and never changes the tree structure.
type NodeSort string

const (
	// NodeSortPipeline is the order the tree is built in: steps in group
	// order, DAG tasks after the tasks they wait for, retry attempts in
	// the order they ran. It is the default, because it reads the way the
	// workflow runs.
	NodeSortPipeline NodeSort = "pipeline"
	// NodeSortStarted orders by start time, then node ID. Nodes without a
	// start time come last.
	NodeSortStarted NodeSort = "started"
	// NodeSortName orders by display name, case-insensitively.
	NodeSortName NodeSort = "name"
	// NodeSortPhase orders by phase rank, then display name.
	NodeSortPhase NodeSort = "phase"
)

// Next returns the next order in the s rotation.
func (k NodeSort) Next() NodeSort {
	switch k {
	case NodeSortPipeline:
		return NodeSortStarted
	case NodeSortStarted:
		return NodeSortName
	case NodeSortName:
		return NodeSortPhase
	default:
		return NodeSortPipeline
	}
}

// nodePhaseRank ranks phases so the rows a reader triages come first.
// Unknown phases rank last.
func nodePhaseRank(p string) int {
	switch p {
	case "Failed", "Error":
		return 0
	case "Running":
		return 1
	case "Pending":
		return 2
	case "Succeeded":
		return 3
	case "Skipped", "Omitted":
		return 4
	default:
		return 5
	}
}

// SortOutline reorders every sibling group in the outline by key. It sorts
// in place, and each order is total, so a later call with another key gives
// the same result as sorting the original. The pipeline order is total
// because the build numbers every sibling group from zero.
func SortOutline(out *Outline, key NodeSort) {
	var walk func(rows []OutlineRow)
	walk = func(rows []OutlineRow) {
		sortOutlineRowsBy(rows, key)
		for i := range rows {
			walk(rows[i].Children)
		}
	}
	walk(out.Rows)
}

// sortOutlineRowsBy orders one sibling group. Every comparator falls back to
// the node ID, or is the build's own numbering, so the result never depends
// on the input order.
func sortOutlineRowsBy(rows []OutlineRow, key NodeSort) {
	switch key {
	case NodeSortName:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			an, bn := strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)
			if an != bn {
				return an < bn
			}
			return a.NodeID < b.NodeID
		})
	case NodeSortPhase:
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			ar, br := nodePhaseRank(a.Phase), nodePhaseRank(b.Phase)
			if ar != br {
				return ar < br
			}
			an, bn := strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)
			if an != bn {
				return an < bn
			}
			return a.NodeID < b.NodeID
		})
	case NodeSortStarted:
		sortOutlineRows(rows)
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	}
}
