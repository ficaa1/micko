package logs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// The pane's own annotations, the stream marker and the retention count,
// are muted so the log lines read first. The text a log line carries is
// never restyled, and the pane's text is the plain pane's exactly. The
// source label in front of a line is the pane's, coloured by pod, and is
// left out of the check.
func TestAnnotationsAreMutedAndLogLinesAreNot(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	th, err := shared.SkinTheme("solarized-dark", false)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.ApplyRecords(recs(3))
	want := m.View()
	m.SetTheme(th)
	got := m.View()
	if ansi.Strip(got) != want {
		t.Fatalf("themed pane text differs:\n%s\n---\n%s", ansi.Strip(got), want)
	}
	var marker, count, logLine string
	for _, l := range strings.Split(got, "\n") {
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
		t.Fatalf("pane is missing a marker, the count or a log line:\n%s", ansi.Strip(got))
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
