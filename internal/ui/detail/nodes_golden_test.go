package detail

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNodesTabGolden pins the nodes tab of the demo workflows as a reader
// sees it on the pane of a 140- and an 80-column terminal, in the plain
// theme: the progress header, the status line, the column heads, the tree
// with its bars, and the info panel in the placement each width gives it.
func TestNodesTabGolden(t *testing.T) {
	workflows := []string{
		"demo-release-gate", "demo-nightly-report", "demo-train-pipeline",
		"demo-data-pull", "demo-deploy-multi-layer",
	}
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range workflows {
			m := demoModel(t, name, width, 24)
			b.WriteString("== " + name + "\n")
			b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		}
		m := demoModel(t, "demo-nightly-report", width, 30)
		cursorTo(t, m, "extract")
		m.handleKey("i")
		b.WriteString("== demo-nightly-report, info on extract\n")
		b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")

		compareGolden(t, filepath.Join("testdata", "nodes-"+itoaDetail(width)+".golden"), b.String())
	}
}
