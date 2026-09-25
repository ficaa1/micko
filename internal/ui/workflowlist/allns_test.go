package workflowlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// crossNamespaceItems holds the same workflow name in two namespaces, which is
// exactly the pair the NAMESPACE column has to tell apart.
func crossNamespaceItems() []core.Summary {
	mk := func(ns, name, uid, phase string) core.Summary {
		return core.Summary{Ref: core.Ref{Namespace: ns, Name: name, UID: uid}, Phase: phase,
			CreatedAt: testkit.FixtureEpoch}
	}
	return []core.Summary{
		mk("team-b", "etl", "uid-b", "Running"),
		mk("team-a", "etl", "uid-a", "Running"),
		mk("ml", "sweep", "uid-m", "Succeeded"),
	}
}

func allNSModel(width int) Model {
	m := New(testTheme(), true)
	m.SetSize(width, 0)
	m.SetAllNamespaces(true)
	m.SetItems(crossNamespaceItems(), testkit.FixtureEpoch)
	return m
}

// Across namespaces the table leads with a NAMESPACE column, and each row
// shows its own namespace in it.
func TestAllNamespacesAddsTheNamespaceColumn(t *testing.T) {
	m := allNSModel(120)
	lines := m.BodyLines(testkit.FixtureEpoch)
	if !strings.HasPrefix(lines[1], strings.Repeat(" ", markGutter)+"NAMESPACE  NAME") {
		t.Fatalf("column heads = %q, want NAMESPACE first", lines[1])
	}
	body := strings.Join(lines, "\n")
	for _, want := range []string{"team-a     etl", "team-b     etl", "ml         sweep"} {
		if !strings.Contains(body, want) {
			t.Errorf("rows lack %q:\n%s", want, body)
		}
	}
	if !strings.Contains(lines[0], "filter by namespace/name") {
		t.Errorf("toolbar = %q, want the namespace-aware filter hint", lines[0])
	}

	one := New(testTheme(), true)
	one.SetSize(120, 0)
	one.SetItems(crossNamespaceItems(), testkit.FixtureEpoch)
	if head := one.BodyLines(testkit.FixtureEpoch)[1]; strings.Contains(head, "NAMESPACE") {
		t.Fatalf("one namespace shows a NAMESPACE column: %q", head)
	}
}

// The namespace column comes out of the message column's room, and on a
// narrow pane out of NAME's, so the table still fits the pane. The toolbar
// above it is clipped by the shell and is not part of the table.
func TestNamespaceColumnFitsEveryWidth(t *testing.T) {
	for _, w := range []int{60, 76, 100, 136} {
		m := allNSModel(w)
		for _, l := range m.BodyLines(testkit.FixtureEpoch)[1:] {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("width %d: line %q is %d cells", w, l, got)
			}
		}
	}
}

// A long namespace is clipped to the column's bound instead of pushing the
// NAME column right.
func TestNamespaceColumnIsBounded(t *testing.T) {
	m := New(testTheme(), true)
	m.SetSize(80, 0)
	m.SetAllNamespaces(true)
	long := core.Summary{Ref: core.Ref{Namespace: strings.Repeat("n", 40), Name: "wf", UID: "u"}, Phase: "Running"}
	m.SetItems([]core.Summary{long}, testkit.FixtureEpoch)
	if w := m.nsWidth(); w != 16 {
		t.Fatalf("nsWidth = %d, want the 80-column bound of 16", w)
	}
	row := m.BodyLines(testkit.FixtureEpoch)[2]
	if !strings.HasPrefix(row, strings.Repeat(" ", markGutter)+strings.Repeat("n", 15)+"…  wf") {
		t.Fatalf("row = %q, want the namespace clipped with an ellipsis", row)
	}
}

// Across namespaces the filter matches "namespace/name", so a namespace (or
// "namespace/") narrows the list to it. In one namespace it matches names
// only.
func TestAllNamespacesFilterMatchesTheNamespace(t *testing.T) {
	m := allNSModel(120)
	m.SetQuery("team-a/")
	if rows := m.Rows(); len(rows) != 1 || rows[0].Ref.UID != "uid-a" {
		t.Fatalf("team-a/ matched %+v", rows)
	}
	m.SetQuery("etl")
	if n := len(m.Rows()); n != 2 {
		t.Fatalf("etl matched %d rows, want both namespaces", n)
	}

	// The query language's other name predicates see the namespace too,
	// and an anchored pattern still matches the bare name.
	for q, want := range map[string]int{"/^team-b\\//": 1, "/^etl$/": 2, "~tma": 1, "!team-a/": 2} {
		m.SetQuery(q)
		if n := len(m.Rows()); n != want {
			t.Errorf("%s matched %d rows, want %d", q, n, want)
		}
	}

	one := New(testTheme(), true)
	one.SetItems(crossNamespaceItems(), testkit.FixtureEpoch)
	one.SetQuery("team-a")
	if n := len(one.Rows()); n != 0 {
		t.Fatalf("in one namespace the filter matched the namespace (%d rows)", n)
	}
}

// Two rows of the same name sort by namespace, whatever order they arrived
// in, under the name and the default sorts alike.
func TestSameNameSortsByNamespace(t *testing.T) {
	for _, key := range []SortKey{SortName, SortPhaseName} {
		rows := Sort(crossNamespaceItems()[:2], key)
		if rows[0].Ref.Namespace != "team-a" || rows[1].Ref.Namespace != "team-b" {
			t.Errorf("%s: order = %s, %s", key, rows[0].Ref.Namespace, rows[1].Ref.Namespace)
		}
	}
}

// An empty cluster-wide list says what it covered.
func TestAllNamespacesEmptyState(t *testing.T) {
	m := New(testTheme(), true)
	m.SetAllNamespaces(true)
	m.SetItems(nil, testkit.FixtureEpoch)
	if v := m.View(); !strings.Contains(v, "no workflows in any namespace this token can read") {
		t.Fatalf("empty all-namespaces view:\n%s", v)
	}
}

// A failed first collection is an error, not an empty namespace.
func TestStaleWithNothingCollectedIsAnError(t *testing.T) {
	m := New(testTheme(), true)
	m.SetItems(nil, testkit.FixtureEpoch)
	m.SetStatus(StatusStale, "connection refused", 0)
	v := m.View()
	if !strings.Contains(v, "no workflows visible: connection refused") || strings.Contains(v, "no workflows in this namespace") {
		t.Fatalf("view:\n%s", v)
	}
}
