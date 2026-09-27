package logs

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// colorModel is the standard test model with the coloured theme, for the
// tests that check a style was applied and where.
func colorModel(t *testing.T) *Model {
	t.Helper()
	os.Unsetenv("NO_COLOR")
	m := NewModel(testRef(), "", "main")
	m.SetTheme(shared.NewTheme(false))
	m.SetSize(80, 24)
	return m
}

// podRec is one record from the named pod.
func podRec(pod, content string) core.LogRecord {
	return core.LogRecord{PodName: pod, Container: "main", Content: content, ReceivedAt: testkit.FixtureEpoch}
}

// The level word is found after up to two timestamps, bracketed or
// followed by a colon, in capitals or decorated; a JSON line's level or
// severity field counts too. Only the word's own range is reported.
func TestLevelSpanDetection(t *testing.T) {
	cases := []struct {
		line string
		want string // the styled word, "" for none
		lvl  level
	}{
		{"ERROR column has nulls", "ERROR", levelError},
		{"ERR short form", "ERR", levelError},
		{"FATAL out of memory", "FATAL", levelError},
		{"PANIC: runtime error", "PANIC", levelError},
		{"WARN heap at 95%", "WARN", levelWarn},
		{"WARNING: deprecated flag", "WARNING", levelWarn},
		{"DEBUG cache hit", "DEBUG", levelDebug},
		{"TRACE enter()", "TRACE", levelDebug},
		{"[ERROR] bracketed", "ERROR", levelError},
		{"[warn] lowercase in brackets", "warn", levelWarn},
		{"error: lowercase with a colon", "error", levelError},
		{"<debug> angle brackets", "debug", levelDebug},
		{"2026-09-24T16:02:00Z ERROR after the program's timestamp", "ERROR", levelError},
		{"2026-09-24T16:02:01.108894494Z 2026-09-24T16:02:00Z ERROR after two", "ERROR", levelError},
		{"2026-09-24 16:02:00,123 WARNING python logging", "WARNING", levelWarn},
		{"[2026-09-24 16:02:00] ERROR bracketed date", "ERROR", levelError},
		{"16:02:00 DEBUG time only", "DEBUG", levelDebug},
		{`{"level":"error","msg":"boom"}`, "error", levelError},
		{`{"ts":1,"severity":"WARNING","msg":"x"}`, "WARNING", levelWarn},
		{`2026-09-24T16:02:00Z {"msg":"x", "Level" : "debug"}`, "debug", levelDebug},
		// False positives the rule has to reject.
		{"errors: 0", "", levelNone},
		{"found 3 errors", "", levelNone},
		{"Error rate is fine", "", levelNone},
		{"error connecting, retrying", "", levelNone},
		{"trace id=4f1c2e9 accepted", "", levelNone},
		{"INFO all good ERROR later in the line", "", levelNone},
		{"2026-09-24T16:02:00Z 2026-09-24T16:02:00Z 2026-09-24T16:02:00Z ERROR three stamps", "", levelNone},
		{`{"level":"info","msg":"ERROR in message"}`, "", levelNone},
		{`{"msg":"no level"}`, "", levelNone},
		{"", "", levelNone},
		{"   ", "", levelNone},
	}
	for _, c := range cases {
		s, e, l, ok := levelSpan(c.line)
		got := ""
		if ok {
			got = c.line[s:e]
		}
		if got != c.want || l != c.lvl {
			t.Errorf("levelSpan(%q) = %q (%v), want %q (%v)", c.line, got, l, c.want, c.lvl)
		}
	}
}

// Only the level word is styled. The text before and after it reaches the
// terminal exactly as retained, so a reader's copy of the rest is intact.
func TestLevelStylingTouchesOnlyTheWord(t *testing.T) {
	m := colorModel(t)
	line := "2026-09-24T16:02:00Z ERROR column 'revenue_eur' has 312 nulls"
	m.ApplyRecords([]core.LogRecord{podRec("pod-1", line)})
	m.labels = false
	got := m.windowLines()
	last := got[len(got)-1]
	want := "2026-09-24T16:02:00Z " + m.theme.ErrorText.Render("ERROR") + " column 'revenue_eur' has 312 nulls"
	if last != want {
		t.Fatalf("styled line\n got %q\nwant %q", last, want)
	}
	if ansi.Strip(last) != line {
		t.Fatalf("the styling changed the text: %q", ansi.Strip(last))
	}
}

// A hostile level word cannot smuggle control bytes: the content was
// sanitized when it was retained, before any styling.
func TestLevelStylingRendersSanitizedText(t *testing.T) {
	m := colorModel(t)
	m.ApplyRecords([]core.LogRecord{podRec("pod-1", "ERROR\x1b]0;pwned\x07 and \x1b[2Jmore")})
	for _, l := range m.BodyLines() {
		stripped := ansi.Strip(l)
		if strings.ContainsRune(stripped, 0x1b) || strings.ContainsRune(stripped, 0x07) {
			t.Fatalf("control byte reached the rendered line: %q", l)
		}
	}
}

