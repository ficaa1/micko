package templatelist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/kindlist"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
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

func text(fs []kindlist.Field) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Label + " | " + f.Value + "\n")
	}
	return b.String()
}

// The cells: entrypoint (or the template it runs), the template and
// parameter counts, the age and the description.
func TestCells(t *testing.T) {
	r := release()
	checks := map[string]string{
		"name": "release", "entrypoint": "main", "templates": "3", "parameters": "4",
		"age": "90d", "description": "Build, test and ship",
	}
	for col, want := range checks {
		if got := cell(r, col, now); got != want {
			t.Errorf("%s = %q, want %q", col, got, want)
		}
	}
	ref := core.WorkflowTemplate{Name: "thin", WorkflowTemplateRef: "cluster/base"}
	if got := cell(ref, "entrypoint", now); got != "→ cluster/base" {
		t.Errorf("entrypoint of a referring template = %q", got)
	}
	if got := cell(ref, "age", now); got != "-" {
		t.Errorf("age with no timestamp = %q", got)
	}
}

// The panel lists the entrypoint, every argument with its value redacted
// until reveal, its default, its allowed values and its description, each
// template with its type, the service account and the labels.
func TestInfoPanel(t *testing.T) {
	r := release()
	got := text(Spec().Info(r, false, now))
	for _, want := range []string{
		"Description | Build, test and ship", "Entrypoint | main",
		"Arguments | target = [REDACTED] · one of: linux/amd64, linux/arm64",
		"git-sha = (no value: supplied at submission) — the commit",
		"retries = (the default) · default [REDACTED]",
		"token = (from supplied at run time)",
		"Templates | main · steps · entrypoint", "build · container", "approve · suspend",
		"Service acct | releaser", "Labels | app=shop", "team=platform",
		"Runs | enter lists the workflows labelled workflows.argoproj.io/workflow-template=release",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "target = linux/amd64") || strings.Contains(got, "default 3") {
		t.Fatal("a value shown before reveal")
	}
	revealed := text(Spec().Info(r, true, now))
	if !strings.Contains(revealed, "target = linux/amd64") || !strings.Contains(revealed, "default 3") {
		t.Fatalf("reveal did not show the values:\n%s", revealed)
	}
	cluster := text(ClusterSpec().Info(core.WorkflowTemplate{Name: "whalesay"}, false, now))
	for _, want := range []string{"Entrypoint | not set", "Arguments | none", "Templates | none", "Service acct | not set", "cluster-workflow-template=whalesay"} {
		if !strings.Contains(cluster, want) {
			t.Errorf("cluster panel lacks %q:\n%s", want, cluster)
		}
	}
}

// A namespaced template drills into its own namespace by the controller's
// template label; a cluster template into the session's scope by the
// cluster label.
func TestDrill(t *testing.T) {
	d := Spec().Drill(release())
	if d.Namespace != "ci" || d.Selector != "workflows.argoproj.io/workflow-template=release" || d.Title != "template release" {
		t.Fatalf("drill = %+v", d)
	}
	c := ClusterSpec().Drill(core.WorkflowTemplate{Name: "whalesay"})
	if c.Namespace != "" || c.Selector != "workflows.argoproj.io/cluster-workflow-template=whalesay" || c.Title != "cluster template whalesay" {
		t.Fatalf("cluster drill = %+v", c)
	}
	if !Spec().Namespaced || ClusterSpec().Namespaced {
		t.Fatal("scopes are swapped")
	}
}

// Every layout fits its width; name first by default, newest with s.
func TestLayoutAndSort(t *testing.T) {
	older := release()
	newer := release()
	newer.Name, newer.CreatedAt = "alpha-newer", now.Add(-time.Hour)
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetItems([]core.WorkflowTemplate{older, newer}, now)
	if rows := m.Rows(); rows[0].Name != "alpha-newer" {
		t.Fatalf("default order = %v", rows)
	}
	for _, w := range []int{60, 76, 100, 136} {
		m.SetSize(w, 10)
		lines := m.BodyLines(now)
		for _, l := range lines {
			if n := ansi.StringWidth(ansi.Strip(l)); n > w {
				t.Fatalf("width %d: %d cells: %q", w, n, l)
			}
		}
		head := ansi.Strip(lines[1])
		for _, col := range []string{"NAME", "TEMPLATES", "PARAMETERS", "AGE"} {
			if !strings.Contains(head, col) {
				t.Errorf("width %d lacks %s: %q", w, col, head)
			}
		}
		if w >= 100 && !strings.Contains(head, "DESCRIPTION") {
			t.Errorf("width %d lacks DESCRIPTION: %q", w, head)
		}
	}
}
