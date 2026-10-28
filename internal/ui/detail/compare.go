package detail

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnose"
	"github.com/ficaa1/micko/internal/ui/shared"
)

type comparisonState struct {
	requested bool
	baseline  *core.Workflow
	note      string
	top       int
	report    diagnose.Report
	at        time.Time
	stale     bool
}

// CompareWanted reports whether the open comparison needs a baseline search.
func (m *Model) CompareWanted() bool {
	return m.tab == "compare" && m.loaded && !m.loading && !m.notFound && m.lastErr == "" && !m.cmp.requested
}

// StartCompare marks the baseline search as pending.
func (m *Model) StartCompare() {
	m.cmp = comparisonState{requested: true, note: "Searching for the previous success…"}
}

// SetComparison applies a baseline search and its coverage notice.
func (m *Model) SetComparison(baseline *core.Workflow, note string) {
	m.cmp.baseline, m.cmp.note = baseline, note
	m.cmp.stale = true
}

// ResetComparison asks for a fresh baseline search when Compare is visible.
func (m *Model) ResetComparison() { m.cmp = comparisonState{} }

func (m *Model) comparisonReport() diagnose.Report {
	if m.cmp.stale || m.cmp.at.IsZero() || m.now.Sub(m.cmp.at) >= time.Second || m.now.Before(m.cmp.at) {
		m.cmp.report = m.buildComparisonReport()
		m.cmp.at, m.cmp.stale = m.now, false
	}
	return m.cmp.report
}

func (m *Model) buildComparisonReport() diagnose.Report {
	wf := m.explainWorkflow()
	note := m.cmp.note
	if note == "" {
		note = "Waiting for workflow detail and baseline search…"
	}
	report := diagnose.Report{Findings: []diagnose.Finding{{Severity: diagnose.Info, Headline: "Compare with last success", Evidence: []diagnose.Evidence{{Label: "search", Text: note}, {Label: "selected", Text: refText(wf.Summary.Ref)}}}}}
	base := m.cmp.baseline
	if base == nil {
		return report
	}
	report.Findings[0].Evidence = append(report.Findings[0].Evidence,
		diagnose.Evidence{Label: "baseline", Text: refText(base.Summary.Ref)},
		diagnose.Evidence{Label: "status", Text: base.Summary.Phase + " → " + wf.Summary.Phase},
		diagnose.Evidence{Label: "duration", Text: durationChange(base.Summary.StartedAt, base.Summary.FinishedAt, base.Summary.Phase, wf.Summary.StartedAt, wf.Summary.FinishedAt, wf.Summary.Phase, m.now)})
	report.Findings = append(report.Findings, compareParameters(base.Summary.Arguments, wf.Summary.Arguments)...)
	if !wf.NodesAvailable || !base.NodesAvailable {
		evidence := []diagnose.Evidence{{Label: "nodes", Text: "Added or missing steps cannot be determined from unavailable node status."}}
		for _, side := range []struct {
			name     string
			workflow core.Workflow
		}{{"baseline", *base}, {"selected", wf}} {
			if !side.workflow.NodesAvailable {
				reason := side.workflow.NodesUnavailableReason
				if reason == "" {
					reason = "node status unavailable"
				}
				evidence = append(evidence, diagnose.Evidence{Label: side.name, Text: reason})
			}
		}
		report.Findings = append(report.Findings, diagnose.Finding{Severity: diagnose.Warning, Headline: "Step comparison unavailable", Evidence: evidence})
		return report
	}
	a, b := comparisonSteps(*base), comparisonSteps(wf)
	union := maps.Clone(a)
	maps.Copy(union, b)
	ordered := slices.Sorted(maps.Keys(union))
	report.Findings = append(report.Findings, diagnose.Finding{Severity: diagnose.Info, Headline: "Steps (baseline → selected)", Evidence: []diagnose.Evidence{{Label: "matching", Text: "Task path, node type and template identify each position. Fanout/retry instances, duplicate positions and unplaced nodes are ambiguous; their individual durations are not aligned. Retry parents show aggregate retry counts. At most 300 positions are shown."}}})
	for i, k := range ordered {
		if i == 300 {
			report.Findings = append(report.Findings, diagnose.Finding{Severity: diagnose.Warning, Headline: fmt.Sprintf("%d further positions omitted", len(ordered)-i)})
			break
		}
		left, right := a[k], b[k]
		f := diagnose.Finding{Severity: diagnose.Info, Headline: k}
		ambiguous := len(left) > 1 || len(right) > 1
		for _, s := range append(append([]comparisonStep(nil), left...), right...) {
			ambiguous = ambiguous || s.ambiguous
		}
		switch {
		case ambiguous:
			f.Severity = diagnose.Warning
			f.Evidence = []diagnose.Evidence{{Label: "alignment", Text: fmt.Sprintf("Ambiguous: baseline %d nodes, selected %d nodes; no individual alignment.", len(left), len(right))}}
		case len(left) == 0:
			f.Evidence = []diagnose.Evidence{{Label: "added", Text: stepSummary(right[0], m.now)}}
		case len(right) == 0:
			f.Evidence = []diagnose.Evidence{{Label: "missing", Text: stepSummary(left[0], m.now)}}
		default:
			l, r := left[0], right[0]
			f.Evidence = []diagnose.Evidence{{Label: "status", Text: l.node.Phase + " → " + r.node.Phase}, {Label: "retries", Text: fmt.Sprintf("%d → %d", l.retries, r.retries)}, {Label: "duration", Text: durationChange(l.node.StartedAt, l.node.FinishedAt, l.node.Phase, r.node.StartedAt, r.node.FinishedAt, r.node.Phase, m.now)}}
		}
		report.Findings = append(report.Findings, f)
	}
	return report
}

