package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// templateRoot is a demo root with the list loaded and queries recorded.
func templateRoot(t *testing.T) (*Root, *queryRecorder) {
	t.Helper()
	rec := &queryRecorder{FakeReader: testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))}
	m := newRoot(rec, "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	deliver(m, m.startListGeneration())
	return m, rec
}

// Every template command word shows its list.
func TestTemplateCommands(t *testing.T) {
	cases := []struct {
		word  string
		route Route
		want  []string
	}{
		{"workflowtemplates", RouteTemplates, []string{"Workflow templates", "4 workflow templates", "nightly-report", "release"}},
		{"wftmpl", RouteTemplates, []string{"deploy", "train"}},
		{"tmpl", RouteTemplates, []string{"DESCRIPTION"}},
		{"clusterworkflowtemplates", RouteClusterTemplates, []string{"Cluster workflow templates", "1 cluster workflow template", "whalesay"}},
		{"cwftmpl", RouteClusterTemplates, []string{"whalesay"}},
	}
	for _, c := range cases {
		m, _ := templateRoot(t)
		runLine(m, c.word)
		if m.route != c.route {
			t.Fatalf(":%s: route %v", c.word, m.route)
		}
		v := screen(m)
		for _, w := range c.want {
			if !strings.Contains(v, w) {
				t.Errorf(":%s: screen lacks %q:\n%s", c.word, w, v)
			}
		}
		if c.route == RouteTemplates && strings.Contains(v, "hparam-sweep") {
			t.Errorf(":%s listed another namespace", c.word)
		}
	}
}

// enter lists the template's workflows by label; esc returns.
func TestTemplateDrillDown(t *testing.T) {
	m, rec := templateRoot(t)
	runLine(m, "tmpl")
	for {
		_, name := m.tmplView.SelectedName()
		if name == "nightly-report" {
			break
		}
		typeKeys(m, "j")
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	q := rec.last()
	if q.LabelSelector != "workflows.argoproj.io/workflow-template=nightly-report" || q.Namespace != "demo" {
		t.Fatalf("query = %+v", q)
	}
	rows := m.listView.Rows()
	if len(rows) != 1 || rows[0].Ref.Name != "demo-nightly-report" {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.Contains(screen(m), "Workflows ← template nightly-report") {
		t.Fatalf("title:\n%s", screen(m))
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if _, name := m.tmplView.SelectedName(); m.route != RouteTemplates || name != "nightly-report" {
		t.Fatalf("esc: route %v cursor %q", m.route, name)
	}
}

// A cluster template drills into the session's scope by the cluster label.
func TestClusterTemplateDrillDown(t *testing.T) {
	m, rec := templateRoot(t)
	runLine(m, "cwftmpl")
	deliver(m, pressKey(m, tea.KeyEnter))
	q := rec.last()
	if q.LabelSelector != "workflows.argoproj.io/cluster-workflow-template=whalesay" || q.Namespace != "demo" {
		t.Fatalf("query = %+v", q)
	}
	if rows := m.listView.Rows(); len(rows) != 1 || rows[0].Ref.Name != "demo-hello-world" {
		t.Fatalf("rows = %+v", rows)
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteClusterTemplates {
		t.Fatalf("esc: route %v", m.route)
	}
}

// On the cluster route n and 0 do nothing and say why.
func TestClusterTemplatesIgnoreNamespaceKeys(t *testing.T) {
	m, rec := templateRoot(t)
	runLine(m, "cwftmpl")
	if !strings.Contains(screen(m), "ns: (cluster-scoped)") {
		t.Fatalf("header:\n%s", screen(m))
	}
	calls := rec.KindCalls
	for _, k := range []string{"0", "n"} {
		gen := m.connGen
		deliver(m, typeKeys(m, k))
		if m.connGen != gen || m.deps.allNamespaces || m.namespaceDialogOpen() {
			t.Fatalf("%s acted on the cluster route", k)
		}
		if !strings.Contains(screen(m), "belong to no namespace") {
			t.Fatalf("%s: no explanation:\n%s", k, screen(m))
		}
	}
	if rec.KindCalls != calls {
		t.Fatal("a namespace key refetched the cluster list")
	}
	// The namespaced template list still takes them.
	runLine(m, "tmpl")
	deliver(m, typeKeys(m, "0"))
	if !m.deps.allNamespaces || m.route != RouteTemplates || !strings.Contains(screen(m), "hparam-sweep") {
		t.Fatalf("0 on the template list:\n%s", screen(m))
	}
}

// f shows a template's manifest, redacted under redactValues; o links per kind.
func TestTemplateManifestAndLinks(t *testing.T) {
	m, _ := templateRoot(t)
	m.SetWebURL("https://argo.example")
	runLine(m, "tmpl")
	raw := strings.Join(m.rawLines(), "\n")
	if !strings.Contains(raw, "kind: WorkflowTemplate") || strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("manifest:\n%s", raw)
	}
	m.SetRedactValues(true)
	if raw := strings.Join(m.rawLines(), "\n"); !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("manifest under redactValues:\n%s", raw)
	}
	if got := m.workflowURL(); got != "https://argo.example/workflow-templates/demo/deploy" {
		t.Fatalf("url = %q", got)
	}
	runLine(m, "cwftmpl")
	if got := m.workflowURL(); got != "https://argo.example/cluster-workflow-templates/whalesay" {
		t.Fatalf("cluster url = %q", got)
	}
}

// Each template kind reports its own refusal.
func TestTemplateErrors(t *testing.T) {
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	f.ClusterTemplateErr = core.NewAPIError(core.ErrForbidden, 403, `clusterworkflowtemplates.argoproj.io is forbidden: cannot list resource "clusterworkflowtemplates" at the cluster scope`)
	m := newRoot(f, "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runLine(m, "cwftmpl")
	if v := screen(m); !strings.Contains(v, "no cluster workflow templates visible: list forbidden") || !strings.Contains(v, "at the cluster scope") {
		t.Fatalf("403:\n%s", v)
	}
	runLine(m, "tmpl")
	if v := screen(m); !strings.Contains(v, "4 workflow templates") {
		t.Fatalf("the namespaced list was affected:\n%s", v)
	}
	m = newRoot(onlyReader{f}, "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runLine(m, "tmpl")
	if v := screen(m); !strings.Contains(v, "cannot list workflow templates") {
		t.Fatalf("no lister:\n%s", v)
	}
}
