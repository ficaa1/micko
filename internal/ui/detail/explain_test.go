package detail

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// explainModel is the Explain section of one demo workflow in the plain
// theme, with the first failing pod's log read from the demo backend when
// read is set.
func explainModel(t *testing.T, name string, w, h int, read bool) *Model {
	t.Helper()
	m := demoModel(t, name, w, h)
	if !m.SetSection("explain") {
		t.Fatal("no explain section")
	}
	if read {
		readDemoLog(t, m, 1)
	}
	return m
}

// readDemoLog answers the section's log request with the demo backend's
// log for that pod, as request id.
func readDemoLog(t *testing.T, m *Model, id uint64) {
	t.Helper()
	in, ok := m.ExplainLogWanted()
	if !ok {
		return
	}
	m.StartExplainLog(id, in)
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	var lines []string
	for _, rec := range r.PodLogs[in.PodName] {
		lines = append(lines, rec.Content)
	}
	if !m.SetExplainLog(id, lines, "", false) {
		t.Fatal("the section dropped the reply to its own request")
	}
}

var explainDemoNames = []string{
	"demo-nightly-report", "demo-oom-backfill", "demo-param-check", "demo-release-gate",
	"demo-train-pipeline", "demo-cleanup", "demo-deploy-multi-layer", "demo-etl-hourly-1790000000",
}

// Every line of the section stays inside the pane at every width, in the
// plain theme and a truecolor skin, the tab strip included.
func TestExplainLinesFitThePane(t *testing.T) {
	for _, name := range explainDemoNames {
		for _, w := range []int{136, 116, 76, 56, 36} {
			for _, skin := range []string{"plain", "nord"} {
				m := explainModel(t, name, w, 30, true)
				if skin != "plain" {
					th, _ := shared.SkinTheme(skin, false)
					m.SetTheme(th)
				}
				for i := 0; i < 3; i++ {
					for _, l := range m.BodyLines() {
						if cw := ansi.StringWidth(l); cw > w {
							t.Errorf("%s at %d (%s): line is %d cells: %q", name, w, skin, cw, ansi.Strip(l))
						}
					}
					m.handleKey("pgdown")
				}
			}
		}
	}
}

// The severity is a glyph and a word on every card, so it survives a
// terminal with no colour.
func TestExplainSeverityIsGlyphAndWord(t *testing.T) {
	want := map[string]string{
		"demo-nightly-report": "✗ ERROR  transform failed all 3 attempts",
		"demo-release-gate":   "◇ INFO  Waiting for a person at approve-production",
	}
	for name, head := range want {
		m := explainModel(t, name, 136, 40, true)
		if s := strings.Join(m.BodyLines(), "\n"); !strings.Contains(s, head) {
			t.Errorf("%s lacks %q:\n%s", name, head, s)
		}
	}
	b := newExplainRenderer(diagnose.Report{}, shared.NewTheme(true), 80, true, false)
	card := b.card(diagnose.Finding{Severity: diagnose.Warning, Headline: "slow", Next: "wait"})
	if !strings.HasPrefix(card[0], "▲ WARNING  slow") {
		t.Errorf("warning card starts %q", card[0])
	}
}

// Parameter values in the evidence follow the pane's reveal state: shown by
// default, hidden by v and by a profile that redacts, on the pane and in
// the copied report alike.
func TestExplainValuesFollowReveal(t *testing.T) {
	m := explainModel(t, "demo-oom-backfill", 136, 40, true)
	has := func(s string) bool {
		return strings.Contains(strings.Join(m.BodyLines(), "\n"), s) &&
			strings.Contains(strings.Join(m.RawLines(), "\n"), s)
	}
	if !has("memory=2Gi") {
		t.Fatal("values are not shown by default")
	}
	m.handleKey("v")
	if !has("memory=[REDACTED]") || has("memory=2Gi") {
		t.Fatal("v does not hide the values")
	}
	if !strings.Contains(m.explainStatusLine(), "values redacted (v reveals)") {
		t.Errorf("status line %q does not say values are hidden", m.explainStatusLine())
	}
	r := explainModel(t, "demo-oom-backfill", 136, 40, true)
	r.SetRedactByDefault(true)
	if strings.Contains(strings.Join(r.RawLines(), "\n"), "memory=2Gi") {
		t.Fatal("a redacting profile shows the values")
	}
}

