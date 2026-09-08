package logs

import (
	"strconv"
	"testing"

	"argo-tui/internal/core"
)

// ---------------------------------------------------------------------------
// benchmark_test.go — the plan §8/§9 D1 benchmark target.
//
//	go test ./internal/ui/logs -bench BenchmarkBuffer -benchmem
//
// The sustained high-volume synthetic stream (LOG-14, PERF-04) must obey
// both caps; the bench pins push cost staying flat after eviction kicks
// in (no reallocation blowup at the steady state, allocations bounded by
// the retained window).
// ---------------------------------------------------------------------------

var benchSink bool

func BenchmarkBuffer(b *testing.B) {
	// Production caps: the ring fills to 10,000 lines and then cycles, so
	// the loop measures the steady state (post-eviction push).
	buf := NewBuffer(MaxLines, MaxBytes)
	payload := "benchmark line payload with some padding to be realistic "
	rec := core.LogRecord{PodName: "pod-1", Container: "main"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Content = payload + strconv.Itoa(i)
		ok := buf.Push(rec)
		benchSink = benchSink && ok
	}
}

func BenchmarkBufferOversizeTruncate(b *testing.B) {
	// Per-line truncation path: lines far beyond maxLineBytes exercise the
	// visible-marker cut on every push.
	buf := NewBuffer(MaxLines, MaxBytes)
	payload := make([]byte, maxLineBytes*4)
	for i := range payload {
		payload[i] = 'x'
	}
	rec := core.LogRecord{PodName: "pod-1", Container: "main"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Content = string(payload) + strconv.Itoa(i)
		ok := buf.Push(rec)
		benchSink = benchSink && ok
	}
}

func BenchmarkRenderWindow(b *testing.B) {
	// Render cost at a filled buffer: snapshot + window + join for the
	// standard 80×24 terminal (the keyboard-responsiveness budget).
	m := testModel(&testing.T{})
	recs := make([]core.LogRecord, 0, MaxLines)
	for i := 0; i < MaxLines; i++ {
		recs = append(recs, rec("steady-state line payload "+strconv.Itoa(i%1000)))
	}
	m.ApplyRecords(recs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
