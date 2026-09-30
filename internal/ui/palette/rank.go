package palette

import (
	"sort"
	"strings"
)

// Tier says how a query matched a term. Lower is better, and the order is the
// ranking rule: a term the reader typed in full beats one they typed the start
// of, which beats one with a later word they typed the start of, which beats
// one that merely contains the query, which beats one whose letters appear in
// order with gaps.
type Tier int

const (
	// TierExact: the query is the whole term.
	TierExact Tier = iota
	// TierPrefix: the term starts with the query.
	TierPrefix
	// TierWordPrefix: a later word of the term starts with the query, words
	// being split at '-', '_', '.' and '/'. "ml" names "demo-ml" far more
	// surely than it names a term that happens to hold the letters "ml".
	TierWordPrefix
	// TierSubstring: the query appears inside the term.
	TierSubstring
	// TierSubsequence: the query's letters appear in the term in order.
	TierSubsequence
	// TierNone: no match. Match never returns it for a non-empty query.
	TierNone
)

// Candidate is one thing a query can match: a command with its aliases, or
// one argument value. Terms are the strings the query is compared with, most
// canonical first.
type Candidate struct {
	Terms []string
}

// Match is one ranked result. Index is the candidate's position in the slice
// passed to Rank; Term is the term that matched best, which is also what tab
// completion inserts, so a reader who types an alias keeps their alias.
type Match struct {
	Index int
	Term  string
	Tier  Tier
}

// matchTerm scores one term. The comparison ignores case: command names and
// Kubernetes names are lower case, and a reader with caps lock on still means
// the same command.
func matchTerm(query, term string) Tier {
	q, t := strings.ToLower(query), strings.ToLower(term)
	switch {
	case q == t:
		return TierExact
	case strings.HasPrefix(t, q):
		return TierPrefix
	case hasWordPrefix(t, q):
		return TierWordPrefix
	case strings.Contains(t, q):
		return TierSubstring
	case isSubsequence(q, t):
		return TierSubsequence
	default:
		return TierNone
	}
}

// hasWordPrefix reports whether a word after the first in t starts with q.
func hasWordPrefix(t, q string) bool {
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '-', '_', '.', '/':
			if strings.HasPrefix(t[i+1:], q) {
				return true
			}
		}
	}
	return false
}

// isSubsequence reports whether every rune of q appears in t in order.
func isSubsequence(q, t string) bool {
	tr := []rune(t)
	i := 0
	for _, r := range q {
		for i < len(tr) && tr[i] != r {
			i++
		}
		if i == len(tr) {
			return false
		}
		i++
	}
	return true
}

// rank orders the candidates that match query, best first.
//
// A candidate scores as its best term. Ties break on the shorter matching
// term, because the shorter term is closer to what was typed, then on the
// candidate's own position, so the registry order decides between equals and
// the list never reshuffles between two identical keystrokes.
//
// An empty query matches everything in the given order: an empty palette is
// the list of every command.
func rank(query string, cands []Candidate) []Match {
	query = strings.TrimSpace(query)
	out := make([]Match, 0, len(cands))
	if query == "" {
		for i, c := range cands {
			if len(c.Terms) > 0 {
				out = append(out, Match{Index: i, Term: c.Terms[0], Tier: TierPrefix})
			}
		}
		return out
	}
	for i, c := range cands {
		best := Match{Index: i, Tier: TierNone}
		for _, term := range c.Terms {
			if term == "" {
				continue
			}
			tier := matchTerm(query, term)
			if tier < best.Tier || (tier == best.Tier && tier != TierNone && len(term) < len(best.Term)) {
				best = Match{Index: i, Term: term, Tier: tier}
			}
		}
		if best.Tier != TierNone {
			out = append(out, best)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if len(a.Term) != len(b.Term) {
			return len(a.Term) < len(b.Term)
		}
		return a.Index < b.Index
	})
	return out
}
