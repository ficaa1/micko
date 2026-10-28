package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/palette"
)

type comparisonReader struct {
	core.Reader
	list func(core.Query) (core.Page, error)
	get  func(core.Ref) (core.Workflow, error)
}

func (r comparisonReader) List(_ context.Context, q core.Query) (core.Page, error) { return r.list(q) }
func (r comparisonReader) Get(_ context.Context, ref core.Ref) (core.Workflow, error) {
	return r.get(ref)
}

func TestCompareFindsPreviousSuccess(t *testing.T) {
	for _, c := range []struct {
		name                                                       string
		labels                                                     map[string]string
		want                                                       string
		incomplete, forbidden, replaced, absent, repeat, oversized bool
	}{
		{name: "cron wins", labels: map[string]string{"workflows.argoproj.io/cron-workflow": "hourly", "workflows.argoproj.io/workflow-template": "etl"}, want: "workflows.argoproj.io/cron-workflow"},
		{name: "template", labels: map[string]string{"workflows.argoproj.io/workflow-template": "etl"}, want: "workflows.argoproj.io/workflow-template"},
		{name: "cluster template", labels: map[string]string{"workflows.argoproj.io/cluster-workflow-template": "etl"}, want: "workflows.argoproj.io/cluster-workflow-template"},
		{name: "no lineage", want: "No baseline"},
		{name: "repeated cursor", labels: map[string]string{"workflows.argoproj.io/workflow-template": "etl"}, want: "Search incomplete", repeat: true},
		{name: "ignored page size", labels: map[string]string{"workflows.argoproj.io/workflow-template": "etl"}, want: "Search incomplete", oversized: true},
		{name: "no eligible success", labels: map[string]string{"workflows.argoproj.io/workflow-template": "etl"}, want: "No previous succeeded run found", absent: true},
		{name: "bounded", labels: map[string]string{"workflows.argoproj.io/cron-workflow": "hourly"}, want: "Search incomplete", incomplete: true},
		{name: "forbidden", labels: map[string]string{"workflows.argoproj.io/cron-workflow": "hourly"}, want: "not permitted", forbidden: true},
		{name: "replaced baseline", labels: map[string]string{"workflows.argoproj.io/cron-workflow": "hourly"}, want: "changed identity", replaced: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, f := demoRoot(t)
			selected := testkit.SyntheticWorkflow("demo", "selected", "Failed", testkit.FixtureEpoch)
			selected.Summary.Labels = c.labels
			m.selection = selected.Summary.Ref
			m.route = RouteDetail
			m.detailState = detailState{ref: m.selection, workflow: selected}
			m.detailView.SetWorkflow(selected, testkit.FixtureEpoch)
			baseline := testkit.SyntheticWorkflow("demo", "baseline", "Succeeded", testkit.FixtureEpoch.Add(-time.Hour))
			baseline.Summary.Labels = c.labels
			end := testkit.FixtureEpoch.Add(-time.Minute)
			baseline.Summary.FinishedAt = &end
			older := baseline
			older.Summary.CreatedAt = older.Summary.CreatedAt.Add(-time.Hour)
			older.Summary.Ref.UID = "older"
			wrongNS := baseline
			wrongNS.Summary.Ref.Namespace = "other"
			wrongLineage := baseline
			wrongLineage.Summary.Labels = map[string]string{}
			overlapping := baseline
			later := testkit.FixtureEpoch.Add(time.Minute)
			overlapping.Summary.FinishedAt = &later
			future := baseline
			future.Summary.CreatedAt = later
			failed := baseline
			failed.Summary.Phase = "Failed"
			missingUID := baseline
			missingUID.Summary.Ref.UID = ""
			reads, gets := 0, 0
			m.deps.reader = comparisonReader{Reader: f, list: func(q core.Query) (core.Page, error) {
				reads++
				if q.Namespace != "demo" || !q.SummaryOnly || q.Limit != 100 {
					t.Fatalf("query %+v", q)
				}
				if c.name == "cron wins" && !strings.HasPrefix(q.LabelSelector, "workflows.argoproj.io/cron-workflow=") {
					t.Fatalf("lineage: %s", q.LabelSelector)
				}
				if c.forbidden {
					return core.Page{}, core.NewAPIError(core.ErrForbidden, 403, "not permitted")
				}
				token := ""
				if c.repeat {
					token = "repeat"
				}
				if c.incomplete {
					token = string(rune('a' + reads))
				}
				items := []core.Summary{wrongNS.Summary, wrongLineage.Summary, overlapping.Summary, future.Summary, failed.Summary, missingUID.Summary}
				if !c.absent {
					items = append(items, older.Summary, baseline.Summary)
				}
				if c.oversized {
					for len(items) < 150 {
						items = append(items, baseline.Summary)
					}
				}
				return core.Page{Items: items, Continue: token}, nil
			}, get: func(ref core.Ref) (core.Workflow, error) {
				gets++
				if ref != baseline.Summary.Ref {
					t.Fatalf("got unrelated run %+v", ref)
				}
				wf := baseline
				if c.replaced {
					wf.Summary.Ref.UID = "replacement"
				}
				return wf, nil
			}}
			settle(m, keys(m, "C"))
			raw := m.copyText()
			if !strings.Contains(raw, c.want) || !strings.Contains(raw, "archive candidates are not searched") {
				t.Fatalf("report:\n%s", raw)
			}
			if c.name == "no lineage" {
				if reads != 0 || gets != 0 {
					t.Fatal("searched unrelated runs")
				}
			} else if c.absent && gets != 0 {
				t.Fatal("read a detail without a candidate")
			} else if !c.absent && !c.forbidden && gets != 1 {
				t.Fatalf("full GETs=%d", gets)
			}
			if reads > 3 {
				t.Fatalf("unbounded list: %d", reads)
			}
		})
	}
}

