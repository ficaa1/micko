package detail

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// explainModel is the Explain section of a demo workflow, with its failing log read when read is set.
func explainModel(t *testing.T, name string, w, h int, read bool) *Model {
	t.Helper()
	m := sectionModel(t, name, "explain", w, h)
	if read {
		readDemoLog(t, m, 1)
	}
	return m
}

// readDemoLog answers the section's log request, as request id, with the demo's log for that pod.
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

// The severity is a glyph and a word on every card.
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

// The section asks for its log only while on screen, and takes only the reply to its latest request.
func TestExplainLogLifecycle(t *testing.T) {
	m := demoModel(t, "demo-nightly-report", 136, 40)
	if _, ok := m.ExplainLogWanted(); ok {
		t.Fatal("the nodes tab asked for the explain log")
	}
	press(m, "X")
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

// A log the server no longer has is said on the card and as a finding of its own.
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

// The copied report is plain text in any skin: the outcome, the counts and every card.
func TestExplainRawReport(t *testing.T) {
	m := explainModel(t, "demo-nightly-report", 136, 40, true)
	m.SetTheme(skin(t))
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

// The status line counts the findings; j and G scroll; the hints offer the failing log when there is one.
func TestExplainStatusScrollAndHints(t *testing.T) {
	m := explainModel(t, "demo-nightly-report", 76, 12, true)
	if s := m.explainStatusLine(); !strings.HasPrefix(s, "explain · 1 error · 2 info · 1/") {
		t.Errorf("status %q", s)
	}
	press(m, "j")
	if m.ex.top != 1 {
		t.Errorf("j scrolled to %d", m.ex.top)
	}
	press(m, "G")
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

// failedDeploy is demo-deploy-multi-layer with the eu-west pod of three layers failed on its own.
func failedDeploy(t *testing.T) core.Workflow {
	t.Helper()
	wf := copyWorkflow(demoWorkflow(t, "demo-deploy-multi-layer"))
	wf.Summary.Phase = "Failed"
	for id, n := range wf.Nodes {
		if n.DisplayName != "apply-eu-west" {
			continue
		}
		for _, p := range n.Inputs.Parameters {
			if p.Name == "layer" && p.Value != "edge" {
				n.Phase, n.ExitCode, n.Message = "Failed", "1", "Error (exit code 1)"
				wf.Nodes[id] = n
			}
		}
	}
	return wf
}

// Every failure card quotes its own pod's log, and n and N pick the card whose log l opens.
func TestExplainPicksAFailure(t *testing.T) {
	for _, h := range []int{12, 24, 60} {
		t.Run(strconv.Itoa(h), func(t *testing.T) {
			m := workflowModel(failedDeploy(t), 136, h)
			m.SetSection("explain")
			var pods []string
			for id := uint64(1); ; id++ {
				in, ok := m.ExplainLogWanted()
				if !ok {
					break
				}
				m.StartExplainLog(id, in)
				m.SetExplainLog(id, []string{"ERROR in " + in.PodName}, "", false)
				pods = append(pods, in.PodName)
			}
			if len(pods) != 3 {
				t.Fatalf("read %d logs, want 3: %v", len(pods), pods)
			}
			all := strings.Join(m.explainLines(), "\n")
			for _, p := range pods {
				if !strings.Contains(all, "> 1  ERROR in "+p) {
					t.Errorf("no card quotes the log of %s:\n%s", p, all)
				}
			}
			if h := m.Hints(); !strings.Contains(h, "n/N pick failure") || !strings.Contains(h, "l its log") {
				t.Errorf("hints %q", h)
			}

			opens := func() string {
				t.Helper()
				intent, _ := press(m, "l")().(NodeLogsIntent)
				return intent.PodName
			}
			for i, key := range []string{"", "n", "n", "n", "N"} {
				if key != "" {
					press(m, key)
				}
				want := pods[[]int{0, 1, 2, 0, 2}[i]]
				if got := opens(); got != want {
					t.Fatalf("after %q l opens %s, want %s", key, got, want)
				}
				picked := ""
				for _, l := range m.BodyLines() {
					if strings.HasPrefix(l, "┃ pod") {
						picked = l
					}
				}
				if !strings.Contains(picked, want) {
					t.Fatalf("after %q the marked card on screen is %q, want the one for %s:\n%s", key, picked, want, body(m))
				}
			}
		})
	}
}

// The Explain section at 140 and 80 columns, with the log read.
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
		golden(t, "explain-"+strconv.Itoa(width), b.String())
	}
}
