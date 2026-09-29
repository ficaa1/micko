package detail

import (
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// The tree build at 1,000 and 5,000 tasks and over a 5,000-step chain.
func BenchmarkNodeOutline(b *testing.B) {
	for name, wf := range map[string]core.Workflow{
		"dag1000":   syntheticWideDAG("bench", 1000),
		"dag5000":   syntheticWideDAG("bench", 5000),
		"steps5000": syntheticStepsChain("bench", 5000),
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if out := BuildNodeOutline(wf, OutlineOptions{}); len(out.Rows) == 0 {
					b.Fatal("empty outline")
				}
			}
		})
	}
}

// One fold key and one frame of a 5,000-node workflow, on the nodes tab and the timeline.
func BenchmarkSection5000(b *testing.B) {
	for _, section := range []string{"nodes", "timeline"} {
		m := workflowModel(syntheticWideDAG("bench", 5000), 160, 50)
		m.SetSection(section)
		b.Run(section+"/fold", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				press(m, "space")
			}
		})
		b.Run(section+"/render", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if len(m.BodyLines()) == 0 {
					b.Fatal("empty body")
				}
			}
		})
	}
}
