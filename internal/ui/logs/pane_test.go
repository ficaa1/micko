package logs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// The body fits the height the shell gave it, through every resize, and
// the scroll window is exactly the rows left for it. If the two disagree
// the shell clips rows the pane believes are on screen, and paging skips or
// repeats lines.
func TestBodyFitsTheHeightThroughResizes(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(100))
	for _, h := range []int{20, 8, 4, 12} {
		m.SetSize(80, h)
		if got := len(m.BodyLines()); got > h {
			t.Fatalf("height %d: the body is %d lines", h, got)
		}
		if got, want := len(m.window()), m.viewRows(); got != want {
			t.Fatalf("height %d: the window shows %d rows, viewRows says %d", h, got, want)
		}
	}
}

// A notice explains an empty stream, wrapped to the pane, and goes as soon
// as a line is retained. An error takes its place on the status line.
func TestNoticeOnlyWhileEmpty(t *testing.T) {
	m := NewModel(testRef(), "pod-1", "main")
	m.SetSize(40, 20)
	m.SetPhase(PhaseEnded)
	m.SetNotice("no log lines: the pods of this run are gone and nothing was archived")
	body := strings.Join(m.BodyLines(), "\n")
	if !strings.Contains(body, "no log lines: the pods of this run are") || !strings.Contains(body, "archived") {
		t.Fatalf("notice missing:\n%s", body)
	}
	for _, l := range m.BodyLines()[1:3] {
		if len(l) > 40 {
			t.Fatalf("notice not wrapped: %q", l)
		}
	}
	m.ApplyRecords([]core.LogRecord{podRec("pod-1", "hello")})
	if strings.Contains(strings.Join(m.BodyLines(), "\n"), "no log lines") {
		t.Fatal("the notice stayed after a line arrived")
	}

	e := NewModel(testRef(), "pod-1", "main")
	e.SetNotice("no log lines")
	e.SetError("stream failed")
	if strings.Contains(strings.Join(e.BodyLines(), "\n"), "no log lines") {
		t.Fatal("the notice was shown beside an error")
	}
}

// The pane's own annotations, the stream header and the retention count,
// are muted so the log lines read first. A skin changes colour only: the
// text is the plain pane's exactly, and a log line's text is never
// restyled.
func TestAnnotationsAreMutedAndLogLinesAreNot(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("solarized-dark", false)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.ApplyRecords(recs(3))
	want := strings.Join(m.BodyLines(), "\n")
	m.SetTheme(th)
	got := m.BodyLines()
	if plain := ansi.Strip(strings.Join(got, "\n")); plain != want {
		t.Fatalf("themed pane text differs:\n%s\n---\n%s", plain, want)
	}
	var marker, count, logLine string
	for _, l := range got {
		plain := ansi.Strip(l)
		switch {
		case strings.HasPrefix(plain, "──"):
			marker = l
		case strings.HasPrefix(plain, "retained:"):
			count = l
		case strings.Contains(plain, "line-1"):
			logLine = l
		}
	}
	if marker == "" || count == "" || logLine == "" {
		t.Fatalf("pane is missing a marker, the count or a log line:\n%s", want)
	}
	for name, l := range map[string]string{"marker": marker, "count": count} {
		if !strings.HasPrefix(l, th.Muted.Render(ansi.Strip(l))) {
			t.Errorf("%s is not muted: %q", name, l)
		}
	}
	if text := logLine[strings.Index(logLine, "line-1"):]; strings.Contains(text, "\x1b[") {
		t.Errorf("a log line was restyled: %q", logLine)
	}
}

// The body is sanitized all the way to the screen, coloured theme included:
// a hostile line cannot put a control byte in the pane.
func TestBodyCarriesNoControlBytes(t *testing.T) {
	m := colorModel(t)
	m.ApplyRecords([]core.LogRecord{podRec("pod-1", "ERROR\x1b]0;pwned\x07 and \x1b[2Jmore")})
	for _, l := range m.BodyLines() {
		if s := ansi.Strip(l); strings.ContainsRune(s, 0x1b) || strings.ContainsRune(s, 0x07) {
			t.Fatalf("control byte reached the pane: %q", l)
		}
	}
}
