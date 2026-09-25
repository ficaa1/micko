// Package app holds the root Bubble Tea model: routing, async command
// orchestration, cancellation and generation-based stale-message discard
// (plan §4: "only the root Tea update loop mutates UI state").
//
// Child view models (B1/C1/D1) are child Tea models that accept core values,
// never HTTP clients; intents (Open/Back/Action) live in this contract and
// the root alone converts them to network effects (plan §4).
package app

import (
	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// Route identifies the active top-level route. F1 implements empty routes
// without worker feature logic (plan §8 F1 slice 5).
type Route int

const (
	// RouteList is the workflow list (home).
	RouteList Route = iota
	// RouteDetail is the selected workflow detail.
	RouteDetail
	// RouteLogs is the log view for the selected workflow.
	RouteLogs
	// RouteCron is the cron workflow list.
	RouteCron
	// RouteTemplates is the workflow template list.
	RouteTemplates
	// RouteClusterTemplates is the cluster workflow template list.
	RouteClusterTemplates
	// RouteArchived is the archived workflow list.
	RouteArchived
)

// String implements fmt.Stringer.
func (r Route) String() string {
	switch r {
	case RouteDetail:
		return "detail"
	case RouteLogs:
		return "logs"
	case RouteCron:
		return "cron"
	case RouteTemplates:
		return "templates"
	case RouteClusterTemplates:
		return "clustertemplates"
	case RouteArchived:
		return "archived"
	default:
		return "list"
	}
}

// OpenWorkflowMsg is an intent from the list view to open a workflow
// detail. The root converts it to a fetch effect; the child never starts
// goroutines (plan §4).
type OpenWorkflowMsg = shared.OpenWorkflowMsg

// OpenLogsMsg is an intent to open logs. PodName empty means workflow-wide
// logs; Container is always explicit (default "main" visible in the UI,
// plan §2).
type OpenLogsMsg = shared.OpenLogsMsg

// BackMsg is an intent to go back one route (Esc).
type BackMsg struct{}

// ActionIntentMsg is the frozen v0.2 action intent placeholder. Alpha and
// demo never emit it; the root will refuse to convert it to a write unless
// --allow-actions is set on that invocation (plan §6; not implemented in
// F1 — the message exists so the shared contract is stable).
type ActionIntentMsg struct {
	Ref    core.Ref
	Action string // "retry" | "resubmit" | "stop" | "terminate"
}

// genStamp carries the two generation counters every response message must
// include (plan §4: "Root response messages include (connectionGeneration,
// selectionGeneration, requestID)").
type genStamp struct {
	Conn    int
	Sel     int
	Attempt uint64
}

// listLoadedMsg carries one collected page of the list snapshot.
type listLoadedMsg struct {
	genStamp
	RequestID uint64
	Page      core.Page
	// Done: no continuation token, snapshot collection finished.
	Done bool
	// Capped: the snapshot cap was reached; result is explicitly incomplete.
	Capped bool
	// Canceled: the operation was superseded/canceled (root discards).
	Canceled bool
	Err      *core.APIError
}

// detailLoadedMsg carries one detail fetch result.
type detailLoadedMsg struct {
	genStamp
	RequestID uint64
	Ref       core.Ref
	Workflow  core.Workflow
	Canceled  bool
	Err       *core.APIError
}

// logRecordMsg is one drained batch of log records (root batches delivery
// instead of one full render per line, plan §5).
type logRecordMsg struct {
	genStamp
	RequestID uint64
	Records   []core.LogRecord
	// Done: stream finished (clean EOF or error below).
	Done bool
	// Canceled distinguishes cancellation from network failure (plan §4).
	Canceled bool
	Err      error
}

// logSourcesMsg carries the pod-to-step map for a workflow-wide log pane.
// Sources is nil when the workflow could not be read.
type logSourcesMsg struct {
	genStamp
	RequestID uint64
	Ref       core.Ref
	Sources   map[string]string
}

// tickMsg schedules the next poll only after the previous collection
// completes (plan §5: "start the next timer after completion, not
// concurrent ticker launches").
type tickMsg struct{}
