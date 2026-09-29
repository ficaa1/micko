package detail

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// pane is everything the shell draws for the route: title, body, hints and status cell.
func pane(m *Model) string {
	return m.PaneTitle() + "\n" + strings.Join(m.BodyLines(), "\n") + "\n" + m.Hints() + "\n" + m.PaneStatus()
}

// resourceFixture is a workflow whose resource holds secret-shaped values and an unknown field.
func resourceFixture() core.Workflow {
	wf := testkit.SyntheticWorkflow("ns", "redact-wf", "Succeeded", testkit.FixtureEpoch)
	wf.Resource = []byte(`{
	  "metadata": {"name": "redact-wf", "namespace": "ns",
	    "annotations": {"workflows.argoproj.io/pod-name-format": "v2"}},
	  "spec": {
	    "arguments": {"parameters": [{"name": "api-key", "value": "sk-1234567890abcdef123456"}]},
	    "entrypoint": "main"
	  },
	  "status": {
	    "phase": "Succeeded",
	    "nodes": {"n1": {"outputs": {"parameters": [{"name": "password", "value": "hunter2-secret"}]}}},
	    "unknownFutureField": {"nested": [1, 2, {"deep": true}]}
	  }
	}`)
	return wf
}

// The pane in every state the root can put it in, and the summary and resource sections.
func TestPaneGolden(t *testing.T) {
	failed := demoWorkflow(t, "demo-nightly-report")
	failed.Summary.Labels = map[string]string{"team": "data", "schedule": "nightly"}
	var b strings.Builder
	for _, c := range []struct {
		name  string
		model func() *Model
	}{
		{"nothing loaded", func() *Model { m := New(); m.SetSize(80, 12); return m }},
		{"loading", func() *Model { m := workflowModel(failed, 80, 12); m.SetLoading(); return m }},
		{"error", func() *Model { m := workflowModel(failed, 80, 12); m.SetError("forbidden: RBAC"); return m }},
		{"not found", func() *Model { m := workflowModel(failed, 80, 12); m.SetNotFound(); return m }},
		{"summary", func() *Model { return workflowModel(failed, 80, 12) }},
		{"summary, suspended", func() *Model { return workflowModel(demoWorkflow(t, "demo-release-gate"), 80, 12) }},
		{"summary, archived", func() *Model { m := workflowModel(failed, 80, 12); m.SetArchived(true); return m }},
		{"resource", func() *Model {
			m := workflowModel(resourceFixture(), 80, 12)
			m.SetSection("resource")
			return m
		}},
		{"nodes, offloaded", func() *Model {
			m := workflowModel(testkit.FixtureOffloadedWorkflow("ns", "fixture-offloaded"), 80, 12)
			m.SetSection("nodes")
			return m
		}},
		{"nodes, not started", func() *Model {
			m := workflowModel(testkit.SyntheticWorkflow("ns", "pending", "Pending", testkit.FixtureEpoch), 80, 12)
			m.SetSection("nodes")
			return m
		}},
	} {
		b.WriteString("== " + c.name + "\n" + pane(c.model()) + "\n")
	}
	golden(t, "pane", b.String())
}

// The digits, the section letters, tab and shift+tab pick the section; other keys leave it alone.
func TestSectionKeys(t *testing.T) {
	m := workflowModel(demoWorkflow(t, "demo-nightly-report"), 80, 20)
	for _, c := range []struct{ key, want string }{
		{"2", "nodes"}, {"3", "timeline"}, {"4", "explain"}, {"5", "events"}, {"6", "resource"}, {"1", "summary"},
		{"T", "timeline"}, {"X", "explain"}, {"E", "events"},
		{"9", "events"}, {"0", "events"}, {"t", "events"}, {"x", "events"},
		{"tab", "resource"}, {"tab", "summary"}, {"tab", "nodes"},
		{"shift+tab", "summary"}, {"shift+tab", "resource"},
	} {
		press(m, c.key)
		if m.Section() != c.want {
			t.Fatalf("%s: section %q, want %q", c.key, m.Section(), c.want)
		}
	}
	if m.SetSection("bogus") || m.Section() != "resource" {
		t.Fatalf("SetSection(bogus) changed the section to %q", m.Section())
	}
}