// The section asks for its log only while it is on screen, says it is
// reading while the read runs, takes only the reply to its latest request,
// and asks again after a canceled read or for another workflow.
func TestExplainLogLifecycle(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 136, 40)
	if _, ok := m.ExplainLogWanted(); ok {
		t.Fatal("the nodes tab asked for the explain log")
	}
	m.handleKey("X")
	in, ok := m.ExplainLogWanted()
	if !ok || in.PodName != m.nodeMap[in.NodeID].PodName || m.nodeMap[in.NodeID].DisplayName != "transform(2)" ||
		in.TailLines != diagnose.LogTail || in.Container != "main" {
		t.Fatalf("wanted %+v, %v", in, ok)
	}
	body := func() string { return strings.Join(m.BodyLines(), "\n") }
	if !strings.Contains(body(), "reading the last 200 lines of transform(2)'s log…") {
		t.Errorf("before the read the card does not say it is coming:\n%s", body())
	}

	m.StartExplainLog(7, in)
	if _, ok := m.ExplainLogWanted(); ok {
		t.Fatal("asked again while its read runs")
	}
	if !strings.Contains(m.explainStatusLine(), "reading the log of transform(2)…") {
		t.Errorf("status %q does not say it is reading", m.explainStatusLine())
	}
	if m.SetExplainLog(6, []string{"ERROR stale"}, "", false) {
		t.Fatal("took the reply to an older request")
	}
	m.StopExplainLog()
	if _, ok := m.ExplainLogWanted(); !ok {
		t.Fatal("a canceled read is not asked for again")
	}
	m.StartExplainLog(8, in)
	if !m.SetExplainLog(8, []string{"loading", "ERROR column 'revenue_eur' has 312 nulls"}, "", false) {
		t.Fatal("dropped the reply to the latest request")
	}
	if !strings.Contains(body(), "> 2  ERROR column 'revenue_eur' has 312 nulls") {
		t.Errorf("the read line is not quoted:\n%s", body())
	}
	if m.SetExplainLog(8, []string{"again"}, "", false) {
		t.Fatal("a second reply to one request replaced the first")
	}
	if _, ok := m.ExplainLogWanted(); ok {
		t.Fatal("asked again after the read finished")
	}
	m.StopExplainLog()
	if _, ok := m.ExplainLogWanted(); ok {
		t.Fatal("stopping a finished read threw it away")
	}

	m.SetWorkflow(demoWorkflow(t, "demo-oom-backfill"), testkit.FixtureEpoch)
	if in2, ok := m.ExplainLogWanted(); !ok || in2.PodName == in.PodName {
		t.Fatalf("another workflow does not ask for its own log: %+v %v", in2, ok)
	}
	if m.SetExplainLog(8, []string{"late"}, "", false) {
		t.Fatal("a reply for the previous workflow was applied")
	}
}

// A log the server no longer has is said on the card and as a finding of
// its own.
func TestExplainGoneLog(t *testing.T) {
	m := explainModel(t, "demo-oom-backfill", 136, 40, false)
	in, _ := m.ExplainLogWanted()
	m.StartExplainLog(3, in)
	m.SetExplainLog(3, nil, `pods "x" not found`, true)
	s := strings.Join(m.BodyLines(), "\n")
	for _, want := range []string{"could not be read (see below)", "▲ WARNING  The log of backfill-2026-q2 is gone", `pods "x" not found`} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

// The report copied with y and shown by f is plain text: the workflow and
// its outcome, the counts, and every card, with no escape sequences in any
// skin.
func TestExplainRawReport(t *testing.T) {
	m := explainModel(t, "demo-nightly-report", 136, 40, true)
	th, _ := shared.SkinTheme("nord", false)
	m.SetTheme(th)
	raw := m.RawLines()
	if raw[0] != "Explain demo-nightly-report in demo: Failed after 4m00s" || raw[1] != "1 error · 2 info" || raw[2] != "" {
		t.Fatalf("report head:\n%s", strings.Join(raw[:3], "\n"))
	}
	joined := strings.Join(raw, "\n")
	if strings.Contains(joined, "\x1b") || strings.Contains(joined, "│") {
		t.Fatalf("the report has escapes or card rules:\n%s", joined)
	}
	for _, want := range []string{
		"✗ ERROR  transform failed all 3 attempts: exit code 1 each time",
		"  next      Retrying did not help",
		"◇ INFO  The exit handler ran and succeeded",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("report lacks %q:\n%s", want, joined)
		}
	}
	if _, ok := m.SelectedNode(); ok {
		t.Error("the section selects a node, so y would copy a pod name instead of the report")
	}
}

