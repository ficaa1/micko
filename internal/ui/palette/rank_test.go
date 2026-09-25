package palette

import (
	"strings"
	"testing"
)

func terms(ms []Match, cands []Candidate) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = cands[m.Index].Terms[0]
	}
	return out
}

// Rank orders an exact match, then a prefix, then a match at a later word's
// start, then a substring, then a subsequence, whatever order the candidates
// arrive in.
func TestRankTiers(t *testing.T) {
	cands := []Candidate{
		{Terms: []string{"xwxoxrxk"}},  // subsequence of "work"
		{Terms: []string{"rework"}},    // substring
		{Terms: []string{"cron-work"}}, // later word starts with it
		{Terms: []string{"workflows"}}, // prefix
		{Terms: []string{"work"}},      // exact
		{Terms: []string{"help"}},      // no match
	}
	got := strings.Join(terms(Rank("work", cands), cands), ",")
	want := "work,workflows,cron-work,rework,xwxoxrxk"
	if got != want {
		t.Fatalf("rank = %s, want %s", got, want)
	}
}

// A command scores as its best term, so a typed alias ranks its command as an
// exact match, and the term tab would insert is the alias the reader typed.
func TestRankAliasesAndTheMatchedTerm(t *testing.T) {
	cands := []Candidate{
		{Terms: []string{"workflows", "wf"}},
		{Terms: []string{"cwf-archive"}},
	}
	ms := Rank("wf", cands)
	if len(ms) != 2 || ms[0].Index != 0 || ms[0].Tier != TierExact || ms[0].Term != "wf" {
		t.Fatalf("rank = %+v, want workflows first via its exact alias", ms)
	}
}

// Equal tiers prefer the shorter term, then the registry order, so the list
// is stable between two identical keystrokes.
func TestRankTieBreaks(t *testing.T) {
	cands := []Candidate{
		{Terms: []string{"profile-long"}},
		{Terms: []string{"profile"}},
		{Terms: []string{"proxy"}},
		{Terms: []string{"prune"}},
	}
	got := strings.Join(terms(Rank("pr", cands), cands), ",")
	if got != "proxy,prune,profile,profile-long" {
		t.Fatalf("rank = %s", got)
	}
}

// Case is ignored, and an empty query lists every candidate in its own order.
func TestRankCaseAndEmptyQuery(t *testing.T) {
	cands := []Candidate{{Terms: []string{"zeta"}}, {Terms: []string{"alpha"}}}
	if ms := Rank("ALP", cands); len(ms) != 1 || ms[0].Index != 1 {
		t.Fatalf("case-insensitive rank = %+v", ms)
	}
	if got := strings.Join(terms(Rank("  ", cands), cands), ","); got != "zeta,alpha" {
		t.Fatalf("empty query order = %s, want registry order", got)
	}
}

func TestSubsequenceNeedsOrder(t *testing.T) {
	if !isSubsequence("wfl", "workflows") {
		t.Fatal("wfl is a subsequence of workflows")
	}
	if isSubsequence("lfw", "workflows") {
		t.Fatal("letters out of order must not match")
	}
}