// The tab strip fits the pane, closing up and then windowing around the active tab.
func TestTabStripFits(t *testing.T) {
	plain := shared.NewTheme(true)
	for _, c := range []struct {
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
	} {
		if got := tabStripFit(c.active, plain, c.width); got != c.want {
			t.Errorf("tabStripFit(%s, %d) = %q, want %q", c.active, c.width, got, c.want)
		}
	}
}

// Every section keeps to the pane at every size; the shell cuts the summary's and resource's long lines.
func TestEverySectionFitsThePane(t *testing.T) {
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	th := skin(t)
	for ref, wf := range r.Workflows {
		for _, section := range []string{"summary", "nodes", "timeline", "explain", "events", "resource"} {
			for _, theme := range []shared.Theme{shared.NewTheme(true), th} {
				m := workflowModel(wf, 80, 24)
				m.SetTheme(theme)
				m.SetSection(section)
				m.ApplyEvents(r.Events)
				m.SetEventsStatus("live", false)
				if section == "explain" {
					readDemoLog(t, m, 1)
				}
				for _, w := range []int{160, 136, 120, 76, 56, 40} {
					for _, h := range []int{3, 30} {
						m.SetSize(w, h)
						for step := 0; step < 2; step++ {
							lines := m.BodyLines()
							if len(lines) > h {
								t.Fatalf("%s %s %dx%d (%s): %d lines", ref.Name, section, w, h, theme.Skin, len(lines))
							}
							for _, l := range lines {
								if cw := ansi.StringWidth(l); cw > w && section != "summary" && section != "resource" {
									t.Fatalf("%s %s %dx%d (%s): a %d-cell line: %q", ref.Name, section, w, h, theme.Skin, cw, ansi.Strip(l))
								}
							}
							press(m, "i")
							press(m, "pgdown")
						}
					}
				}
			}
		}
	}
}

// Values follow one reveal state everywhere: v flips it for the workflow, another starts from the profile.
func TestReveal(t *testing.T) {
	for _, c := range []struct {
		name          string
		wf            func() core.Workflow
		open          func(t *testing.T, m *Model)
		value, hidden string
	}{
		{"resource", resourceFixture, func(t *testing.T, m *Model) { m.SetSection("resource") },
			"sk-1234567890abcdef123456", redactedMarker},
		{"info panel", func() core.Workflow { return demoWorkflow(t, "demo-nightly-report") }, func(t *testing.T, m *Model) {
			m.SetSection("nodes")
			cursorTo(t, m, "extract")
			m.showInfo = true
		}, "date = 2026-09-07", "date = " + redactedMarker},
		{"explanation", func() core.Workflow { return demoWorkflow(t, "demo-oom-backfill") }, func(t *testing.T, m *Model) {
			m.SetSection("explain")
			readDemoLog(t, m, 1)
		}, "memory=2Gi", "memory=" + redactedMarker},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := workflowModel(c.wf(), 160, 60)
			shows := func() bool {
				c.open(t, m)
				s := body(m) + "\n" + strings.Join(m.RawLines(), "\n")
				if strings.Contains(s, c.value) == strings.Contains(s, c.hidden) {
					t.Fatalf("both or neither of %q and %q:\n%s", c.value, c.hidden, s)
				}
				return strings.Contains(s, c.value)
			}
			other := func() {
				wf := c.wf()
				wf.Summary.Ref.UID = "another-run"
				m.SetWorkflow(wf, testkit.FixtureEpoch)
			}
			for i, step := range []struct {
				do   func()
				want bool
			}{
				{func() {}, true},
				{func() { press(m, "v") }, false},
				{func() { m.SetWorkflow(c.wf(), testkit.FixtureEpoch) }, false},
				{other, true},
				{func() { m.SetRedactByDefault(true) }, false},
				{func() { press(m, "v") }, true},
				{func() { m.SetWorkflow(c.wf(), testkit.FixtureEpoch) }, false},
				{func() { m.SetRedactByDefault(false) }, true},
			} {
				step.do()
				if got := shows(); got != step.want {
					t.Fatalf("step %d: values shown %v, want %v", i, got, step.want)
				}
			}
		})
	}
}

