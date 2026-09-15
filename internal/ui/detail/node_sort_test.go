package detail

import (
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// sortWorkflow gives one parent four children whose name, phase and start
// order all disagree, so each sort key produces a different sequence.
func sortWorkflow() core.Workflow {
	base := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	at := func(min int) *time.Time {
		t := base.Add(time.Duration(min) * time.Minute)
		return &t
	}
	nodes := map[string]core.Node{
		"root": {ID: "root", Name: "deploy", DisplayName: "deploy", Type: "Steps",
			Phase: "Running", Children: []string{"c-zulu", "c-alpha", "c-mike", "c-bravo"}},
		"c-zulu": {ID: "c-zulu", Name: "zulu", DisplayName: "zulu", Type: "Pod",
			Phase: "Succeeded", BoundaryID: "root", StartedAt: at(1)},
		"c-alpha": {ID: "c-alpha", Name: "alpha", DisplayName: "alpha", Type: "Pod",
			Phase: "Running", BoundaryID: "root", StartedAt: at(2)},
		"c-mike": {ID: "c-mike", Name: "mike", DisplayName: "mike", Type: "Pod",
			Phase: "Failed", BoundaryID: "root", StartedAt: at(3)},
		"c-bravo": {ID: "c-bravo", Name: "bravo", DisplayName: "bravo", Type: "Pod",
			Phase: "Pending", BoundaryID: "root", StartedAt: at(4)},
	}
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: "deploy", UID: "u1"}, Phase: "Running"},
		Nodes:          nodes,
		NodesAvailable: true,
	}
}

func childNames(out Outline) []string {
	if len(out.Rows) == 0 {
		return nil
	}
	names := make([]string, 0, len(out.Rows[0].Children))
	for _, c := range out.Rows[0].Children {
		names = append(names, c.DisplayName)
	}
	return names
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSortOutlineOrdersSiblings(t *testing.T) {
	cases := []struct {
		key  NodeSort
		want []string
	}{
		{NodeSortStarted, []string{"zulu", "alpha", "mike", "bravo"}},
		{NodeSortName, []string{"alpha", "bravo", "mike", "zulu"}},
		{NodeSortPhase, []string{"mike", "alpha", "bravo", "zulu"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			out := BuildNodeOutline(sortWorkflow(), OutlineOptions{})
			SortOutline(&out, tc.key)
			if got := childNames(out); !equalNames(got, tc.want) {
				t.Errorf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

// Sorting must not add, drop or reparent a node.
func TestSortOutlineKeepsTheTreeShape(t *testing.T) {
	base := BuildNodeOutline(sortWorkflow(), OutlineOptions{})
	baseRows, _ := FlattenOutline(base, false)

	for _, key := range []NodeSort{NodeSortName, NodeSortPhase, NodeSortStarted} {
		out := BuildNodeOutline(sortWorkflow(), OutlineOptions{})
		SortOutline(&out, key)
		rows, _ := FlattenOutline(out, false)
		if len(rows) != len(baseRows) {
			t.Fatalf("%s: %d rows, want %d", key, len(rows), len(baseRows))
		}
		for _, r := range rows {
			if r.Row.DisplayName != "deploy" && r.Depth != 1 {
				t.Errorf("%s: %s moved to depth %d", key, r.Row.DisplayName, r.Depth)
			}
		}
	}
}

// s is a rotation: three distinct orders, then back to the first.
func TestNodeSortCycleReturnsToStart(t *testing.T) {
	k := NodeSortStarted
	seen := []NodeSort{k}
	for i := 0; i < 2; i++ {
		k = k.Next()
		seen = append(seen, k)
	}
	if k.Next() != NodeSortStarted {
		t.Errorf("cycle ends at %q, want %q", k.Next(), NodeSortStarted)
	}
	for i := range seen {
		for j := i + 1; j < len(seen); j++ {
			if seen[i] == seen[j] {
				t.Fatalf("%q appears twice in the cycle", seen[i])
			}
		}
	}
}
