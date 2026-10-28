package argo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

func TestResourceWriteSafety(t *testing.T) {
	for _, c := range []struct {
		name      string
		confirmed bool
		status    int
		answer    string
		want      core.ActionOutcome
	}{
		{"unconfirmed", false, 200, `{}`, core.ActionRefused},
		{"empty identity", true, 200, `{}`, core.ActionUnknown},
		{"wrong namespace", true, 200, `{"metadata":{"name":"new","namespace":"wrong","uid":"u"}}`, core.ActionUnknown},
		{"truncated", true, 200, `{"metadata":`, core.ActionUnknown},
		{"unauthorized", true, 401, `{"message":"secret"}`, core.ActionUnknown},
		{"forbidden", true, 403, `{"message":"secret"}`, core.ActionUnknown},
		{"unavailable", true, 503, `{}`, core.ActionUnknown},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(c.status)
				io.WriteString(w, c.answer)
			})
			client := newTestClient(t, srv)
			req := core.ResourceActionRequest{Resource: core.ResourceState{Kind: core.KindClusterWorkflowTemplate, Ref: core.Ref{Name: "template", UID: "uid"}}, Namespace: "ns", Action: core.ActionSubmit, Confirmation: core.Confirmation{Confirmed: c.confirmed}}
			result, err := client.ExecuteResourceAction(context.Background(), req)
			if err == nil || result.Outcome != c.want || strings.Contains(err.Error(), "secret") {
				t.Fatalf("outcome=%s err=%v", result.Outcome, err)
			}
			want := 1
			if !c.confirmed {
				want = 0
			}
			if calls != want {
				t.Fatalf("sent %d requests, want %d", calls, want)
			}
		})
	}
	t.Run("lost response", func(t *testing.T) {
		calls := 0
		client := newTestClient(t, serve(t, func(http.ResponseWriter, *http.Request) {}))
		client.http.Transport = resourceRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("lost") })
		result, err := client.ExecuteResourceAction(context.Background(), core.ResourceActionRequest{Resource: core.ResourceState{Kind: core.KindWorkflowTemplate, Ref: core.Ref{Namespace: "ns", Name: "template", UID: "uid"}}, Namespace: "ns", Action: core.ActionSubmit, Confirmation: core.Confirmation{Confirmed: true}})
		if err == nil || result.Outcome != core.ActionUnknown || calls != 1 {
			t.Fatalf("outcome=%s calls=%d err=%v", result.Outcome, calls, err)
		}
	})
}

type resourceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f resourceRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
