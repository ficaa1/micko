package testkit

import (
	"context"
	"errors"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
)

func TestFakeActionerDoesNotRecordCanceledConfirmation(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "uid"}
	fake := &FakeActioner{AllowActions: true, CurrentRef: ref}
	_, err := fake.Execute(context.Background(), core.ActionRequest{Ref: ref, Action: core.ActionStop})
	if !errors.Is(err, core.ErrActionNotConfirmed) {
		t.Fatalf("error = %v", err)
	}
	if len(fake.Requests) != 0 {
		t.Fatalf("canceled request was recorded: %d", len(fake.Requests))
	}
}

func TestFakeActionerModelsUIDMismatchAndAmbiguousOutcome(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "wf", UID: "old"}
	fake := &FakeActioner{AllowActions: true, CurrentRef: core.Ref{Namespace: "ns", Name: "wf", UID: "new"}}
	req := core.ActionRequest{Ref: ref, Action: core.ActionRetry, Confirmation: core.Confirmation{Confirmed: true}}
	if _, err := fake.Execute(context.Background(), req); err == nil {
		t.Fatal("expected UID mismatch")
	}
	fake.CurrentRef = ref
	fake.ActionErr = errors.New("connection reset after send")
	fake.Result = core.ActionResult{Outcome: core.ActionUnknown}
	result, err := fake.Execute(context.Background(), req)
	if result.Outcome != core.ActionUnknown || err == nil || len(fake.Requests) != 1 {
		t.Fatalf("ambiguous result = %#v, err=%v, requests=%d", result, err, len(fake.Requests))
	}
}

func TestFakeWatcherPreservesOpaqueCursor(t *testing.T) {
	fake := &FakeWatcher{Events: []core.WatchEvent{{Type: "FUTURE", ResourceVersion: "rv-2"}}}
	var got core.WatchEvent
	if err := fake.Watch(context.Background(), core.WatchRequest{Namespace: "ns", ResourceVersion: "rv-1"}, func(event core.WatchEvent) error { got = event; return nil }); err != nil {
		t.Fatal(err)
	}
	if got.Type != "FUTURE" || len(fake.Requests) != 1 || fake.Requests[0].ResourceVersion != "rv-1" {
		t.Fatalf("event/request = %#v / %#v", got, fake.Requests)
	}
}
