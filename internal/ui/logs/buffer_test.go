package logs

import (
	"strconv"
	"strings"
	"testing"
)

// The retained window obeys both caps, whichever binds first, and always
// evicts the oldest lines. Evictions are counted so the pane can report
// them.
func TestBufferCapsEvictOldestFirst(t *testing.T) {
	for _, c := range []struct {
		name               string
		maxLines, maxBytes int
		push               int // one-byte lines "0", "1", ...
		wantFirst, wantLen int
	}{
		{"line cap", 3, 0, 5, 2, 3},
		{"byte cap", 0, 6, 8, 2, 6},
		{"byte cap binds before the line cap", 10, 4, 6, 2, 4},
		{"line cap binds before the byte cap", 3, 4, 6, 3, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := NewBuffer(c.maxLines, c.maxBytes)
			for i := 0; i < c.push; i++ {
				if !b.Push(rec(strconv.Itoa(i))) {
					t.Fatalf("line %d dropped", i)
				}
			}
			lines := b.Lines()
			if len(lines) != c.wantLen {
				t.Fatalf("retained %d lines, want %d", len(lines), c.wantLen)
			}
			if first, last := lines[0].Content, lines[len(lines)-1].Content; first != strconv.Itoa(c.wantFirst) || last != strconv.Itoa(c.push-1) {
				t.Fatalf("retained %s..%s, want %d..%d", first, last, c.wantFirst, c.push-1)
			}
			n, bytes, pushed, evicted, _ := b.Counts()
			if n != c.wantLen || bytes != c.wantLen || pushed != int64(c.push) || evicted != int64(c.push-c.wantLen) {
				t.Fatalf("counts lines=%d bytes=%d pushed=%d evicted=%d", n, bytes, pushed, evicted)
			}
		})
	}
}

// A sustained stream at the production caps settles at the caps while
// every push keeps succeeding: memory stays bounded however long a stream
// runs, paused or not.
func TestBufferSustainedStreamSettles(t *testing.T) {
	b := NewBuffer(MaxLines, MaxBytes)
	for i := 0; i < MaxLines+5000; i++ {
		if !b.Push(rec("sustained-" + strconv.Itoa(i%1000))) {
			t.Fatalf("push %d dropped", i)
		}
	}
	lines, bytes, _, _, _ := b.Counts()
	if lines != MaxLines || bytes > MaxBytes {
		t.Fatalf("lines=%d bytes=%d, caps %d/%d", lines, bytes, MaxLines, MaxBytes)
	}
}

// The byte cap measures stored text: markers never count, and escape
// sequences stripped on push do not either.
func TestBufferByteBudgetCountsStoredTextOnly(t *testing.T) {
	b := NewBuffer(0, 0)
	b.PushMarker(markOpen, "pod-1", "main")
	b.Push(rec("\x1b[31m" + strings.Repeat("x", 100) + "\x1b[0m"))
	if _, bytes, _, _, _ := b.Counts(); bytes != 100 {
		t.Fatalf("bytes = %d, want 100", bytes)
	}
}

// An over-long line is stored cut to the per-line allowance and says so; a
// record beyond the per-record cap is dropped and leaves a marker row. Both
// reach the screen: nothing is clipped silently.
func TestBufferOversizeIsVisible(t *testing.T) {
	b := NewBuffer(4, 0)
	if !b.Push(rec(strings.Repeat("a", maxLineBytes+100))) {
		t.Fatal("an over-long line was dropped instead of truncated")
	}
	got := b.Lines()[0]
	if !got.Truncated || len(got.Content) != maxLineBytes || !strings.HasSuffix(got.Content, markerTruncated) {
		t.Fatalf("truncated=%v len=%d, want the %d-byte allowance ending in the marker", got.Truncated, len(got.Content), maxLineBytes)
	}
	if b.Push(rec(strings.Repeat("a", maxRecordBytes+1))) {
		t.Fatal("a record beyond the per-record cap was stored")
	}
	entries := b.Entries()
	if last := entries[len(entries)-1]; last.kind != markDropped {
		t.Fatalf("no dropped marker after the oversized record: %+v", last)
	}
	if _, _, _, _, dropped := b.Counts(); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
}

// Identical lines are legitimate output and are all kept.
func TestBufferKeepsDuplicates(t *testing.T) {
	b := NewBuffer(0, 0)
	for i := 0; i < 10; i++ {
		b.Push(rec("same line"))
	}
	if n := len(b.Lines()); n != 10 {
		t.Fatalf("kept %d of 10 identical lines", n)
	}
}

// Lines hands out a copy: a caller editing it cannot change the buffer.
func TestBufferLinesIsACopy(t *testing.T) {
	b := NewBuffer(4, 0)
	b.Push(rec("x"))
	b.Lines()[0].Content = "mutated"
	if got := b.Lines()[0].Content; got != "x" {
		t.Fatalf("the buffer changed to %q", got)
	}
}

// Content is sanitized once, when it is retained, so nothing later can put
// a control sequence on the screen. Safe whitespace and Unicode survive.
func TestBufferSanitizesOnPush(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"\x1b]0;pwned\x07normal \x1b[31mred\x1b[0m text", "normal red text"},
		{"col1\tcol2\nnext", "col1\tcol2\nnext"},
		{"日本語 log ünïcode ✓", "日本語 log ünïcode ✓"},
	} {
		b := NewBuffer(4, 0)
		b.Push(rec(c.in))
		if got := b.Lines()[0].Content; got != c.want {
			t.Errorf("Push(%q) stored %q, want %q", c.in, got, c.want)
		}
	}
	// A hostile pod name cannot smuggle bytes into the stream header.
	b := NewBuffer(4, 0)
	b.PushMarker(markOpen, "\x1b]0;pwned\x07pod", "main")
	e := b.Entries()[0]
	if got := markerText(e.kind, e.ctxPod, e.ctxCont); strings.ContainsRune(got, 0x1b) || !strings.Contains(got, "pod") {
		t.Fatalf("stream header = %q", got)
	}
}
