package diagnose

import (
	"fmt"
	"sort"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// eventFindings quotes the warnings matched to this run, one card each, and
// says so only when the event stream cannot be trusted to have them all.
func eventFindings(in Input) []Finding {
	if in.Events == nil {
		return nil
	}
	ref := in.Workflow.Summary.Ref
	pods := map[string][]string{}
	for id, n := range in.Workflow.Nodes {
		if n.PodName != "" {
			pods[n.PodName] = append(pods[n.PodName], id)
		}
	}
	events := append([]core.Event(nil), in.Events...)
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.Before(b.LastSeen)
		}
		if a.ObjectName != b.ObjectName {
			return a.ObjectName < b.ObjectName
		}
		if a.Reason != b.Reason {
			return a.Reason < b.Reason
		}
		if a.Message != b.Message {
			return a.Message < b.Message
		}
		return a.UID < b.UID
	})
	var out []Finding
	overLimit := 0
	for _, e := range events {
		if e.Type != "Warning" || e.Deleted {
			continue
		}
		if e.Namespace != ref.Namespace {
			continue
		}
		id, object := "", "workflow"
		switch e.ObjectKind {
		case "Workflow":
			if ref.UID == "" || e.ObjectUID != ref.UID || e.ObjectName != ref.Name {
				continue
			}
			// The controller's phase warnings restate the failure cards.
			if e.Reason == "WorkflowFailed" || e.Reason == "WorkflowNodeFailed" || e.Reason == "WorkflowNodeError" {
				continue
			}
		case "Pod":
			ids := pods[e.ObjectName]
			if len(ids) != 1 || in.Workflow.Summary.CreatedAt.IsZero() || e.FirstSeen.IsZero() || e.FirstSeen.Before(in.Workflow.Summary.CreatedAt) {
				continue
			}
			id = ids[0]
			if finish := in.Workflow.Summary.FinishedAt; finish != nil {
				end := *finish
				if nodeFinish := in.Workflow.Nodes[id].FinishedAt; nodeFinish != nil && nodeFinish.After(end) {
					end = *nodeFinish
				}
				if e.FirstSeen.After(end) {
					continue
				}
			}
			object = in.Workflow.Nodes[id].DisplayName
			if object == "" {
				object = id
			}
		default:
			continue
		}
		if len(out) >= 20 {
			overLimit++
			continue
		}
		when := "time unknown"
		if !e.LastSeen.IsZero() {
			when = e.LastSeen.UTC().Format(time.RFC3339)
		}
		headline := object + ": " + e.Reason
		if e.Count > 1 {
			headline += fmt.Sprintf(" ×%d", e.Count)
		}
		out = append(out, Finding{Rule: "warning-event", Severity: Warning, NodeID: id, Headline: headline,
			Evidence: []Evidence{{Label: "event", Text: when + " · " + e.ObjectKind + "/" + e.ObjectName + " · " + e.Message}}})
	}
	if overLimit > 0 {
		out = append(out, Finding{Rule: "event-coverage", Severity: Info, Headline: fmt.Sprintf("%d more warning events not shown", overLimit)})
	}
	// "live" is a healthy stream and "" one still starting; anything else is
	// why warnings may be missing.
	if st := in.EventStatus; st != "" && st != "live" {
		out = append(out, Finding{Rule: "event-coverage", Severity: Info, Headline: "Kubernetes events: " + st})
	}
	return out
}
