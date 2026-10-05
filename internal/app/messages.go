// Package app holds the root Bubble Tea model. Only its update loop changes
// UI state: child views emit intents, and the root turns them into requests
// and drops stale replies by generation.
package app

import (
	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Route identifies the active top-level route.
type Route int

const (
	// RouteList is the workflow list, the home route.
	RouteList Route = iota
	// RouteDetail is the selected workflow's detail.
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

// OpenWorkflowMsg asks the root to open a workflow's detail.
type OpenWorkflowMsg = shared.OpenWorkflowMsg

// OpenLogsMsg asks the root to open logs. An empty PodName means the whole
// workflow.
type OpenLogsMsg = shared.OpenLogsMsg

// BackMsg is an intent to go back one route (Esc).
type BackMsg struct{}

// genStamp carries the generations and attempt a request was made under, so
// its reply can be checked for staleness.
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
	// Done means the snapshot is complete.
	Done bool
	// Capped means the snapshot cap was reached and the result is incomplete.
	Capped bool
	// Canceled means the request was superseded.
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

// logRecordMsg is one batch of log records.
type logRecordMsg struct {
	genStamp
	RequestID uint64
	Records   []core.LogRecord
	// Done means the stream ended, cleanly or with Err.
	Done bool
	// Canceled tells cancellation apart from a network failure.
	Canceled bool
	Err      error
	// Next runs as a separate command to avoid nesting drains.
	Next tea.Cmd
}

// logSourcesMsg carries the pod-to-step map for a workflow-wide log pane.
// Sources is nil when the workflow could not be read.
type logSourcesMsg struct {
	genStamp
	RequestID uint64
	Ref       core.Ref
	Sources   map[string]string
}

// tickMsg triggers the next poll.
type tickMsg struct{}