func refText(r core.Ref) string { return r.Namespace + "/" + r.Name + " · UID " + r.UID }

func compareParameters(a, b []core.Argument) []diagnose.Finding {
	left, right := map[string]core.Argument{}, map[string]core.Argument{}
	for _, p := range a {
		left[p.Name] = p
	}
	for _, p := range b {
		right[p.Name] = p
	}
	union := maps.Clone(left)
	maps.Copy(union, right)
	ordered := slices.Sorted(maps.Keys(union))
	f := diagnose.Finding{Severity: diagnose.Info, Headline: "Changed input parameters"}
	for _, name := range ordered {
		l, lok := left[name]
		r, rok := right[name]
		if lok && rok && l.HasValue == r.HasValue && l.Value == r.Value && l.Default == r.Default {
			continue
		}
		ev := diagnose.Evidence{Label: "parameter", Text: name + " changed"}
		if !lok {
			ev.Text = name + " added"
		}
		if !rok {
			ev.Text = name + " missing"
		}
		if lok {
			ev.Params = append(ev.Params, core.Parameter{Name: "baseline", Value: comparisonArgument(l)})
		}
		if rok {
			ev.Params = append(ev.Params, core.Parameter{Name: "selected", Value: comparisonArgument(r)})
		}
		f.Evidence = append(f.Evidence, ev)
	}
	if len(f.Evidence) == 0 {
		f.Evidence = []diagnose.Evidence{{Label: "parameters", Text: "No changed input parameters recorded."}}
	}
	return []diagnose.Finding{f}
}

func comparisonArgument(p core.Argument) string {
	if p.HasValue {
		return p.Value
	}
	if p.Default != "" {
		return "default: " + p.Default
	}
	return "(unset)"
}

var comparisonGroup = regexp.MustCompile(`\[[0-9]+\]`)

type comparisonStep struct {
	node      core.Node
	retries   int
	ambiguous bool
}

