package detail

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// RenderSummary renders the one-screen workflow summary: name,
// namespace, phase, age, duration, message, labels and argument names
// (values are redacted — names only, never values).
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

	// Duration rule: finished workflows measure
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

	// Argument parameter names visible, values redacted (the
	// summary shows argument names only).
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
