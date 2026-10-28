package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

type comparisonMsg struct {
	genStamp
	RequestID uint64
	Ref       core.Ref
	Baseline  *core.Workflow
	Note      string
}

func (m *Root) openComparison() tea.Cmd {
	if m.route == RouteList {
		ref := m.listView.SelectedRef()
		if ref.UID == "" {
			m.flash = "select a workflow to compare"
			return nil
		}
		cmd := m.openWorkflow(ref)
		m.detailView.SetSection("compare")
		return tea.Batch(cmd, m.syncSections())
	}
	if m.route != RouteDetail {
		m.flash = "open a workflow to compare"
		return nil
	}
	m.detailView.SetSection("compare")
	return m.syncSections()
}

func (m *Root) syncComparison() tea.Cmd {
	if m.route != RouteDetail || m.detailView == nil || m.detailView.Section() != "compare" {
		m.stopComparison()
		return nil
	}
	if m.detailState.loading || !m.detailView.CompareWanted() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	id := m.ids.newID()
	m.comparisonID = id
	m.setInflight("comparison", id, cancel)
	m.detailView.StartCompare()
	wf := m.detailState.workflow
	d := m.deps
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	return func() tea.Msg {
		defer cancel()
		baseline, note := findComparison(ctx, d.reader, wf)
		return comparisonMsg{genStamp: g, RequestID: id, Ref: wf.Summary.Ref, Baseline: baseline, Note: note}
	}
}

func (m *Root) handleComparison(msg comparisonMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Ref != m.selection || msg.RequestID != m.comparisonID || !m.hasInflight("comparison") {
		return nil
	}
	m.clearInflight("comparison", msg.RequestID)
	if m.route != RouteDetail || m.detailView.Section() != "compare" {
		return nil
	}
	if msg.Baseline != nil {
		key, value := comparisonLineage(m.detailState.workflow.Summary)
		if !comparisonCandidate(msg.Baseline.Summary, m.detailState.workflow.Summary, key, value) {
			m.detailView.SetComparison(nil, "Selected run lineage or times changed; refresh to search again.")
			return nil
		}
	}
	m.detailView.SetComparison(msg.Baseline, shared.Sanitize(msg.Note))
	return nil
}

func comparisonLineage(s core.Summary) (string, string) {
	for _, key := range []string{"workflows.argoproj.io/cron-workflow", "workflows.argoproj.io/workflow-template", "workflows.argoproj.io/cluster-workflow-template"} {
		if value := s.Labels[key]; value != "" {
			return key, value
		}
	}
	return "", ""
}

// findComparison reads at most three summary pages and one full baseline.
func findComparison(ctx context.Context, reader core.Reader, wf core.Workflow) (*core.Workflow, string) {
	const coverage = "Live runs only; archive candidates are not searched. At most 300 summaries across three pages and one baseline detail are read. "
	selected := wf.Summary
	key, value := comparisonLineage(selected)
	if key == "" {
		return nil, coverage + "No baseline: this run has no cron or template lineage label; unrelated runs are not compared."
	}
	if selected.Ref.UID == "" || selected.Ref.Namespace == "" || selected.CreatedAt.IsZero() {
		return nil, coverage + "No baseline: selected run identity or creation time is unavailable."
	}
	var candidates []core.Summary
	token := ""
	seen := map[string]bool{}
	complete := false
	count := 0
	resourceVersion := ""
	consistent := true
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		page, err := reader.List(ctx, core.Query{Namespace: selected.Ref.Namespace, LabelSelector: key + "=" + value, Limit: 100, Continue: token, SummaryOnly: true})
		if err != nil {
			return nil, coverage + "Baseline search unavailable: " + comparisonError(ctx, err)
		}
		if page.ResourceVersion != "" {
			if resourceVersion != "" && resourceVersion != page.ResourceVersion {
				consistent = false
			}
			resourceVersion = page.ResourceVersion
		}
		items := page.Items
		oversized := len(items) > 100
		if oversized {
			items = items[:100]
		}
		for _, s := range items {
			if count == 300 {
				break
			}
			count++
			if comparisonCandidate(s, selected, key, value) {
				candidates = append(candidates, s)
			}
		}
		if oversized {
			break
		}
		if page.Continue == "" {
			complete = true
			break
		}
		if count >= 300 || page.Continue == token || seen[page.Continue] {
			break
		}
		token = page.Continue
		seen[token] = true
	}
	note := coverage + fmt.Sprintf("Lineage %s=%s; inspected %d summaries. ", key, value, count)
	if !complete || !consistent {
		note += "Search incomplete: the baseline is the newest eligible success among inspected candidates, and a newer eligible run may be outside this bound. "
	}
	if !consistent {
		note += "List resource versions changed across pages. "
	}
	if len(candidates) == 0 {
		return nil, note + "No previous succeeded run found. Candidates must have been created and finished before the selected run was created, with recorded times and a UID."
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.Ref.UID < b.Ref.UID
	})
	candidate := candidates[0]
	baseline, err := reader.Get(ctx, candidate.Ref)
	if err != nil {
		return nil, note + "Baseline detail unavailable: " + comparisonError(ctx, err)
	}
	if baseline.Summary.Ref != candidate.Ref || !comparisonCandidate(baseline.Summary, selected, key, value) {
		return nil, note + "Baseline changed identity, lineage, phase or time while loading; refresh to search again."
	}
	return &baseline, note + "Baseline selected by newest creation time among eligible successes."
}

func comparisonCandidate(s, selected core.Summary, key, value string) bool {
	candidateKey, candidateValue := comparisonLineage(s)
	return candidateKey == key && candidateValue == value && s.Ref.Namespace == selected.Ref.Namespace && s.Ref.UID != "" && s.Ref.UID != selected.Ref.UID && s.Ref.Name != "" && s.Phase == "Succeeded" && s.Labels[key] == value && !s.CreatedAt.IsZero() && s.CreatedAt.Before(selected.CreatedAt) && s.FinishedAt != nil && !s.FinishedAt.After(selected.CreatedAt) && !s.FinishedAt.Before(s.CreatedAt)
}

func comparisonError(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "request canceled or timed out"
	}
	if ae := core.AsAPIError(err); ae != nil {
		return shared.Sanitize(shared.RedactTokens(ae.Message))
	}
	return "read failed"
}

func (m *Root) stopComparison() {
	if m.hasInflight("comparison") {
		m.cancelInflight("comparison")
		if m.detailView != nil {
			m.detailView.ResetComparison()
		}
	}
}
