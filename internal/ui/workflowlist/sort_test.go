package workflowlist

import (
	"slices"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

func TestListSortOrders(t *testing.T) {
	now := testkit.FixtureEpoch
	start := now.Add(-time.Hour)
	recentStart := now.Add(-time.Minute)
	items := []core.Summary{summary("zebra", "Running"), summary("apple", "Mystery"), summary("mango", "Failed"), summary("banana", "Running"), summary("missing", "Running")}
	items[0].CreatedAt = now.Add(-3 * time.Hour)
	items[0].StartedAt = &recentStart
	items[1].CreatedAt = now.Add(-3 * time.Hour)
	items[2].CreatedAt = now.Add(-2 * time.Hour)
	items[3].CreatedAt = now.Add(-2 * time.Hour)
	items[3].StartedAt = &start
	m := newList(t)
	m.SetItems(items, now)
	for _, c := range []struct {
		key  string
		want []string
	}{
		{"phase", []string{"mango", "zebra", "banana", "missing", "apple"}},
		{"name", []string{"apple", "banana", "mango", "missing", "zebra"}},
		{"time", []string{"banana", "mango", "apple", "zebra", "missing"}},
		{"phase", []string{"mango", "zebra", "banana", "missing", "apple"}},
	} {
		if string(m.sort) != c.key || !slices.Equal(rowIDs(m.Rows()), c.want) || m.SelectedRef().UID != c.want[0] {
			t.Fatalf("%s: rows %v selection %v", c.key, rowIDs(m.Rows()), m.SelectedRef())
		}
		m.Update(runeKey('j'))
		m.Update(runeKey('s'))
	}
	t.Run("failure common rank", func(t *testing.T) {
		for _, phases := range [][2]string{{"Error", "Failed"},
			{"Failed", "Error"}} {
			m := newList(t)
			m.SetItems([]core.Summary{summary("z", phases[0]), summary("a", phases[1])}, now)
			if got := rowIDs(m.Rows()); !slices.Equal(got, []string{"a", "z"}) {
				t.Fatalf("%v: %v", phases, got)
			}
		}
	})
	t.Run("namespace tie", func(t *testing.T) {
		m := newList(t)
		m.SetItems(crossNamespaceItems()[:2], now)
		for i := 0; i < 2; i++ {
			if got := rowIDs(m.Rows()); !slices.Equal(got, []string{"uid-a", "uid-b"}) {
				t.Fatalf("%s: %v", m.sort, got)
			}
			m.Update(runeKey('s'))
		}
	})
	t.Run("missing timestamps and equal time keys", func(t *testing.T) {
		m := newList(t)
		a, b := summary("a-missing", "Running"), summary("b-missing", "Running")
		x, y := summary("x", "Running"), summary("y", "Running")
		x.Ref.Name = "same"
		y.Ref.Name = "same"
		x.CreatedAt = now
		y.CreatedAt = now
		m.SetItems([]core.Summary{b, y, a, x}, now)
		m.Update(runeKey('s'))
		m.Update(runeKey('s'))
		if got := rowIDs(m.Rows()); !slices.Equal(got, []string{"y", "x", "a-missing", "b-missing"}) {
			t.Fatalf("time ties preserve snapshot order: %v", got)
		}
		m.SetItems([]core.Summary{b, y, a, x}, now)
		if got := rowIDs(m.Rows()); !slices.Equal(got, []string{"y", "x", "a-missing", "b-missing"}) {
			t.Fatalf("snapshot time stable order: %v", got)
		}
	})
}

func TestPhaseFilters(t *testing.T) {
	m := newList(t)
	gate := summary("gate", "Running")
	gate.Suspended = true
	m.SetItems([]core.Summary{gate, summary("run", "Running"), summary("pending", "Pending"), summary("done", "Succeeded"), summary("fail", "Failed"), summary("error", "Error"), summary("future", "Mystery"), summary("missing", "")}, testkit.FixtureEpoch)
	for _, c := range []struct {
		phase PhaseFilter
		want  []string
	}{
		{PhaseAll, []string{"gate", "error", "fail", "run", "pending", "done", "future", "missing"}},
		{PhaseSuspended, []string{"gate"}},
		{PhaseRunning, []string{"gate", "run"}},
		{PhasePending, []string{"pending"}},
		{PhaseSucceeded, []string{"done"}},
		{PhaseFailed, []string{"fail"}},
		{PhaseOther, []string{"future", "missing"}},
		{PhaseAll, []string{"gate", "error", "fail", "run", "pending", "done", "future", "missing"}},
	} {
		if m.phase != c.phase || !slices.Equal(rowIDs(m.Rows()), c.want) {
			t.Fatalf("%s: rows %v want %v", c.phase, rowIDs(m.Rows()), c.want)
		}
		m.Update(runeKey('p'))
	}
}