func comparisonSteps(wf core.Workflow) map[string][]comparisonStep {
	out := map[string][]comparisonStep{}
	outline := BuildNodeOutline(wf, OutlineOptions{})
	var resource struct {
		Status struct {
			Nodes map[string]struct {
				TemplateScope string `json:"templateScope"`
				TemplateRef   *struct {
					Name         string `json:"name"`
					Template     string `json:"template"`
					ClusterScope bool   `json:"clusterScope"`
				} `json:"templateRef"`
			} `json:"nodes"`
		} `json:"status"`
	}
	_ = json.Unmarshal(wf.Resource, &resource)
	var walk func(OutlineRow, string, bool, bool)
	walk = func(r OutlineRow, parent string, root, uncertain bool) {
		name := r.DisplayName
		if root {
			name = "root"
			if r.Role != "" {
				name = r.Role
			}
		}
		if name == "" {
			name = "unknown"
			uncertain = true
		}
		if at := strings.Index(name, "("); at >= 0 {
			name = name[:at] + "(instances)"
			uncertain = true
		}
		n := wf.Nodes[r.NodeID]
		if !root {
			positionName := strings.SplitN(strings.TrimPrefix(n.Name, wf.Summary.Ref.Name), "(", 2)[0]
			if groups := comparisonGroup.FindAllString(positionName, -1); len(groups) > 0 {
				name = groups[len(groups)-1] + " " + name
			}
		}
		template := r.Template
		wire := resource.Status.Nodes[r.NodeID]
		if wire.TemplateRef != nil {
			template = fmt.Sprintf("ref:%t:%s/%s", wire.TemplateRef.ClusterScope, wire.TemplateRef.Name, wire.TemplateRef.Template)
		} else if n.TemplateRefTemplate != "" {
			uncertain = true
		}
		scope := wire.TemplateScope
		if scope == "local/"+wf.Summary.Ref.Name || scope == "local/" || scope == "local" {
			scope = ""
		}
		if scope != "" {
			template = scope + "/" + template
		}
		if r.Type == "TaskGroup" {
			uncertain = true
		}
		if template == "" {
			uncertain = true
		}
		segment := name + " [" + r.Type + " · " + template + "]"
		path := segment
		if parent != "" {
			path = parent + " / " + segment
		}
		out[path] = append(out[path], comparisonStep{node: n, retries: r.Retries, ambiguous: uncertain || n.Retried})
		for _, c := range r.Children {
			walk(c, path, false, uncertain || r.Type == "Retry" || r.Type == "TaskGroup")
		}
	}
	for _, r := range outline.Rows {
		walk(r, "", true, false)
	}
	for _, r := range outline.Unreachable {
		walk(r, "unplaced", false, true)
	}
	return out
}

func recordedDuration(start, end *time.Time, now time.Time, phase string) (time.Duration, string, bool) {
	if start == nil {
		return 0, "unavailable", false
	}
	if end == nil {
		if phase != "Running" && phase != "Pending" {
			return 0, "unavailable", false
		}
		if now.Before(*start) {
			return 0, "unavailable", false
		}
		return now.Sub(*start), shared.ShortDuration(now.Sub(*start)) + " elapsed", true
	}
	if end.Before(*start) {
		return 0, "unavailable", false
	}
	return end.Sub(*start), shared.ShortDuration(end.Sub(*start)), true
}

func durationChange(as, ae *time.Time, ap string, bs, be *time.Time, bp string, now time.Time) string {
	a, at, aok := recordedDuration(as, ae, now, ap)
	b, bt, bok := recordedDuration(bs, be, now, bp)
	text := at + " → " + bt
	if aok && bok {
		delta := b - a
		sign := "+"
		if delta < 0 {
			sign = "−"
			delta = -delta
		}
		text += " (" + sign + shared.ShortDuration(delta) + ")"
	}
	return text
}

func stepSummary(s comparisonStep, now time.Time) string {
	_, duration, _ := recordedDuration(s.node.StartedAt, s.node.FinishedAt, now, s.node.Phase)
	return fmt.Sprintf("%s · retries %d · %s", s.node.Phase, s.retries, duration)
}

func (m *Model) comparisonLines(raw bool) []string {
	width, theme := m.width, m.theme
	if raw {
		width, theme = 0, shared.NewTheme(true)
	}
	report := m.comparisonReport()
	renderer := newExplainRenderer(report, theme, width, m.revealResource, raw)
	lines, _ := renderer.cards(report)
	return lines
}