// A line is labelled by the step that ran its pod, or by its pod name
// without the workflow's name in front, clipped to the label column.
func TestSourceLabels(t *testing.T) {
	m := testModel(t)
	m.SetSources(map[string]string{"wf-1-transform-2104578372": "transform(0)"})
	for _, c := range []struct{ pod, want string }{
		{"wf-1-transform-2104578372", "transform(0)"},
		{"wf-1-extract-111", "extract-111"},
		{"other-pod-name", "other-pod-name"},
		{"wf-1-a-very-long-template-name-999", "a-very-long-tem…"},
		{"", ""},
	} {
		if got := m.sourceLabel(c.pod); got != c.want {
			t.Errorf("sourceLabel(%q) = %q, want %q", c.pod, got, c.want)
		}
	}
	// A node name from the server is sanitized like any other text.
	m.SetSources(map[string]string{"p": "evil\x1b[31mname"})
	if got := m.sourceLabel("p"); strings.ContainsRune(got, 0x1b) {
		t.Fatalf("label carries an escape: %q", got)
	}
}

// A pod's colour depends on its name alone: the same in two panes, in any
// arrival order, and different pods spread over the palette.
func TestSourceLabelColoursAreStable(t *testing.T) {
	pods := []string{"wf-1-extract-1", "wf-1-transform-2", "wf-1-transform-3", "wf-1-load-4", "wf-1-notify-5", "wf-1-x-6"}
	a, b := colorModel(t), colorModel(t)
	seen := map[string]bool{}
	for i, p := range pods {
		q := pods[len(pods)-1-i]
		if a.labelCell(q) != b.labelCell(q) {
			t.Fatalf("pod %s rendered differently in two panes", q)
		}
		seen[a.labelStyle(p).Render("x")] = true
	}
	if len(seen) < 3 {
		t.Fatalf("six pods used %d colours; the palette is not spread", len(seen))
	}
	for _, p := range pods {
		if a.labelStyle(p).Render("x") == a.theme.PhaseFailed.Render("x") {
			t.Fatalf("pod %s got the failure red", p)
		}
	}
}

// Labels are on for workflow-wide logs and off for one pod's log, and L
// toggles them. The label column is fixed, so text starts in one column.
func TestLabelsDefaultAndToggle(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{podRec("wf-1-a-1", "alpha"), podRec("wf-1-bb-2", "beta")})
	lines := m.windowLines()
	if lines[1] != "a-1              alpha" || lines[2] != "bb-2             beta" {
		t.Fatalf("labelled lines:\n%q", lines)
	}
	press(m, 'L')
	if got := m.windowLines()[1]; got != "alpha" {
		t.Fatalf("L did not hide the labels: %q", got)
	}
	pod := NewModel(testRef(), "wf-1-a-1", "main")
	pod.SetNoColor(true)
	pod.SetSize(80, 24)
	pod.ApplyRecords([]core.LogRecord{podRec("wf-1-a-1", "alpha")})
	if got := pod.windowLines()[1]; got != "alpha" {
		t.Fatalf("a pod-scoped pane starts labelled: %q", got)
	}
}

// Wrapping cuts between runes by cell width; a wide rune never straddles
// two lines.
func TestWrapBreaks(t *testing.T) {
	if got := wrapBreaks("abcdefghij", 4); len(got) != 3 || got[1] != 4 || got[2] != 8 {
		t.Fatalf("ascii breaks = %v", got)
	}
	if got := wrapBreaks("abc", 4); len(got) != 1 {
		t.Fatalf("a short line broke: %v", got)
	}
	// Each CJK rune is two cells and three bytes.
	if got := wrapBreaks("日本語テキスト", 5); len(got) != 4 || got[1] != 6 || got[2] != 12 {
		t.Fatalf("wide-rune breaks = %v", got)
	}
}

// renderRange styles each span inside the range only, so a highlight cut
// by a wrap is closed on the first line and reopened on the next.
func TestRenderRangeClosesStylesAtTheBreak(t *testing.T) {
	m := colorModel(t)
	style := m.theme.Selected
	text := "aaaaMATCHbbbb"
	spans := []span{{4, 9, style}}
	first := renderRange(text, 0, 6, spans)
	second := renderRange(text, 6, len(text), spans)
	if first != "aaaa"+style.Render("MA") || second != style.Render("TCH")+"bbbb" {
		t.Fatalf("split highlight:\n %q\n %q", first, second)
	}
}

// The search highlight survives wrapping: the current match is still
// reversed when it lands across the break.
func TestSearchHighlightSurvivesWrapping(t *testing.T) {
	m := colorModel(t)
	m.labels = false
	m.SetSize(20, 24)
	m.ApplyRecords([]core.LogRecord{podRec("p", strings.Repeat("x", 16)+"NEEDLE"+strings.Repeat("y", 10))})
	press(m, '/')
	typeInto(m, "needle")
	pressKey(m, keyEnter)
	press(m, 'w')
	lines := m.windowLines()
	joined := strings.Join(lines, "\n")
	sel := m.theme.Selected
	if !strings.Contains(joined, sel.Render("NEED")) || !strings.Contains(joined, sel.Render("LE")) {
		t.Fatalf("the highlight did not follow the wrap:\n%q", lines)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 20 && !strings.HasPrefix(ansi.Strip(l), "──") {
			t.Fatalf("a wrapped line is %d cells: %q", w, l)
		}
	}
}

// ctrl+t in the pane: the key is reported as "ctrl+t".
func ctrlT() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl} }
