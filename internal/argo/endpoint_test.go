package argo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ficaa1/argo-tui/internal/core"
)

// A managed endpoint is a local forwarded port, and the port is released as
// soon as the forward drops. The operating system may then give it to any
// other program, so a request that cannot resolve an endpoint must fail
// rather than fall back to the port the client started on. Falling back
// sends the bearer token to whatever now listens there.
func TestAnUnresolvableEndpointFailsInsteadOfUsingTheOldPort(t *testing.T) {
	var reached int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	endpoint := srv.URL
	c, err := NewClient(Options{
		Server:        srv.URL,
		ResolveServer: func() string { return endpoint },
		TokenFn:       func() (string, error) { return "t", nil },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := c.List(context.Background(), core.Query{Namespace: "ns"}); err != nil {
		t.Fatalf("list with a live endpoint: %v", err)
	}

	// The forward drops: the resolver can no longer name an endpoint.
	endpoint = ""
	before := reached
	_, err = c.List(context.Background(), core.Query{Namespace: "ns"})
	if err == nil {
		t.Fatal("a list with no endpoint succeeded; it fell back to the old port")
	}
	if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrUnavailable {
		t.Fatalf("error = %v, want an unavailable API error", err)
	}
	if reached != before {
		t.Fatal("the request was sent to the old port")
	}
}

// An action must fail the same way. A PUT that lands on an unrelated local
// process is worse than a read: it carries a body meant to change state.
func TestAnActionRefusesAnUnresolvableEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the action reached a server; it should have been refused")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewClient(Options{
		Server:        srv.URL,
		ResolveServer: func() string { return "" },
		TokenFn:       func() (string, error) { return "t", nil },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = c.Execute(context.Background(), core.ActionRequest{
		Ref:          core.Ref{Namespace: "ns", Name: "wf", UID: "uid-1"},
		Action:       core.ActionResume,
		Confirmation: core.Confirmation{Confirmed: true},
	})
	if err == nil {
		t.Fatal("the action was sent with no endpoint")
	}
	if ae := core.AsAPIError(err); ae == nil || ae.Kind != core.ErrUnavailable {
		t.Fatalf("error = %v, want an unavailable API error", err)
	}
}
