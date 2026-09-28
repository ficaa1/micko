package workflowlist

import (
	"fmt"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// Benchmarks for PERF-01 (5,000 summaries keyboard-responsive): filter+sort
// over the full snapshot and a full View render.
func benchItems(n int) []core.Summary {
	return summariesFrom(testkit.FixtureWorkflowList("ns", n))
}

func BenchmarkApplyView5k(b *testing.B) {
	m := New(testTheme(), true)
	items := benchItems(5000)
	m.SetItems(items, testkit.FixtureEpoch)
	m.SetSize(120, 40)
	for i := 0; i < b.N; i++ {
		m.SetQuery("fixture-wf-00" + fmt.Sprint(i%10))
	}
}

func BenchmarkViewRender5k(b *testing.B) {
	m := New(testTheme(), true)
	m.SetItems(benchItems(5000), testkit.FixtureEpoch)
	m.SetSize(120, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.ViewAt(testkit.FixtureEpoch)
	}
}