func TestCompareEntryRefreshAndStaleReplies(t *testing.T) {
	m := loadDemoList(t)
	for i := 0; i < 20 && m.listView.SelectedRef().Name != "demo-etl-hourly-1790000000"; i++ {
		typeKeys(m, "j")
	}
	_, cmd := m.Update(palette.RunMsg{Name: "compare"})
	settle(m, cmd)
	if m.route != RouteDetail || m.detailView.Section() != "compare" {
		t.Fatal("palette did not open comparison")
	}
	raw := m.copyText()
	for _, want := range []string{"demo-etl-hourly-1789996400", "Succeeded → Failed", "Changed input parameters", "quality-check"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %q: %s", want, raw)
		}
	}
	f := m.deps.reader.(*testkit.FakeReader)
	before := f.GetCalls
	settle(m, keys(m, "r"))
	if f.GetCalls != before+2 {
		t.Fatalf("refresh GETs=%d want detail and baseline", f.GetCalls-before)
	}
	keys(m, "esc")
	if m.route != RouteDetail || m.detailView.Section() == "compare" {
		t.Fatal("escape did not return to run")
	}
	keys(m, "C")
	m.detailView.ResetComparison()
	cmd = m.syncComparison()
	msgs := runCmd(cmd)
	var reply comparisonMsg
	for _, msg := range msgs {
		if value, ok := msg.(comparisonMsg); ok {
			reply = value
		}
	}
	if reply.RequestID == 0 {
		t.Fatal("no comparison reply")
	}
	// A reply for an older connection, selection, identity or request cannot change the report.
	for _, kind := range []string{"connection", "selection", "UID", "request"} {
		bad := reply
		bad.Note = "STALE BASELINE"
		switch kind {
		case "connection":
			bad.Conn--
		case "selection":
			bad.Sel--
		case "UID":
			bad.Ref.UID = "other"
		case "request":
			bad.RequestID--
		}
		m.Update(bad)
		if strings.Contains(m.copyText(), "STALE BASELINE") || !m.hasInflight("comparison") {
			t.Fatalf("accepted stale %s", kind)
		}
	}
	m.Update(reply)
	if m.hasInflight("comparison") {
		t.Fatal("reply did not finish search")
	}
	m.detailView.ResetComparison()
	cmd = m.syncComparison()
	keys(m, "esc")
	if m.hasInflight("comparison") {
		t.Fatal("leaving comparison did not cancel")
	}
	for _, msg := range runCmd(cmd) {
		m.Update(msg)
	}
	if m.detailView.Section() == "compare" {
		t.Fatal("canceled reply reopened comparison")
	}
}
