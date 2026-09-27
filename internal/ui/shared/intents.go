package shared

import "github.com/ficaa1/micko/internal/core"

// OpenWorkflowMsg asks the root to load a workflow detail. Section names the
// detail section to open it on; empty keeps the section the pane last
// showed.
type OpenWorkflowMsg struct {
	Ref     core.Ref
	Section string
}

// The detail sections a workflow list key opens a workflow straight onto,
// by the IDs the detail pane knows them by.
const (
	SectionTimeline = "timeline"
	SectionExplain  = "explain"
	SectionEvents   = "events"
)

// OpenLogsMsg asks the root to attach a log stream.
type OpenLogsMsg struct {
	Ref       core.Ref
	PodName   string
	Container string
}

// SwitchContextIntent asks the root to restart logs for a new context.
type SwitchContextIntent struct {
	Ref       core.Ref
	PodName   string
	Container string
}
