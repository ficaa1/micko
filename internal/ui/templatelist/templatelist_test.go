package templatelist

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/shared"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func release() core.WorkflowTemplate {
	return core.WorkflowTemplate{
		Namespace: "ci", Name: "release", CreatedAt: now.Add(-90 * 24 * time.Hour),
		Description: "Build, test and ship",
		Entrypoint:  "main", ServiceAccount: "releaser",
		Labels: map[string]string{"team": "platform", "app": "shop"},
		Arguments: []core.Argument{
			{Name: "target", Value: "linux/amd64", HasValue: true, Enum: []string{"linux/amd64", "linux/arm64"}},
			{Name: "git-sha", Description: "the commit"},
			{Name: "retries", Default: "3"},
			{Name: "token", ValueFrom: "supplied at run time"},
		},
		Templates: []core.TemplateInfo{{Name: "main", Type: "steps"}, {Name: "build", Type: "container"}, {Name: "approve", Type: "suspend"}},
	}
}

// pane is the app's list of spec at w×h holding items.
func pane(spec kindlist.Spec[core.WorkflowTemplate], w, h int, items ...core.WorkflowTemplate) *kindlist.Model[core.WorkflowTemplate] {
	m := kindlist.New(spec, shared.NewTheme(true))
	m.SetSize(w, h)
	m.SetItems(items, now)
	m.SetStatus(kindlist.StatusIdle, "", 0)
	return m
}

// press sends each key in turn, drawing the pane first as the shell does.
func press(m *kindlist.Model[core.WorkflowTemplate], keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		m.BodyLines(now)
		msg := tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		if k == "enter" {
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
		cmd = m.Update(msg)
	}
	return cmd
}

// lines is the pane's body with styling removed and runs of spaces collapsed.
func lines(m *kindlist.Model[core.WorkflowTemplate]) []string {
	var out []string
	for _, l := range m.BodyLines(now) {
		out = append(out, strings.Join(strings.Fields(ansi.Strip(l)), " "))
	}
	return out
}

// A row shows the entrypoint, or the template it runs, the template and
// parameter counts, the age and the description.
func TestRows(t *testing.T) {
	thin := core.WorkflowTemplate{Namespace: "ci", Name: "thin", WorkflowTemplateRef: "cluster/base"}
	got := strings.Join(lines(pane(Spec(), 136, 10, release(), thin)), "\n")
	for _, want := range []string{
		"release main 3 4 90d Build, test and ship",
		"thin → cluster/base 0 0 -",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no row %q in\n%s", want, got)
		}
	}
}

// Every layout fits its width: a narrow pane drops the entrypoint and a wide
// one adds the description.
func TestLayout(t *testing.T) {
	cases := []struct {
		width int
		head  string
	}{
		{60, "NAME TEMPLATES PARAMETERS AGE"},
		{76, "NAME ENTRYPOINT TEMPLATES PARAMETERS AGE"},
		{100, "NAME ENTRYPOINT TEMPLATES PARAMETERS AGE DESCRIPTION"},
		{136, "NAME ENTRYPOINT TEMPLATES PARAMETERS AGE DESCRIPTION"},
	}
	for _, c := range cases {
		m := pane(Spec(), c.width, 10, release())
		if got := lines(m)[1]; got != c.head {
			t.Errorf("width %d: head %q, want %q", c.width, got, c.head)
		}
		for _, l := range m.BodyLines(now) {
			if n := ansi.StringWidth(l); n > c.width {
				t.Errorf("width %d: line of %d cells: %q", c.width, n, ansi.Strip(l))
			}
		}
	}
}

