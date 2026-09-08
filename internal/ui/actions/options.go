package actions

import "argo-tui/internal/core"

// Options makes the safety posture explicit for struct-based callers.
type Options struct {
	AllowActions bool
	ReadOnly     bool
	Demo         bool
}

// NewWithOptions constructs a guarded action model from named safety flags.
func NewWithOptions(ref core.Ref, options Options) *Model {
	return New(ref, options.AllowActions, options.ReadOnly, options.Demo)
}
