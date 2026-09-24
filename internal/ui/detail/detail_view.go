package detail

import (
	"strings"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// DetailViewState carries everything the detail renderer needs, derived
// from a core.Workflow. It is a plain value — the root app owns it; this
// package never starts goroutines or issues network effects (plan §4).
type DetailViewState struct {
	Summary  core.Summary
	Outline  Outline
	Resource string // pre-rendered, redacted + sanitized YAML
	Message  string
	// Nodes is the node map as it arrived, for the timeline's span.
	Nodes map[string]core.Node
}

// DetailViewStateFromWorkflow derives the view state from a workflow. now
// drives the deterministic age/duration math; the caller (root app, on a
// fake or real clock) supplies it — this function never reads the wall
// clock. The resource view is rendered redacted by default; reveal is an
// explicit per-session decision of the caller via RenderResource's
// argument.
func DetailViewStateFromWorkflow(wf core.Workflow, now time.Time) DetailViewState {
	return DetailViewState{
		Summary:  wf.Summary,
		Outline:  BuildNodeOutline(wf, OutlineOptions{}),
		Resource: RenderResource(wf, false),
		Message:  wf.Summary.Message,
		Nodes:    wf.Nodes,
	}
}

// RenderDetail renders one detail tab. active selects the body pane;
// unknown tabs render an explicit unknown-tab line (distinguishable
// states, plan §2). Deterministic for identical state.
func RenderDetail(state DetailViewState, active string) string {
	return "DETAIL " + shared.Sanitize(state.Summary.Ref.Name) + "\n" +
		RenderDetailBody(state, active)
}

// RenderDetailBody renders the tab strip and the active section without the
// pane title, which the shell draws in its border.
func RenderDetailBody(state DetailViewState, active string) string {
	var b strings.Builder
	b.WriteString(tabStrip(active, shared.Theme{}) + "\n")
	switch active {
	case "summary":
		b.WriteString("phase: " + shared.Sanitize(phaseName(state.Summary.Phase)) + "\n")
		if state.Message != "" {
			b.WriteString("message: " + shared.Sanitize(state.Message) + "\n")
		} else if state.Summary.Phase == "Failed" || state.Summary.Phase == "Error" {
			b.WriteString("message: (none)\n")
		}
	case "nodes":
		b.WriteString(renderOutlinePane(state.Outline))
	case "timeline":
		for _, l := range renderTimelineText(state) {
			b.WriteString(l + "\n")
		}
	case "resource":
		b.WriteString(state.Resource)
	default:
		b.WriteString("(unknown tab \"" + shared.Sanitize(active) + "\")\n")
	}
	return b.String()
}

// renderTimelineText is the timeline as plain text with no clock: the chart
// ends at the latest time the workflow records, so a running node's bar
// reaches as far as the data does.
func renderTimelineText(state DetailViewState) []string {
	m := New()
	m.state = state
	m.nodeMap = state.Nodes
	m.now = workflowSpan(m.workflow(), time.Time{}).end
	m.rebuildNodes("")
	m.showSection("timeline")
	return m.timelineRawLines()
}
