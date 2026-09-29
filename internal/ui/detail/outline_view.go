package detail

import (
	"sort"
	"strings"

	"github.com/ficaa1/micko/internal/ui/shared"
)

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
	// Absent phase ⇒ "not yet started"; never render an
	// empty phase.
	if r.Phase == "" {
		return "not started"
	}
	return r.Phase
}
