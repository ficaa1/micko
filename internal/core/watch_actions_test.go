package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWatchContractCarriesOpaqueCursorAndEventIdentity(t *testing.T) {
	var watcher Watcher = watchFunc(func(ctx context.Context, req WatchRequest, emit func(WatchEvent) error) error {
		if req.ResourceVersion != "00000000000000000042" {
			t.Fatalf("resource version changed: %q", req.ResourceVersion)
		}
		return emit(WatchEvent{Type: WatchAdded, ResourceVersion: req.ResourceVersion, Summary: Summary{Ref: Ref{Namespace: "ns", Name: "wf", UID: "uid"}}})
	})
	var got WatchEvent
	err := watcher.Watch(context.Background(), WatchRequest{Namespace: "ns", ResourceVersion: "00000000000000000042"}, func(event WatchEvent) error { got = event; return nil })
	if err != nil || got.Type != WatchAdded || got.Summary.Ref.UID != "uid" {
		t.Fatalf("watch result = %#v, %v", got, err)
	}
}

func TestWatchCursorExpiredIsDistinguishable(t *testing.T) {
	err := NewWatchError(WatchExpired, "cursor expired", "rv-7", nil)
	var watchErr *WatchError
	if !errors.As(err, &watchErr) || watchErr.Kind != WatchExpired || watchErr.LastResourceVersion != "rv-7" {
		t.Fatalf("error = %#v", err)
	}
}

func TestActionRequestRequiresConfirmationAndMatchingUID(t *testing.T) {
	req := ActionRequest{Action: ActionTerminate, Ref: Ref{Namespace: "ns", Name: "wf", UID: "old"}, Confirmation: Confirmation{Confirmed: true, TypedName: "wf"}}
	if err := req.Validate(Ref{Namespace: "ns", Name: "wf", UID: "new"}); err == nil {
		t.Fatal("expected UID mismatch")
	}
	req.Ref.UID = "new"
	req.Confirmation.Confirmed = false
	if err := req.Validate(req.Ref); !errors.Is(err, ErrActionNotConfirmed) {
		t.Fatalf("cancel-default validation error = %v", err)
	}
}

func TestTerminateRequiresTypedWorkflowName(t *testing.T) {
	req := ActionRequest{Action: ActionTerminate, Ref: Ref{Namespace: "ns", Name: "wf", UID: "uid"}, Confirmation: Confirmation{Confirmed: true, TypedName: "other"}}
	if err := req.Validate(req.Ref); !errors.Is(err, ErrConfirmationNameMismatch) {
		t.Fatalf("typed-name validation error = %v", err)
	}
}

func TestActionWireBodiesMatchPinnedSchemasAndExcludeUID(t *testing.T) {
	ref := Ref{Namespace: "ns", Name: "wf", UID: "secret-uid"}
	cases := []struct {
		name   string
		action Action
		want   string
	}{
		{"retry", ActionRetry, `{"name":"wf","namespace":"ns","nodeFieldSelector":"phase=Failed","parameters":["x=y"],"restartSuccessful":true}`},
		{"resubmit", ActionResubmit, `{"memoized":true,"name":"wf","namespace":"ns","parameters":["x=y"]}`},
		{"stop", ActionStop, `{"message":"operator stop","name":"wf","namespace":"ns","nodeFieldSelector":"phase=Failed"}`},
		{"terminate", ActionTerminate, `{"name":"wf","namespace":"ns"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := ActionRequest{Ref: ref, Action: tc.action, NodeFieldSelector: "phase=Failed", RestartSuccessful: true, Memoized: true, Parameters: []string{"x=y"}, Message: "operator stop"}
			body, err := json.Marshal(req.WireBody())
			if err != nil || string(body) != tc.want {
				t.Fatalf("body = %s, err = %v; want %s", body, err, tc.want)
			}
			if strings.Contains(string(body), "secret-uid") {
				t.Fatal("UID leaked into action body")
			}
		})
	}
}

type watchFunc func(context.Context, WatchRequest, func(WatchEvent) error) error

func (f watchFunc) Watch(ctx context.Context, req WatchRequest, emit func(WatchEvent) error) error {
	return f(ctx, req, emit)
}
