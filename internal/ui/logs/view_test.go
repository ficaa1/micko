package logs

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// ---------------------------------------------------------------------------
// view tests — golden rendering and sanitization. These verify what the
// terminal receives for the standard terminal cases (control-sequence
// stripping, truncation markers).
// ---------------------------------------------------------------------------

// TestViewReachesTerminalWithoutControlSequences pins: hostile
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

// TestViewTruncationMarkerVisible pins the truncation marker in the rendered surface: an
// over-allowance line shows the visible truncation marker.
func TestViewTruncationMarkerVisible(t *testing.T) {
	m := testModel(t)
	long := strings.Repeat("y", maxLineBytes+50)
	m.ApplyRecords([]core.LogRecord{{PodName: "pod-1", Container: "main", Content: long, ReceivedAt: testkit.FixtureEpoch}})
	if v := m.View(); !strings.Contains(v, "truncated") {
		t.Fatal("truncation marker missing from the rendered view")
	}
}

// TestViewGoldenFollow pins the follow-mode render: header, status with
// scope + FOLLOWING, context marker with count, lines with their source
// label (workflow-wide logs label every line by default).
func TestViewGoldenFollow(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{rec("one"), rec("two")})
	got := m.View()
	want := strings.Join([]string{
		"logs: wf-1",
		"Scope: workflow-wide · container: main · [FOLLOWING]",
		"retained: 2/10000 lines (0 evicted)",
		"── (all pods):main ── 2 lines recorded",
		"pod-1            one",
		"pod-1            two",
		"t follow  space pause  / search  n next  & only matches  w wrap  L labels  c container  | pipe  esc back",
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
		"pod-1            one",
		"t follow  space pause  / search  n next  & only matches  w wrap  L labels  c container  | pipe  esc back",
	}, "\n")
	if got != want {
		t.Errorf("golden paused mismatch:\n got: %q\nwant: %q", got, want)
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