// The status line counts the findings and gives the position once the
// cards overflow; j and G scroll; the hints offer the failing log only
// when there is one.
func TestExplainStatusScrollAndHints(t *testing.T) {
	m := explainModel(t, "demo-nightly-report", 76, 12, true)
	if s := m.explainStatusLine(); !strings.HasPrefix(s, "explain · 1 error · 2 info · 1/") {
		t.Errorf("status %q", s)
	}
	m.handleKey("j")
	if m.ex.top != 1 {
		t.Errorf("j scrolled to %d", m.ex.top)
	}
	m.handleKey("G")
	lines := m.BodyLines()
	if last := lines[len(lines)-1]; !strings.Contains(last, "Nothing to do: it ran after the workflow failed.") {
		t.Errorf("G does not show the end: %q", last)
	}
	if h := m.Hints(); !strings.Contains(h, "y copy report") || !strings.Contains(h, "l failing log") {
		t.Errorf("hints %q", h)
	}
	g := explainModel(t, "demo-release-gate", 76, 12, true)
	if h := g.Hints(); strings.Contains(h, "l failing log") {
		t.Errorf("a workflow with no failure offers its log: %q", h)
	}
	if cmd := g.explainLogsCmd(); cmd != nil {
		t.Error("l opens a log with nothing failed")
	}
	if cmd := m.explainLogsCmd(); cmd == nil {
		t.Fatal("l does not open the failing pod's log")
	} else if intent, ok := cmd().(NodeLogsIntent); !ok || intent.Name != "transform(2)" || intent.PodName == "" {
		t.Errorf("l opens %+v", intent)
	}
}

// TestExplainGolden pins the section as a reader sees it on the pane of a
// 140- and an 80-column terminal, in the plain theme, with the log read.
func TestExplainGolden(t *testing.T) {
	for _, width := range []int{140, 80} {
		var b strings.Builder
		for _, name := range []string{
			"demo-nightly-report", "demo-oom-backfill", "demo-etl-hourly-1790000000", "demo-param-check",
			"demo-release-gate", "demo-train-pipeline", "demo-cleanup", "demo-deploy-multi-layer",
		} {
			m := explainModel(t, name, width-4, 36, true)
			b.WriteString("== " + name + "\n")
			b.WriteString(strings.Join(m.BodyLines(), "\n") + "\n")
		}
		compareGolden(t, filepath.Join("testdata", "explain-"+itoaDetail(width)+".golden"), b.String())
	}
}

// The tab strip fits the pane: whole when there is room, closed up when
// there is less, and a window around the active tab below that.
func TestTabStripFits(t *testing.T) {
	plain := shared.NewTheme(true)
	cases := []struct {
		active string
		width  int
		want   string
	}{
		{"explain", 80, " Summary   Nodes   Timeline  [Explain]  Events   Resource "},
		{"explain", 50, "Summary Nodes Timeline [Explain] Events Resource"},
		{"explain", 30, "… Timeline [Explain] Events …"},
		{"summary", 30, "[Summary] Nodes Timeline …"},
		{"resource", 24, "… Events [Resource]"},
		{"explain", 13, "… [Explain] …"},
		{"explain", 8, "… [Expl…"},
	}
	for _, tc := range cases {
		got := tabStripFit(tc.active, plain, tc.width)
		if got != tc.want {
			t.Errorf("tabStripFit(%s, %d) = %q, want %q", tc.active, tc.width, got, tc.want)
		}
		if cellWidth(got) > tc.width {
			t.Errorf("tabStripFit(%s, %d) is %d cells", tc.active, tc.width, cellWidth(got))
		}
	}
}
