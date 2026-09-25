package detail

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// renderOutlinePane renders the outline rows as an indented text pane.
// Deterministic: builds on the sorted outline and never invents ordering.
func renderOutlinePane(out Outline) string {
	var b strings.Builder
	if !out.Available {
		// DET-04: explicit unavailable state — never an empty workflow.
		b.WriteString("node status unavailable")
		if out.UnavailableReason != "" {
			b.WriteString(" (" + shared.Sanitize(out.UnavailableReason) + ")")
		}
		b.WriteString("\n")
		return b.String()
	}
	if len(out.Rows) == 0 {
		b.WriteString("(no nodes yet — workflow not started)\n")
	}
	var walk func(rows []OutlineRow, depth int)
	walk = func(rows []OutlineRow, depth int) {
		for _, r := range rows {
			for i := 0; i < depth; i++ {
				b.WriteString("  ")
			}
			b.WriteString(fmt.Sprintf("- %s type=%s phase=%s",
				shared.Sanitize(rowDisplayName(r)), shared.Sanitize(rowType(r)),
				shared.Sanitize(rowPhase(r))))
			if len(r.Deps) > 0 {
				b.WriteString(" after=" + shared.Sanitize(strings.Join(r.Deps, ",")))
			}
			if r.Message != "" {
				b.WriteString(" msg=" + shared.Sanitize(r.Message))
			}
			b.WriteString("\n")
			walk(r.Children, depth+1)
		}
	}
	walk(out.Rows, 0)
	// Ungrouped sections (DET-08): dangling references and unreachable
	// nodes are listed explicitly.
	if len(out.Dangling) > 0 {
		b.WriteString("ungrouped (referenced but missing):\n")
		for _, d := range out.Dangling {
			b.WriteString("  ? " + shared.Sanitize(d.NodeID) + " " + shared.Sanitize(d.Message) + "\n")
		}
	}
	if len(out.Unreachable) > 0 {
		b.WriteString("ungrouped (not attached):\n")
		for _, d := range out.Unreachable {
			b.WriteString("  ! " + shared.Sanitize(d.NodeID) + " " + shared.Sanitize(d.Message) + "\n")
		}
	}
	return b.String()
}

// renderLabelsSorted renders label pairs sorted by key (deterministic).
func renderLabelsSorted(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, shared.Sanitize(k)+"="+shared.RedactTokens(shared.Sanitize(labels[k])))
	}
	return strings.Join(parts, ", ")
}

// rowDisplayName resolves the row's display name (DisplayName → Name → ID).
func rowDisplayName(r OutlineRow) string {
	switch {
	case r.DisplayName != "":
		return r.DisplayName
	case r.Name != "":
		return r.Name
	default:
		return r.NodeID
	}
}

func rowType(r OutlineRow) string {
	if r.Type == "" {
		return "unknown"
	}
	return r.Type
}

func rowPhase(r OutlineRow) string {
	// Absent phase ⇒ "not yet started" (protocol §2); never render an
	// empty phase.
	if r.Phase == "" {
		return "not started"
	}
	return r.Phase
}