// The resource tab keeps unknown fields and explains a payload it cannot show.
func TestResourceTab(t *testing.T) {
	for _, c := range []struct {
		name     string
		resource string
		want     []string
		notWant  []string
	}{
		{"unknown fields", string(resourceFixture().Resource), []string{"unknownFutureField", "deep", "pod-name-format", "entrypoint"}, nil},
		{"none", "", []string{"(no resource payload returned by the server)"}, nil},
		{"invalid JSON", "{not-json}", []string{"not valid JSON"}, []string{"{not-json}"}},
	} {
		wf := testkit.SyntheticWorkflow("ns", "wf", "Running", testkit.FixtureEpoch)
		wf.Resource = []byte(c.resource)
		m := workflowModel(wf, 80, 40)
		m.SetSection("resource")
		raw := strings.Join(m.RawLines(), "\n")
		for _, w := range c.want {
			if !strings.Contains(raw, w) {
				t.Errorf("%s: lacks %q:\n%s", c.name, w, raw)
			}
		}
		for _, w := range c.notWant {
			if strings.Contains(raw, w) {
				t.Errorf("%s: shows %q:\n%s", c.name, w, raw)
			}
		}
	}
}

// Server text reaches the terminal without control sequences, and a node message stays on its row.
func TestServerTextIsSanitized(t *testing.T) {
	wf := newTreeFixture("wf").
		node("wf", "wf", "wf", "DAG", "Failed", "", 0, "a").
		node("a", "wf.a", "a", "Pod", "Failed", "wf", 0).
		with("a", func(n *core.Node) { n.Message = "one\ntwo\tthree\r\nfour" }).
		workflow()
	wf.Summary.Phase = "Failed"
	wf.Summary.Message = "boom \x1b[31mor owned\x1b[0m"
	wf.Summary.Labels = map[string]string{"evil": "x\x07y"}
	wf.Resource = []byte(`{"metadata":{"name":"n","x":"a\u0007b"},"status":{"message":"esc\u001b[2Jhere"}}`)
	m := workflowModel(wf, 120, 30)
	var all []string
	for _, section := range []string{"summary", "nodes", "resource"} {
		m.SetSection(section)
		all = append(all, m.BodyLines()...)
	}
	m.SetError("denied \x1b]0;pwned\x07")
	all = append(all, m.BodyLines()...)
	s := strings.Join(all, "\n")
	if strings.ContainsAny(s, "\x1b\x07") {
		t.Fatalf("a control byte reached the pane: %q", s)
	}
	for _, want := range []string{"or owned", "one two three four", "denied"} {
		if !strings.Contains(s, want) {
			t.Errorf("text lost: %q:\n%s", want, s)
		}
	}
}

// A skin styles guides, the selected row and the active tab, and draws the plain theme's text.
func TestSkinStyles(t *testing.T) {
	th := skin(t)
	plain := shared.NewTheme(true)
	for _, name := range demoNames() {
		m := demoModel(t, name, 70, 40)
		press(m, "h")
		for i, r := range m.nodes {
			got := m.renderer(70, th).render(r, false, false)
			if want := m.renderer(70, plain).render(r, false, false); ansi.Strip(got) != want {
				t.Fatalf("%s: themed row text differs:\n%q\n%q", name, ansi.Strip(got), want)
			}
			if r.Prefix != "" && r.Section == "" && !r.HasChildren && !strings.HasPrefix(got, th.TreeGuide.Render(r.Indent)) {
				t.Errorf("%s: connector %q is not drawn as a guide: %q", name, r.Indent, got)
			}
			if i == 0 {
				sel := m.renderer(100, th).render(r, true, false)
				if w := ansi.StringWidth(sel); w != 100 || !strings.HasPrefix(sel, sgrOf(th.Selected)) {
					t.Errorf("%s: selected row is %d cells, not a bar in the selection style: %q", name, w, sel)
				}
			}
		}
	}
	got := tabStrip("nodes", th)
	if !strings.Contains(got, th.TabActive.Render("[Nodes]")) || !strings.Contains(got, th.TabInactive.Render(" Summary ")) {
		t.Errorf("tab strip styles: %q", got)
	}
}

// sgrOf is the escape sequence a style opens with.
func sgrOf(s interface{ Render(...string) string }) string {
	out := s.Render("x")
	return out[:strings.Index(out, "x")]
}
