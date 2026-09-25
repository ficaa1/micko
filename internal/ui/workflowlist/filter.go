// Package filter.go is part of the B1 list component: local filters over
// the injected snapshot (plan §8 B1; LIST-05/09 scope rules).
package workflowlist

import (
	"strings"

	"github.com/ficaa1/argo-tui/internal/core"
)

// PhaseFilter is the local phase bucket. "Other" collects every phase the
// pinned upstream type set does not define, so unknown server phases stay
// visible and filterable instead of disappearing (LIST-11).
type PhaseFilter string

const (
	PhaseAll PhaseFilter = "All"
	// PhaseSuspended selects only the workflows parked on a manual gate.
	// They are Running to the server, but a person has to act on them, so
	// they get their own bucket rather than hiding among the running ones.
	PhaseSuspended PhaseFilter = "Suspended"
	PhaseRunning   PhaseFilter = "Running"
	PhasePending   PhaseFilter = "Pending"
	PhaseSucceeded PhaseFilter = "Succeeded"
	PhaseFailed    PhaseFilter = "Failed"
	PhaseOther     PhaseFilter = "Other"
)

// phaseCycle is the deterministic rotation order for the phase key.
var phaseCycle = []PhaseFilter{
	PhaseAll, PhaseSuspended, PhaseRunning, PhasePending, PhaseSucceeded, PhaseFailed, PhaseOther,
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

// Matches reports whether a summary belongs to this bucket. It takes the
// whole summary because "Suspended" is not a server phase: it is a running
// workflow that also holds an open Suspend node.
func (f PhaseFilter) Matches(s core.Summary) bool {
	switch f {
	case PhaseAll:
		return true
	case PhaseSuspended:
		return s.Suspended
	case PhaseOther:
		return !isCanonicalPhase(s.Phase)
	default:
		return string(f) == s.Phase
	}
}

// DisplayPhase is the phase word the list shows for a summary. A suspended
// workflow reports "Suspended" so a reader can tell at a glance which of the
// running rows is actually waiting for them.
func DisplayPhase(s core.Summary) string {
	if s.Suspended {
		return "Suspended"
	}
	return s.Phase
}

// isCanonicalPhase covers the phases the pinned upstream types define
// (docs/development.md). "Error" is canonical upstream even though the
// theme styles it like Failed.
func isCanonicalPhase(p string) bool {
	switch p {
	case "Running", "Pending", "Succeeded", "Failed", "Error":
		return true
	}
	return false
}

// matches is the list's search predicate. Across namespaces it also matches
// the row's "namespace/name", so typing a namespace, or "namespace/" for one
// namespace alone, narrows the cluster-wide list to it. In one namespace the
// namespace is the same on every row and matching it would match everything.
func (m *Model) matches(ref core.Ref, query string) bool {
	if m.allNS {
		return nameMatches(ref.Namespace+"/"+ref.Name, query)
	}
	return nameMatches(ref.Name, query)
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

var _ = core.Ref{}
