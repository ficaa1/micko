// Package filter.go is part of the B1 list component: local filters over
// the injected snapshot (plan §8 B1; LIST-05/09 scope rules).
package workflowlist

import (
	"strings"

	"argo-tui/internal/core"
)

// PhaseFilter is the local phase bucket. "Other" collects every phase the
// pinned upstream type set does not define, so unknown server phases stay
// visible and filterable instead of disappearing (LIST-11).
type PhaseFilter string

const (
	PhaseAll       PhaseFilter = "All"
	PhaseRunning   PhaseFilter = "Running"
	PhasePending   PhaseFilter = "Pending"
	PhaseSucceeded PhaseFilter = "Succeeded"
	PhaseFailed    PhaseFilter = "Failed"
	PhaseOther     PhaseFilter = "Other"
)

// phaseCycle is the deterministic rotation order for the phase key.
var phaseCycle = []PhaseFilter{
	PhaseAll, PhaseRunning, PhasePending, PhaseSucceeded, PhaseFailed, PhaseOther,
}

// Next returns the next phase filter in the rotation.
func (f PhaseFilter) Next() PhaseFilter {
	for i, p := range phaseCycle {
		if p == f {
			return phaseCycle[(i+1)%len(phaseCycle)]
		}
	}
	return PhaseAll
}

// Matches reports whether a summary phase belongs to this bucket.
func (f PhaseFilter) Matches(phase string) bool {
	switch f {
	case PhaseAll:
		return true
	case PhaseOther:
		return !isCanonicalPhase(phase)
	default:
		return string(f) == phase
	}
}

// isCanonicalPhase covers the phases the pinned upstream types define
// (docs/protocol.md §4). "Error" is canonical upstream even though the
// theme styles it like Failed.
func isCanonicalPhase(p string) bool {
	switch p {
	case "Running", "Pending", "Succeeded", "Failed", "Error":
		return true
	}
	return false
}

// nameMatches is the local search predicate: case-insensitive substring
// over the workflow name. The scope is always "names within the loaded
// snapshot" and the view must say so.
func nameMatches(name, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(query))
}

// unused core import guard removal: core is re-used by future filter work.
var _ = core.Ref{}
