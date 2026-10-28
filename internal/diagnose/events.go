package diagnose

import (
	"fmt"
	"sort"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// eventFindings quotes observations without treating an old warning as a current cause.
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
	skipped := 0
	for _, e := range events {
		if e.Type != "Warning" || e.Deleted {
			continue
		}
		if e.Namespace != ref.Namespace {
			skipped++
			continue
		}
		id, object := "", "workflow"
		switch e.ObjectKind {
		case "Workflow":
			if ref.UID == "" || e.ObjectUID != ref.UID || e.ObjectName != ref.Name {
				skipped++
				continue
			}
		case "Pod":
			ids := pods[e.ObjectName]
			if len(ids) != 1 || in.Workflow.Summary.CreatedAt.IsZero() || e.FirstSeen.IsZero() || e.FirstSeen.Before(in.Workflow.Summary.CreatedAt) {
				skipped++
				continue
			}
			id = ids[0]
			if finish := in.Workflow.Summary.FinishedAt; finish != nil {
				end := *finish
				if nodeFinish := in.Workflow.Nodes[id].FinishedAt; nodeFinish != nil && nodeFinish.After(end) {
					end = *nodeFinish
				}
				if e.FirstSeen.After(end) {
					skipped++
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
			skipped++
			continue
		}
		when := "time unavailable"
		if !e.LastSeen.IsZero() {
			when = e.LastSeen.UTC().Format(time.RFC3339)
		}
		evidence := []Evidence{{Label: "event", Text: fmt.Sprintf("%s · %s · %s · count %d · %s", when, e.ObjectKind+"/"+e.ObjectName, e.Reason, max(e.Count, 1), e.Message)}}
		if id != "" {
			evidence = append(evidence, Evidence{Label: "identity", Text: "Matched by the workflow's verified pod name and event time; pod UID is not available in workflow node status."})
		}
		out = append(out, Finding{Rule: "warning-event", Severity: Warning, NodeID: id, Headline: object + ": Kubernetes reported " + e.Reason, Evidence: evidence, Next: "Check the event time and current node state; this observation does not establish that the condition persists."})
	}
	status := in.EventStatus
	if status == "" {
		status = "connecting; event evidence is pending"
	}
	text := status + ". At most 2,000 recent candidate events and 20 warning cards are retained. Kubernetes events may expire (usually after about an hour); absence is not evidence of no problem."
	if len(out) == 0 {
		text += " No correlated warning events are available."
	}
	if skipped > 0 {
		text += fmt.Sprintf(" %d warnings omitted: identity/time could not be verified, or the card limit was reached.", skipped)
	}
	out = append(out, Finding{Rule: "event-coverage", Severity: Info, Headline: "Kubernetes event coverage", Evidence: []Evidence{{Label: "events", Text: text}}})
	return out
}
