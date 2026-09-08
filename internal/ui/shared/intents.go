package shared

import "argo-tui/internal/core"

// OpenWorkflowMsg asks the root to load a workflow detail.
type OpenWorkflowMsg struct{ Ref core.Ref }

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
