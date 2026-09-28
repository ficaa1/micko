package logs

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// ---------------------------------------------------------------------------
// buffer_test.go — ring-buffer byte/line cap and eviction (plan §8 D1
// slices; acceptance LOG-06: ≤10,000 lines and ≤8 MiB text, whichever hit
// first; eviction order oldest-first; truncation visible; LOG-04: bounded
// oversize handling; legitimate duplicate lines are never deduplicated).
// ---------------------------------------------------------------------------

// TestBufferLineCapEvictsOldestFirst pins ring semantics: when the line
// cap is exceeded the oldest entries leave first, in order.
func TestBufferLineCapEvictsOldestFirst(t *testing.T) {
	b := NewBuffer(3, 0)
	for i := 0; i < 5; i++ {
		if !b.Push(rec(strconv.Itoa(i))) {
			t.Fatalf("line %d unexpectedly dropped", i)
		}
	}
	lines := b.Lines()
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[0].Content != "2" || lines[2].Content != "4" {
		t.Fatalf("eviction order wrong: %q..%q, want 2..4", lines[0].Content, lines[2].Content)
	}
	linesN, _, pushed, evicted, _ := b.Counts()
	if linesN != 3 || pushed != 5 || evicted != 2 {
		t.Fatalf("counts = lines:%d pushed:%d evicted:%d, want 3/5/2", linesN, pushed, evicted)
	}
}

// TestBufferByteCapEvictsOldestFirst pins the byte cap: it evicts oldest
// first, exactly like the line cap (LOG-06: whichever limit is hit first).
func TestBufferByteCapEvictsOldestFirst(t *testing.T) {
	b := NewBuffer(0, 6) // 6 bytes; each 1-char line counts 1 stored byte
	for i := 0; i < 8; i++ {
		b.Push(rec(strconv.Itoa(i)))
	}
	lines := b.Lines()
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want 6 (byte cap reached)", len(lines))
	}
	if lines[0].Content != "2" || lines[5].Content != "7" {
		t.Fatalf("byte-cap eviction wrong: %q..%q, want 2..7", lines[0].Content, lines[5].Content)
	}
	if lines, bytes, _, _, _ := b.Counts(); bytes != 6 || lines != 6 {
		t.Fatalf("counts = lines:%d bytes:%d, want 6/6", lines, bytes)
	}
}

// TestBufferByteCapCountsStoredBytes pins that the byte cap measures the
// sanitized, stored text — ANSI-dressed padding does not inflate it.
func TestBufferByteCapCountsStoredBytes(t *testing.T) {
	raw := "\x1b[31m" + strings.Repeat("x", 100) + "\x1b[0m"
	b := NewBuffer(0, 0)
	b.Push(rec(raw))
	if _, bytes, _, _, _ := b.Counts(); bytes != 100 {
		t.Fatalf("bytes = %d, want 100 (measure after sanitize)", bytes)
	}
}

// TestBufferLineCapAndByteCapTogether pins the "whichever first" rule:
// here the byte cap binds before the (loose) line cap.
func TestBufferLineCapAndByteCapTogether(t *testing.T) {
	// Byte cap 4 evicts before the line cap 10 could bind.
	b := NewBuffer(10, 4)
	for i := 0; i < 6; i++ {
		b.Push(rec(strconv.Itoa(i)))
	}
	lines := b.Lines()
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4 (byte cap binds first)", len(lines))
	}
	if lines[0].Content != "2" || lines[3].Content != "5" {
		t.Fatalf("eviction wrong: %q..%q, want 2..5", lines[0].Content, lines[3].Content)
	}
}

// TestBufferBothCapsBindTogether pins that when both caps are live, the
// retained window satisfies both simultaneously.
func TestBufferBothCapsBindTogether(t *testing.T) {
	b := NewBuffer(3, 4) // 3 lines of 1 byte = 3 bytes ≤ 4: line cap binds
	for i := 0; i < 6; i++ {
		b.Push(rec(strconv.Itoa(i)))
	}
	lines := b.Lines()
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3 (line cap binds)", len(lines))
	}
	if lines[0].Content != "3" || lines[2].Content != "5" {
		t.Fatalf("eviction wrong: %q..%q, want 3..5", lines[0].Content, lines[2].Content)
	}
	if _, bytes, _, _, _ := b.Counts(); bytes != 3 {
		t.Fatalf("bytes = %d, want 3", bytes)
	}
}

