package logs

import (
	"sync"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// buffer.go — bounded memory representation for retained log entries
// (plan §8 D1; acceptance LOG-06: retain at most 10,000 lines and 8 MiB of
// text, whichever limit is hit first; enforce single-record/frame size
// bounds and visible truncation markers; LOG-14: sustained streams keep
// memory settling, never growing unboundedly).
//
// Caps are enforced per push: the oldest entries evict first (ring
// semantics). A record beyond maxRecordBytes is dropped outright and a
// visible marker records it; a line beyond maxLineBytes is truncated with
// a visible marker. Nothing is ever silently clipped.
//
// Duplicates are legitimate log output: no deduplication ever happens
// (LOG-06/LOG-11 gate).
//
// buffer is unexported: the model talks to it through
// Push/PushMarker/ApplyMarker/Counts/Lines/Search.

// Frozen caps (plan §5; docs/development.md LOG-06).
const (
	// MaxLines caps the number of retained entries (lines + markers).
	MaxLines = 10_000
	// MaxBytes caps retained text bytes (log content only).
	MaxBytes = 8 << 20 // 8 MiB
	// maxRecordBytes bounds the largest single storable record; larger
	// records are dropped outright with a visible dropped marker (LOG-04
	// bounded oversize handling).
	maxRecordBytes = 1 << 20 // 1 MiB
	// maxLineBytes is the per-line storage allowance; longer lines are
	// truncated and carry a visible marker.
	maxLineBytes = 16 << 10 // 16 KiB
)

// markerTruncated is the visible suffix appended to an oversized line
// (never a silent clip, plan §5/LOG-04).
const markerTruncated = " …[truncated]"

// buffer is the in-memory ring of retained entries (log lines + markers).
type buffer struct {
	buf []entry // retained entries in arrival order

	bytes     int // text bytes across retained log lines only
	pushed    int64
	evicted   int64
	dropped   int64
	reconnect bool // a reconnect marker is pending (gap annotation)

	maxLines, maxBytes int

	mu sync.Mutex
}

// NewBuffer creates a ring bounded by both caps. Passing 0 for a cap means
// "no cap" — that mode exists for tests/demo only; production constructors
// always set both (NewModel uses MaxLines/MaxBytes).
func NewBuffer(maxLines, maxBytes int) *buffer {
	return &buffer{maxLines: maxLines, maxBytes: maxBytes}
}

// Push appends one record after sanitizing and bounding its content.
// Returns false only when the record was dropped outright (beyond the
// per-record cap); per-line truncation still stores the line visibly.
func (b *buffer) Push(r core.LogRecord) bool {
	content := shared.Sanitize(r.Content)
	if len(content) > maxRecordBytes {
		// Beyond even a bounded store: drop, but leave a visible trace.
		b.mu.Lock()
		b.dropped++
		b.mu.Unlock()
		b.PushMarker(markDropped, r.PodName, r.Container)
		return false
	}
	truncated := false
	if len(content) > maxLineBytes {
		content = content[:maxLineBytes-len(markerTruncated)] + markerTruncated
		truncated = true
	}
	b.mu.Lock()
	b.pushed++
	b.pushEntry(entry{kind: logEntryKind, line: logLine{
		Content:   content,
		PodName:   r.PodName,
		Container: r.Container,
		Truncated: truncated,
	}})
	b.mu.Unlock()
	return true
}

// PushMarker appends a stream-context annotation row. Marker context is
// sanitized here so no caller can later render it raw (SEC-01).
func (b *buffer) PushMarker(kind markerKind, podName, container string) {
	b.mu.Lock()
	b.pushEntry(entry{kind: kind, ctxPod: shared.Sanitize(podName), ctxCont: shared.Sanitize(container)})
	b.mu.Unlock()
}

// SetReconnect sets (or clears) the pending-gap flag: the next push
// annotates a reconnect marker first (LOG-11). The root calls this on a
// fresh stream attempt for the same buffer.
func (b *buffer) SetReconnect() {
	b.mu.Lock()
	b.reconnect = true
	b.mu.Unlock()
}

// pushEntry appends e and evicts oldest-first while a cap is exceeded.
// Markers do not count toward the byte cap (they are UI authoring) but do
// count toward the line cap so the retained window stays bounded.
// Caller holds b.mu.
func (b *buffer) pushEntry(e entry) {
	// A pending reconnect annotation is emitted before the entry that
	// triggered it (the gap precedes the new stream's lines).
	if b.reconnect && e.kind == logEntryKind {
		b.reconnect = false
		b.pushEntryLocked(entry{kind: markReconnect})
	}
	b.buf = append(b.buf, e)
	if e.kind == logEntryKind {
		b.bytes += len(e.line.Content)
	}
	for (b.maxLines > 0 && len(b.buf) > b.maxLines) ||
		(b.maxBytes > 0 && b.bytes > b.maxBytes) {
		front := b.buf[0]
		b.buf = b.buf[1:]
		if front.kind == logEntryKind {
			b.bytes -= len(front.line.Content)
		}
		b.evicted++
		if len(b.buf) == 0 {
			// Cap smaller than one entry: keep the newest rather than
			// looping forever; it still counts evicted so the operator
			// can see the cap is degenerate.
			break
		}
	}
}

// pushEntryLocked is pushEntry without the reconnect preamble (used by the
// preamble itself to avoid recursion on the marker). Caller holds b.mu.
func (b *buffer) pushEntryLocked(e entry) { b.pushEntry(e) }

// Counts snapshots the buffer counters under one lock.
func (b *buffer) Counts() (lines, bytes int, pushed, evicted, dropped int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf), b.bytes, b.pushed, b.evicted, b.dropped
}

// Entries returns a snapshot copy of all retained entries in order.
// Mutating the result never affects the buffer (value copies; strings are
// immutable). The view renders from this snapshot only.
func (b *buffer) Entries() []entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]entry, len(b.buf))
	copy(out, b.buf)
	return out
}

// Lines returns a snapshot copy of the retained log lines (markers
// skipped) — the search scope (LOG-09: retained buffer only).
func (b *buffer) Lines() []logLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]logLine, 0, len(b.buf))
	for _, e := range b.buf {
		if e.kind == logEntryKind {
			out = append(out, e.line)
		}
	}
	return out
}

// Search runs s over the retained log lines only (LOG-09). Indices point
// into Entries()-ordered log lines (the same order Lines() returns).
func (b *buffer) Search(s searchState) []int {
	return matchIndices(b.Lines(), s.term, s.caseSensitive)
}