// Name order ignores case and is the default; s sorts newest first.
func TestSort(t *testing.T) {
	older, newer, upper := release(), release(), release()
	newer.Name, newer.CreatedAt = "zulu-newer", now.Add(-time.Hour)
	upper.Name, upper.CreatedAt = "Yankee", now.Add(-48*time.Hour)
	cases := []struct {
		keys []string
		want []string
	}{
		{nil, []string{"release", "Yankee", "zulu-newer"}},
		{[]string{"s"}, []string{"zulu-newer", "Yankee", "release"}},
	}
	for _, c := range cases {
		m := pane(Spec(), 100, 10, older, newer, upper)
		press(m, c.keys...)
		var got []string
		for _, l := range lines(m)[2:] {
			if f := strings.Fields(l); len(f) > 0 {
				got = append(got, f[0])
			}
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("keys %v: order %v, want %v", c.keys, got, c.want)
		}
	}
}

// The panel lists the entrypoint, every argument with its value redacted
// until v, each template with its type, the service account, the labels and
// where enter leads; a bare cluster template says what it lacks.
func TestInfoPanel(t *testing.T) {
	cases := []struct {
		name    string
		spec    kindlist.Spec[core.WorkflowTemplate]
		item    core.WorkflowTemplate
		keys    []string
		want    []string
		without []string
	}{
		{"redacted", Spec(), release(), []string{"i"}, []string{
			"Description Build, test and ship", "Entrypoint main",
			"Arguments target = [REDACTED] · one of: linux/amd64, linux/arm64",
			"git-sha = (no value: supplied at submission) — the commit",
			"retries = (the default) · default [REDACTED]",
			"token = (from supplied at run time)",
			"Templates main · steps · entrypoint", "build · container", "approve · suspend",
			"Service acct releaser", "Labels app=shop", "team=platform",
			"Created 2026-06-10 12:00 UTC (90d ago)",
			"Runs enter lists the workflows labelled workflows.argoproj.io/workflow-template=release",
		}, []string{"linux/amd64 ·", "default 3"}},
		{"revealed", Spec(), release(), []string{"i", "v"},
			[]string{"target = linux/amd64 · one of", "retries = (the default) · default 3"}, []string{"[REDACTED]"}},
		{"bare cluster template", ClusterSpec(), core.WorkflowTemplate{Name: "whalesay"}, []string{"i"}, []string{
			"Entrypoint not set: chosen at submission", "Arguments none", "Templates none",
			"Service acct not set: the namespace's default, or the one given at submission", "Labels none",
			"workflows.argoproj.io/cluster-workflow-template=whalesay",
		}, []string{"Created"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pane(c.spec, 100, 60, c.item)
			m.SetRedact(true)
			press(m, c.keys...)
			got := strings.Join(lines(m), "\n")
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("panel lacks %q:\n%s", want, got)
				}
			}
			for _, bad := range c.without {
				if strings.Contains(got, bad) {
					t.Errorf("panel shows %q:\n%s", bad, got)
				}
			}
		})
	}
}

// A namespaced template drills into its own namespace by the controller's
// template label; a cluster template into the session's scope by the cluster
// label, and has no namespace keys.
func TestDrill(t *testing.T) {
	cases := []struct {
		name      string
		spec      kindlist.Spec[core.WorkflowTemplate]
		item      core.WorkflowTemplate
		wantTitle string
		want      kindlist.DrillMsg
		wantNS    bool
	}{
		{"namespaced", Spec(), release(), "Workflow templates",
			kindlist.DrillMsg{Namespace: "ci", Selector: "workflows.argoproj.io/workflow-template=release", Title: "template release"}, true},
		{"cluster", ClusterSpec(), core.WorkflowTemplate{Name: "whalesay"}, "Cluster workflow templates",
			kindlist.DrillMsg{Selector: "workflows.argoproj.io/cluster-workflow-template=whalesay", Title: "cluster template whalesay"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pane(c.spec, 100, 10, c.item)
			if got := press(m, "enter")(); got != c.want {
				t.Errorf("enter = %#v, want %#v", got, c.want)
			}
			if m.PaneTitle() != c.wantTitle {
				t.Errorf("title = %q, want %q", m.PaneTitle(), c.wantTitle)
			}
			if got := strings.Contains(m.Hints(), "0 all ns"); got != c.wantNS {
				t.Errorf("hints = %q, want the namespace keys: %v", m.Hints(), c.wantNS)
			}
		})
	}
}
