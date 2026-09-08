package testkit

import (
	"context"
	"sync"
	"time"

	"argo-tui/internal/core"
)

// FakeWatcher is an in-memory watcher for deterministic cursor, cancellation,
// and reconnect tests. It never retries or performs I/O.
type FakeWatcher struct {
	mu         sync.Mutex
	Events     []core.WatchEvent
	WatchErr   error
	WatchDelay time.Duration
	Requests   []core.WatchRequest
}

var _ core.Watcher = (*FakeWatcher)(nil)

func (f *FakeWatcher) Watch(ctx context.Context, req core.WatchRequest, emit func(core.WatchEvent) error) error {
	f.mu.Lock()
	f.Requests = append(f.Requests, req)
	events, watchErr, delay := append([]core.WatchEvent(nil), f.Events...), f.WatchErr, f.WatchDelay
	f.mu.Unlock()
	if delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	} else if err := ctx.Err(); err != nil {
		return err
	}
	if watchErr != nil {
		return watchErr
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(event); err != nil {
			return err
		}
	}
	return nil
}

// FakeActioner records requests and models preflight identity checking. It is
// disabled by default; demo mode is always non-mutating. ActionErr can model
// a timeout/reset after send, while Result can carry ActionUnknown.
type FakeActioner struct {
	mu           sync.Mutex
	AllowActions bool
	Demo         bool
	CurrentRef   core.Ref
	Result       core.ActionResult
	ActionErr    error
	Requests     []core.ActionRequest
}

var _ core.Actioner = (*FakeActioner)(nil)

func (f *FakeActioner) Execute(ctx context.Context, req core.ActionRequest) (core.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ActionResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.AllowActions || f.Demo {
		return core.ActionResult{}, core.ErrActionDisabled
	}
	if err := req.Validate(f.CurrentRef); err != nil {
		return core.ActionResult{}, err
	}
	f.Requests = append(f.Requests, req)
	result := f.Result
	if result.Action == "" {
		result.Action = req.Action
	}
	if result.Target == (core.Ref{}) {
		result.Target = req.Ref
	}
	if result.Outcome == "" {
		result.Outcome = core.ActionConfirmed
	}
	return result, f.ActionErr
}
