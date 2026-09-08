package actions

import "argo-tui/internal/core"

// ConfirmAction validates the UI's opt-in confirmation without performing an
// API call. It is useful to roots that keep confirmation orchestration outside
// the child model.
func ConfirmAction(req core.ActionRequest, actual core.Ref) error { return req.Validate(actual) }

// DisplayTarget is the exact human-readable identity shown before submit.
func DisplayTarget(ref core.Ref) string { return target(ref) }
