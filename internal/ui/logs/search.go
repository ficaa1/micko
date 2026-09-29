package logs

import (
	"strings"
)

// search.go — retained-buffer search primitives:
// search over the retained buffer only, scope visible; the search never
// claims un-retained coverage. Matches are literal — no regex — and
// Unicode case-folded by default.

// searchState holds the active search. Empty term = not a search.
type searchState struct {
	term          string
	caseSensitive bool
}

// matchIndices returns indices of lines containing term under the given
// case mode. Empty term or empty lines = nil (no matches). The needle is
// bounded by the longest retained line, which keeps long-needle searches
// cheap. Folding uses strings.ToLower over runes (simple Unicode fold —
// sufficient for the literal, human-typed terms this viewer searches; no
// regex compile, no unicode package dependency).
func matchIndices(lines []logLine, term string, caseSensitive bool) []int {
	if term == "" || len(lines) == 0 {
		return nil
	}
	// Nothing can match a needle longer than the longest line (also keeps
	// adversarial long-needle input from triggering a full scan per line).
	maxLen := 0
	for i := range lines {
		if len(lines[i].Content) > maxLen {
			maxLen = len(lines[i].Content)
		}
	}
	if len(term) > maxLen {
		return nil
	}
	var hits []int
	if caseSensitive {
		for i := range lines {
			if strings.Contains(lines[i].Content, term) {
				hits = append(hits, i)
			}
		}
		return hits
	}
	folded := strings.ToLower(term)
	for i := range lines {
		if strings.Contains(strings.ToLower(lines[i].Content), folded) {
			hits = append(hits, i)
		}
	}
	return hits
}
