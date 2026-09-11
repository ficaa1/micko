package logs

import (
	"strconv"
	"strings"
	"testing"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

// ---------------------------------------------------------------------------
// view tests — golden rendering and sanitization. These verify what the
// terminal receives for the acceptance-matrix terminal cases (LOG-05,
// SEC-01/02, LOG-04 truncation markers), plus the pinned disconnect/
// reconnect/ended surfaces (LOG-11/LOG-12).
// ---------------------------------------------------------------------------

// TestViewReachesTerminalWithoutControlSequences pins LOG-05/SEC-01: hostile
// log content is sanitized before the terminal surface sees it.
func TestViewReachesTerminalWithoutControlSequences(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{
		{PodName: "pod-1", Container: "main", Content: "\x1b]0;pwned\x07line\x1b[31m-red\x1b[0m\x07", ReceivedAt: testkit.FixtureEpoch},
	})
	v := m.View()
	if !strings.Contains(v, "line-red") {
		t.Fatalf("sanitized text missing from view: %q", v)
	}
	if strings.ContainsRune(v, 0x1b) || strings.ContainsRune(v, 0x07) {
		t.Fatal("control byte reached the terminal surface")
	}
}

// TestViewUnicodeSurvives pins that multibyte content renders intact.
func TestViewUnicodeSurvives(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("日本語 log ünïcode ✓")})
	v := m.View()
	if !strings.Contains(v, "日本語 log ünïcode ✓") {
		t.Fatalf("unicode content mangled: %q", v)
	}
}

// TestViewTruncationMarkerVisible pins LOG-04 in the rendered surface: an
// over-allowance line shows the visible truncation marker.
func TestViewTruncationMarkerVisible(t *testing.T) {
	m := testModel(t)
	long := strings.Repeat("y", maxLineBytes+50)
	m.ApplyRecords([]core.LogRecord{{PodName: "pod-1", Container: "main", Content: long, ReceivedAt: testkit.FixtureEpoch}})
	if v := m.View(); !strings.Contains(v, "truncated") {
		t.Fatal("truncation marker missing from the rendered view (LOG-04)")
	}
}

// TestViewGoldenFollow pins the follow-mode render: header, status with
// scope + FOLLOWING, context marker with count, lines.
func TestViewGoldenFollow(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("one"), rec("two")})
	got := m.View()
	want := strings.Join([]string{
		"logs: wf-1",
		"Scope: workflow-wide · container: main · [FOLLOWING]",
		"retained: 2/10000 lines (0 evicted)",
		"── (all pods):main ── 2 lines recorded",
		"one",
		"two",
		"t follow  space pause  / search  n next  c container  | pipe  f raw  y copy  esc back",
	}, "\n")
	if got != want {
		t.Errorf("golden follow mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestViewGoldenPaused pins the paused render: badge flips to PAUSED,
// content and marker unchanged.
func TestViewGoldenPaused(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("one")})
	press(m, ' ')
	got := m.View()
	want := strings.Join([]string{
		"logs: wf-1",
		"Scope: workflow-wide · container: main · [PAUSED]",
		"retained: 1/10000 lines (0 evicted)",
		"── (all pods):main ── 1 line recorded",
		"one",
		"t follow  space pause  / search  n next  c container  | pipe  f raw  y copy  esc back",
	}, "\n")
	if got != want {
		t.Errorf("golden paused mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestViewGoldenReconnect pins LOG-11 in the render: the reconnect gap
// marker with pinned wording between the pre/post lines.
func TestViewGoldenReconnect(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("before")})
	m.NewStream()
	m.ApplyRecords([]core.LogRecord{rec("after")})
	got := m.View()
	want := strings.Join([]string{
		"logs: wf-1",
		"Scope: workflow-wide · container: main · [FOLLOWING]",
		"retained: 2/10000 lines (0 evicted)",
		"── (all pods):main ── 2 lines recorded",
		"before",
		"── reconnect: new stream; overlap/gap possible ──",
		"after",
		"t follow  space pause  / search  n next  c container  | pipe  f raw  y copy  esc back",
	}, "\n")
	if got != want {
		t.Errorf("golden reconnect mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestViewGoldenEnded pins the ended lifecycle: badge ENDED plus the
// end-of-stream row.
func TestViewGoldenEnded(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("only")})
	m.ApplyMarker(markEnd, "pod-1", "main")
	m.SetPhase(PhaseEnded)
	got := m.View()
	want := strings.Join([]string{
		"logs: wf-1",
		"Scope: workflow-wide · container: main · [ENDED]",
		"retained: 1/10000 lines (0 evicted)",
		"── (all pods):main ── 1 line recorded",
		"only",
		"── stream ended ──",
		"t follow  space pause  / search  n next  c container  | pipe  f raw  y copy  esc back",
	}, "\n")
	if got != want {
		t.Errorf("golden ended mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestViewGoldenContextHeader pins the pod-scoped context header wording
// (empty pod renders "(all pods)"; a named pod renders its name).
func TestViewGoldenContextHeader(t *testing.T) {
	m := NewModel(testRef(), "mypod-abc", "sidecar")
	m.SetNoColor(true)
	m.SetSize(80, 24)
	m.ApplyRecords([]core.LogRecord{rec("x")})
	v := m.View()
	if !strings.Contains(v, "── mypod-abc:sidecar ──") || !strings.Contains(v, "pod: mypod-abc") {
		t.Fatalf("pod-scoped header missing:\n%s", v)
	}
}

// TestViewLongLinesDoNotBloatRender pins that a window of long-but-bounded
// lines renders in bounded text (the buffer truncated them at the cap).
func TestViewLongLinesDoNotBloatRender(t *testing.T) {
	m := testModel(t)
	for i := 0; i < 50; i++ {
		m.ApplyRecords([]core.LogRecord{rec(strings.Repeat("z", maxLineBytes*2))})
	}
	v := m.View()
	if len(v) > maxLineBytes*len(windowTexts(m))+4096 {
		t.Fatalf("render inflated: %d bytes for a %d-row window", len(v), len(windowTexts(m)))
	}
	if n := strings.Count(v, "truncated"); n == 0 {
		t.Fatal("expected visible truncation markers")
	}
	_ = strconv.Itoa // keep strconv imported for the arithmetic below
}