// TestBufferTruncatesOversizeLineWithVisibleMarker pins per-line truncation
// with a visible marker (LOG-04: bounded oversize, never a silent clip).
func TestBufferTruncatesOversizeLineWithVisibleMarker(t *testing.T) {
	b := NewBuffer(1, 0)
	huge := strings.Repeat("a", maxLineBytes+100)
	if !b.Push(rec(huge)) {
		t.Fatal("oversize line must be stored (truncated), not dropped")
	}
	got := b.Lines()[0]
	if !got.Truncated {
		t.Fatal("Truncated flag not set for an over-allowance line")
	}
	if len(got.Content) != maxLineBytes {
		t.Fatalf("stored bytes = %d, want %d (per-line allowance)", len(got.Content), maxLineBytes)
	}
	if !strings.HasSuffix(got.Content, markerTruncated) {
		t.Fatalf("visible truncation marker missing: %q", tail(got.Content, 40))
	}
}

// TestBufferDropsRecordBeyondRecordCap pins the single-record bound: a
// record beyond maxRecordBytes is dropped outright and leaves a visible
// dropped marker (LOG-04).
func TestBufferDropsRecordBeyondRecordCap(t *testing.T) {
	b := NewBuffer(4, 0)
	huge := strings.Repeat("a", maxRecordBytes+1)
	if b.Push(rec(huge)) {
		t.Fatal("line beyond the per-record cap must be dropped, not stored")
	}
	lines, _, _, _, dropped := b.Counts()
	if lines != 1 || dropped != 1 { // the dropped marker row remains
		t.Fatalf("counts = lines:%d dropped:%d, want 1/1 (marker row kept)", lines, dropped)
	}
	if n := len(b.Entries()); n != 1 || b.Entries()[0].kind != markDropped {
		t.Fatalf("dropped marker missing: %d entries", n)
	}
}

// TestBufferDeduplicatesNothing pins that identical legitimate lines are
// all kept (LOG-06 gate: duplicate text may be legitimate output).
func TestBufferDeduplicatesNothing(t *testing.T) {
	b := NewBuffer(0, 0)
	for i := 0; i < 10; i++ {
		b.Push(rec("same line"))
	}
	if n := len(b.Entries()); n != 10 {
		t.Fatalf("entries = %d, want 10 (duplicates are legitimate output)", n)
	}
}

// TestBufferMarkersDoNotCountAsBytes pins that markers are UI authoring:
// they consume line slots but never the text budget.
func TestBufferMarkersDoNotCountAsBytes(t *testing.T) {
	b := NewBuffer(0, 8)
	b.PushMarker(markOpen, "pod-1", "main")
	for i := 0; i < 8; i++ {
		b.Push(rec("x"))
	}
	if _, bytes, _, _, _ := b.Counts(); bytes != 8 {
		t.Fatalf("bytes = %d, want 8 (markers excluded from the byte budget)", bytes)
	}
}

// TestBufferReconnectMarkerPrecedesNextLine pins LOG-11 ordering: the gap
// annotation appears before the first line of the new stream.
func TestBufferReconnectMarkerPrecedesNextLine(t *testing.T) {
	b := NewBuffer(8, 0)
	b.Push(rec("before"))
	b.SetReconnect()
	b.Push(rec("after"))
	entries := b.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[1].kind != markReconnect {
		t.Fatalf("entry 1 kind = %v, want reconnect marker", entries[1].kind)
	}
	if entries[2].line.Content != "after" {
		t.Fatalf("entry 2 = %q, want after", entries[2].line.Content)
	}
}

// TestBufferLinesMutationSafe pins snapshot isolation: mutating a Lines()
// result must not affect the buffer.
func TestBufferLinesMutationSafe(t *testing.T) {
	b := NewBuffer(4, 0)
	b.Push(rec("x"))
	before := b.Lines()[0].Content
	s := b.Lines()
	s[0].Content = "mutated"
	if b.Lines()[0].Content != before {
		t.Fatal("mutating a Lines() result must not affect the buffer")
	}
}

// TestBufferZeroCapIsTestOnlyButSane pins the uncapped mode behaves
// sanely (it exists for tests/demo only; production sets both caps).
func TestBufferZeroCapIsTestOnlyButSane(t *testing.T) {
	b := NewBuffer(0, 0)
	for i := 0; i < 50; i++ {
		if !b.Push(rec("x")) {
			t.Fatal("uncapped buffer dropped a line")
		}
	}
	if n := len(b.Entries()); n != 50 {
		t.Fatalf("entries = %d, want 50", n)
	}
}

// ---------------------------------------------------------------------------
// Search primitives (slice: retained-buffer search; acceptance LOG-09 —
// literal semantics, case-folded by default, scope bounded by the buffer).
// ---------------------------------------------------------------------------

func TestSearchMatchesCaseInsensitive(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("ERROR: disk full"))
	b.Push(rec("info: all good"))
	b.Push(rec("Error: retry later"))
	got := b.Search(searchState{term: "error"})
	if len(got) != 2 {
		t.Fatalf("matches = %d, want 2", len(got))
	}
	if got[0] != 0 || got[1] != 2 {
		t.Fatalf("match indices = %v, want [0 2]", got)
	}
}

