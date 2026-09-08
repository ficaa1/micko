package logs

import (
	"testing"
)

// ---------------------------------------------------------------------------
// search_test.go — retained-buffer search UX (LOG-09). Additional
// primitive coverage beyond buffer_test.go: scope bounded to retained
// lines only, indices ordered, nil on no-match.
// ---------------------------------------------------------------------------

func TestMatchIndicesHitOrder(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("alpha beta"))
	b.Push(rec("gamma delta"))
	b.Push(rec("beta again"))
	hits := matchIndices(b.Lines(), "beta", false)
	if len(hits) != 2 || hits[0] != 0 || hits[1] != 2 {
		t.Fatalf("hits = %v, want [0 2]", hits)
	}
}

func TestMatchIndicesNoMatchNil(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("nothing here"))
	if got := matchIndices(b.Lines(), "absent", false); got != nil {
		t.Fatalf("no-match hits = %v, want nil", got)
	}
}

func TestMatchIndicesScopeIsInput(t *testing.T) {
	// The primitive searches exactly the lines passed in: scope honesty
	// (LOG-09) is structural — the model only feeds retained lines.
	b := NewBuffer(0, 0)
	b.Push(rec("hit"))
	b.Push(rec("miss"))
	b.Push(rec("HIT"))
	got := matchIndices(b.Lines(), "hit", false)
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("folded hits = %v, want [0 2]", got)
	}
}
