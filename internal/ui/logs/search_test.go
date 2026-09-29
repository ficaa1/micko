package logs

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// A search is a literal substring match over the retained lines, folded for
// case unless the reader asks otherwise. No match, an empty term or an empty
// buffer is no hits at all.
func TestMatchIndices(t *testing.T) {
	lines := func(texts ...string) []logLine {
		b := NewBuffer(0, 0)
		for _, s := range texts {
			b.Push(rec(s))
		}
		return b.Lines()
	}
	for _, c := range []struct {
		name  string
		lines []logLine
		term  string
		cs    bool
		want  []int
	}{
		{"case folded by default", lines("ERROR: disk full", "info", "Error: retry"), "error", false, []int{0, 2}},
		{"case sensitive on request", lines("Hello", "hello"), "Hello", true, []int{0}},
		{"literal, not a regular expression", lines("a.b", "axb"), "a.b", false, []int{0}},
		{"Unicode case folding", lines("ÀÉÎ OUTPUT"), "àéî", false, []int{0}},
		{"no match", lines("nothing here"), "absent", false, nil},
		{"empty term is not a search", lines("a", "b"), "", false, nil},
		{"a needle longer than any line", lines("short", "short"), strings.Repeat("x", 2048), false, nil},
		{"empty buffer", nil, "x", false, nil},
	} {
		if got := matchIndices(c.lines, c.term, c.cs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: hits = %v, want %v", c.name, got, c.want)
		}
	}
}

// A search shows the reader the word they searched for, in place. The line
// itself is never edited: the retained text, which a copy yanks, stays
// clean.
func TestSearchHighlightsTheTermInPlace(t *testing.T) {
	m := colorModel(t)
	th := shared.NewTheme(false)
	m.ApplyRecords(recs(6))
	m.applySearch("line-2")
	if body := strings.Join(m.BodyLines(), "\n"); !strings.Contains(body, th.Selected.Render("line-2")) {
		t.Fatalf("no highlight reached the screen:\n%q", body)
	}
	for _, l := range m.RawLines() {
		if strings.Contains(l, "\x1b[") {
			t.Fatalf("styling leaked into the retained line %q", l)
		}
	}
}

// n and N walk the hits and wrap around. Jumping to a hit detaches follow,
// or the tail would drag the reader off it.
func TestNextAndPreviousMatchWalkTheHits(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(6))
	m.applySearch("line-") // every line matches
	steps := []struct {
		key  rune
		want int
	}{{'n', 1}, {'N', 0}, {'N', 5}, {'n', 0}}
	if m.curHit != 0 || len(m.hits) != 6 {
		t.Fatalf("a fresh search: hit %d of %d, want 0 of 6", m.curHit, len(m.hits))
	}
	for _, s := range steps {
		press(m, s.key)
		if m.curHit != s.want {
			t.Fatalf("%c: hit %d, want %d", s.key, m.curHit, s.want)
		}
	}
	if m.follow {
		t.Fatal("jumping to a match left follow on")
	}
}

// Esc cancels an edit without touching the committed search; enter commits
// it, and a committed search keeps its hits as more lines arrive.
func TestSearchEditCancelAndCommit(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(8))
	press(m, '/')
	typeInto(m, "zzz")
	pressKey(m, keyEscape)
	if m.searchOn || m.search.term != "" || m.hits != nil {
		t.Fatalf("esc applied the edit: open=%v term=%q hits=%v", m.searchOn, m.search.term, m.hits)
	}
	if body := strings.Join(m.BodyLines(), "\n"); strings.Contains(body, "zzz") {
		t.Fatalf("the cancelled term reached the screen:\n%s", body)
	}
	press(m, '/')
	typeInto(m, "line-3")
	pressKey(m, keyEnter)
	m.ApplyRecords(recs(4))
	if m.searchOn || !reflect.DeepEqual(m.hits, []int{3}) {
		t.Fatalf("after commit and more lines: open=%v hits=%v, want closed and [3]", m.searchOn, m.hits)
	}
}
