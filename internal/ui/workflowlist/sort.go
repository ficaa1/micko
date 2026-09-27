// sort.go — deterministic sort orders for the workflow list (B1).
//
// All orders are total, stable and independent of map iteration order.
// Missing timestamps must sort "sensibly" (plan gate): never crash, never
// jump non-deterministically, and never bury unfinished work behind
// finished work by accident — see each comparator's rule below.
package workflowlist

import (
	"cmp"
	"sort"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// SortKey selects the sort order. Sorting is always deterministic and
// stable; equal rows keep their relative order (stable sort on the input).
type SortKey string

const (
	// SortPhaseName is the default: phase (canonical order, unknown last),
	// then name. Unknown/missing phases sort deterministically last within
	// their bucket so tolerant display stays stable (LIST-11).
	SortPhaseName SortKey = "phase"
	// SortName is by name, case-insensitively.
	SortName SortKey = "name"
	// SortTime is newest-created first, but NOT-yet-CREATED (zero
	// CreatedAt, i.e. timestamp missing) last, and among missing ones by
	// name — "missing timestamps sensible" (plan B1 gate; DET-10 analogue).
	SortTime SortKey = "time"
)

// Next returns the next sort key in rotation (a single cycling key, not a
// modifier — keep the interaction discoverable and testable).
func (k SortKey) Next() SortKey {
	switch k {
	case SortPhaseName:
		return SortName
	case SortName:
		return SortTime
	default:
		return SortPhaseName
	}
}

// phaseRank groups phases into a small canonical order used by SortPhaseName
// and by the local phase bucketing; unknown phases rank last.
func phaseRank(p string) int {
	switch p {
	case "Suspended":
		return 0 // waiting on a person: nothing moves until someone acts
	case "Failed", "Error":
		return 1 // failures next: they are what a user triages
	case "Running":
		return 2
	case "Pending":
		return 3
	case "Succeeded":
		return 4
	default:
		return 5 // unknown/empty phases (LIST-11)
	}
}

// summaryRank ranks a summary, treating an open manual gate as its own
// phase so those rows sort to the top of the list.
func summaryRank(s core.Summary) int { return phaseRank(DisplayPhase(s)) }

// newerFirst compares two summaries by age, newest first, with missing
// timestamps last. It returns 0 when neither is newer, so callers can fall
// through to a name tiebreak and keep the order total.
func newerFirst(a, b core.Summary) int {
	ta, tb := ageKey(a), ageKey(b)
	switch {
	case ta.IsZero() && tb.IsZero():
		return 0
	case ta.IsZero():
		return 1 // missing timestamps last
	case tb.IsZero():
		return -1
	case ta.Equal(tb):
		return 0
	case ta.After(tb):
		return -1
	default:
		return 1
	}
}

// Sort sorts items in place with the given key. It never panics on zero
// timestamps and never uses map iteration order.
func Sort(items []core.Summary, key SortKey) []core.Summary {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch key {
		case SortName:
			if c := cmp.Compare(toLower(a.Ref.Name), toLower(b.Ref.Name)); c != 0 {
				return c < 0
			}
			// The same name in two namespaces is two workflows; the
			// namespace orders them before the arbitrary UID does.
			if a.Ref.Namespace != b.Ref.Namespace {
				return a.Ref.Namespace < b.Ref.Namespace
			}
			return a.Ref.UID < b.Ref.UID
		case SortTime:
			// Rule: rows with a CreatedAt sort newest-first; rows with
			// zero/missing CreatedAt go last, ordered by name (never
			// interleaved with real times — zero time would otherwise be
			// "oldest ever" and bury real data at the bottom).
			switch {
			case a.CreatedAt.IsZero() && b.CreatedAt.IsZero():
				return a.Ref.Name < b.Ref.Name
			case a.CreatedAt.IsZero():
				return false
			case b.CreatedAt.IsZero():
				return true
			default:
				if !a.CreatedAt.Equal(b.CreatedAt) {
					return a.CreatedAt.After(b.CreatedAt)
				}
				return a.Ref.Name < b.Ref.Name
			}
		default: // SortPhaseName
			// Phase groups first, then newest within the group. Reading a
			// group of running workflows is a chronological job: the newest
			// run is the one an operator just triggered and wants to see.
			ra, rb := summaryRank(a), summaryRank(b)
			if ra != rb {
				return ra < rb
			}
			if c := newerFirst(a, b); c != 0 {
				return c < 0
			}
			if c := cmp.Compare(toLower(a.Ref.Name), toLower(b.Ref.Name)); c != 0 {
				return c < 0
			}
			if a.Ref.Namespace != b.Ref.Namespace {
				return a.Ref.Namespace < b.Ref.Namespace
			}
			return a.Ref.UID < b.Ref.UID
		}
	})
	return items
}

// NewestFirst is the age-column presentation rule shared with tests: start
// time if present, else created time; zero values sort last and a name
// tiebreak keeps the order total (missing timestamps stay sensible, LIST-09).
func ageKey(s core.Summary) time.Time {
	if s.StartedAt != nil && !s.StartedAt.IsZero() {
		return *s.StartedAt
	}
	return s.CreatedAt
}

// AgeOrdered returns items ordered for the AGE column display: newest
// activity first, missing-timestamp rows last by name. Sorting itself uses
// Sort; this helper exists so the view and tests share one rule.
func AgeOrdered(items []core.Summary) []core.Summary {
	out := make([]core.Summary, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := ageKey(out[i]), ageKey(out[j])
		if a.IsZero() && b.IsZero() {
			return out[i].Ref.Name < out[j].Ref.Name
		}
		if a.IsZero() {
			return false
		}
		if b.IsZero() {
			return true
		}
		if !a.Equal(b) {
			return a.After(b)
		}
		return out[i].Ref.Name < out[j].Ref.Name
	})
	return out
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if 'A' <= b[i] && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