func TestSearchEmptyTermIsNoSearch(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("a"))
	b.Push(rec("b"))
	if got := b.Search(searchState{term: ""}); got != nil {
		t.Fatalf("empty term = %v, want nil (empty term is not a search)", got)
	}
}

func TestSearchIsLiteralNotRegex(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("a.b"))
	b.Push(rec("axb"))
	got := b.Search(searchState{term: "a.b"})
	if len(got) != 1 {
		t.Fatalf("regex semantics leaked: matches = %d, want 1", len(got))
	}
}

func TestSearchCaseSensitiveMode(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("Hello"))
	b.Push(rec("hello"))
	m := searchState{term: "Hello", caseSensitive: true}
	if got := b.Search(m); len(got) != 1 || got[0] != 0 {
		t.Fatalf("case-sensitive matches = %v, want [0]", got)
	}
	m.term = "hello"
	if got := b.Search(m); len(got) != 1 || got[0] != 1 {
		t.Fatalf("case-sensitive lowercase matches = %v, want [1]", got)
	}
}

func TestSearchUnicodeCaseFolding(t *testing.T) {
	b := NewBuffer(0, 0)
	b.Push(rec("ÀÉÎ OUTPUT"))
	if got := b.Search(searchState{term: "àéî"}); len(got) != 1 {
		t.Fatalf("unicode case-fold match failed: %v", got)
	}
}

func TestSearchLongNeedleMatchesNothing(t *testing.T) {
	// A needle longer than any line matches nothing and stays fast.
	b := NewBuffer(0, 0)
	for i := 0; i < 100; i++ {
		b.Push(rec("short"))
	}
	needle := strings.Repeat("x", 2048)
	if got := b.Search(searchState{term: needle}); got != nil {
		t.Fatalf("long needle hits = %v, want nil", got)
	}
}

func TestSearchEmptyBufferReturnsNil(t *testing.T) {
	b := NewBuffer(4, 0)
	if got := b.Search(searchState{term: "x"}); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Sanitize path (slice: sanitized long/unicode/log-control-sequence
// rendering; LOG-05/SEC-01). The buffer stores sanitized content at Push
// time so nothing unsafe can ever reach a later View.
// ---------------------------------------------------------------------------

func TestBufferSanitizesContentOnPush(t *testing.T) {
	b := NewBuffer(4, 0)
	b.Push(rec("\x1b]0;pwned\x07normal \x1b[31mred\x1b[0m text"))
	lines := b.Lines()
	if lines[0].Content != "normal red text" {
		t.Fatalf("control sequences survived: %q", lines[0].Content)
	}
}

func TestBufferSanitizePreservesTabsAndNewlines(t *testing.T) {
	b := NewBuffer(4, 0)
	b.Push(rec("col1\tcol2\nnext"))
	if lines := b.Lines(); lines[0].Content != "col1\tcol2\nnext" {
		t.Fatalf("safe whitespace lost: %q", lines[0].Content)
	}
}

// TestBufferMarkerContextSanitized pins that a hostile pod/container name
// cannot smuggle control sequences into any rendered marker row: the
// open-context header carries the (sanitized) name, the reconnect marker
// never echoes it (SEC-01).
func TestBufferMarkerContextSanitized(t *testing.T) {
	b := NewBuffer(4, 0)
	b.PushMarker(markOpen, "\x1b]0;pwned\x07pod", "main")
	e := b.Entries()[0]
	got := markerText(e.kind, e.ctxPod, e.ctxCont)
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("escape byte survived in marker: %q", got)
	}
	if !strings.Contains(got, "pod") {
		t.Fatalf("sanitized context lost: %q", got)
	}
	// The reconnect marker is a fixed annotation: no untrusted context.
	if r := markerText(markReconnect, "\x1b[2Jx", "y"); strings.ContainsRune(r, 0x1b) || strings.Contains(r, "x") {
		t.Fatalf("reconnect marker must be a fixed, context-free annotation: %q", r)
	}
}

// TestBufferSustainedStreamSettles pins LOG-14: a sustained high-volume
// stream keeps both caps obeyed — the retained counts stay at the caps
// while pushes keep succeeding (no unbounded growth).
func TestBufferSustainedStreamSettles(t *testing.T) {
	b := NewBuffer(MaxLines, MaxBytes)
	for i := 0; i < MaxLines+5000; i++ {
		if !b.Push(rec("sustained-" + strconv.Itoa(i%1000))) {
			t.Fatalf("push %d dropped unexpectedly", i)
		}
	}
	lines, bytes, _, _, _ := b.Counts()
	if lines != MaxLines {
		t.Fatalf("lines = %d, want %d (line cap)", lines, MaxLines)
	}
	if bytes > MaxBytes {
		t.Fatalf("bytes = %d, want <= %d (byte cap)", bytes, MaxBytes)
	}
}

var (
	_ = testkit.FixtureEpoch
	_ = core.LogRecord{}
)

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
