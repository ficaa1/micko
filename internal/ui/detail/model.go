package detail

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// RenderSummary renders the one-screen workflow summary (DET-01): name,
// namespace, phase, age, duration, message, labels and argument names
// (values redacted per DET-12 policy — names only, never values).
//
// now is the injected "current time" for deterministic age/duration math —
// the summary never reads the wall clock itself.

func RenderSummary(wf core.Workflow, now time.Time) string {
	var b strings.Builder
	s := wf.Summary
	b.WriteString("workflow: " + shared.Sanitize(s.Ref.Name) + "\n")
	b.WriteString("namespace: " + shared.Sanitize(s.Ref.Namespace) + "\n")
	b.WriteString("phase: " + shared.Sanitize(phaseName(s.Phase)) + "\n")

	// Age: from CreatedAt, or explicit "unknown" when absent.
	if s.CreatedAt.IsZero() {
		b.WriteString("age: unknown\n")
	} else {
		b.WriteString("age: " + humanDuration(now.Sub(s.CreatedAt)) + "\n")
	}

	// Duration rule (DET-01 companion): finished workflows measure
	// started→finished; running ones measure started→now (labeled running);
	// a workflow without a start time has no duration — stated explicitly,
	// never fabricated as 0s.
	switch {
	case s.StartedAt == nil:
		if s.Phase == "" {
			b.WriteString("duration: n/a (not started)\n")
		} else {
			b.WriteString("duration: n/a (no start time recorded)\n")
		}
	case s.FinishedAt != nil:
		b.WriteString("duration: " + humanDuration(s.FinishedAt.Sub(*s.StartedAt)) + "\n")
	default:
		b.WriteString("duration: " + humanDuration(now.Sub(*s.StartedAt)) + " (running)\n")
	}

	if s.Message != "" {
		b.WriteString("message: " + shared.Sanitize(s.Message) + "\n")
	} else if s.Phase == "Failed" || s.Phase == "Error" {
		// A failed workflow with no message: say so, don't hide the line.
		b.WriteString("message: (none)\n")
	}

	// Labels (sorted for determinism). Values pass the sanitizer; token
	// shapes get redacted defensively.
	if len(s.Labels) > 0 {
		b.WriteString("labels: " + renderLabelsSorted(s.Labels) + "\n")
	}

	// Argument parameter names visible, values redacted (DET-12 ⛨ across
	// surfaces: the summary shows argument names only).
	for _, p := range argumentParameters(wf) {
		b.WriteString("argument: " + shared.Sanitize(p.name) + "=[REDACTED]\n")
	}
	return b.String()
}

func phaseName(p string) string {
	if p == "" {
		return "not started"
	}
	return p
}

type paramRef struct{ name string }

// argumentParameters extracts spec.arguments.parameters names from the raw
// resource payload. Decode failures are silent here: the resource view
// surfaces protocol problems; the summary degrades gracefully.
func argumentParameters(wf core.Workflow) []paramRef {
	if len(wf.Resource) == 0 {
		return nil
	}
	var doc struct {
		Spec struct {
			Arguments struct {
				Parameters []struct {
					Name string `json:"name"`
				} `json:"parameters"`
			} `json:"arguments"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(wf.Resource, &doc); err != nil {
		return nil
	}
	out := make([]paramRef, 0, len(doc.Spec.Arguments.Parameters))
	for _, p := range doc.Spec.Arguments.Parameters {
		if p.Name != "" {
			out = append(out, paramRef{name: p.Name})
		}
	}
	return out
}

// humanDuration renders a duration in coarse human units, deterministically
// (seconds < 1m, minutes < 1h, hours < 24h, otherwise days).
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d / (24 * time.Hour))
		h := int((d % (24 * time.Hour)) / time.Hour)
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

// NodeLogIntent describes what log actions are available for an outline
// row (DET-11 pod-name discipline).
type NodeLogIntent struct {
	// WorkflowWideAllowed is always true: workflow-wide logs never require
	// a pod name (plan §5).
	WorkflowWideAllowed bool
	// PodScopedAllowed is true only when the node has pod potential AND a
	// verified pod name was supplied (user-entered in v0.1). It is never
	// derived from the node ID (no pod-name guessing).
	PodScopedAllowed bool
	// PodName is the verified pod name to use, verbatim from the caller;
	// empty when pod-scoped logs are unavailable.
	PodName string
	// DisabledReason explains why the node shortcut is disabled — the view
	// must show this explanation, never silently hide it.
	DisabledReason string
}

// NodeLogIntent computes the log intent for an outline row. userPodName is
// the user-supplied/verified pod name ("" when none).
func ComputeNodeLogIntent(wf core.Workflow, row OutlineRow, userPodName string) NodeLogIntent {
	intent := NodeLogIntent{WorkflowWideAllowed: true}
	if !row.HasPod {
		intent.DisabledReason = fmt.Sprintf(
			"node %q is type %s; no pod runs for structural nodes — use workflow-wide logs",
			row.NodeID, nodeTypeLabel(row.Type))
		return intent
	}
	if userPodName == "" {
		intent.DisabledReason = "pod name resolution is not verified for this server version; " +
			"enter a pod name explicitly or use workflow-wide logs"
		return intent
	}
	intent.PodScopedAllowed = true
	intent.PodName = userPodName
	return intent
}

func nodeTypeLabel(t string) string {
	if t == "" {
		return "unknown"
	}
	return t
}
