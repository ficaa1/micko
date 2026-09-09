package actions

import "argo-tui/internal/core"

// Options makes the safety posture explicit for struct-based callers.
type Options struct {
	AllowActions bool
	ReadOnly     bool
	Demo         bool
	Server       string
	Profile      string
	Phase        string
}

// NewWithOptions constructs a guarded action model from named safety flags.
func NewWithOptions(ref core.Ref, options Options) *Model {
	m := New(ref, options.AllowActions, options.ReadOnly, options.Demo)
	m.server, m.profile, m.phase = options.Server, options.Profile, options.Phase
	return m
}
