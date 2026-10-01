package diagnostics

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

// Emit writes one JSON line of allowlisted lifecycle facts and drops any
// event that carries an unknown stage, caller text or a negative count.
func TestEmit(t *testing.T) {
	type call struct {
		stage     Stage
		state     string
		retry     int
		uncertain bool
		status    int
		category  string
	}
	cases := []struct {
		name string
		call call
		want *Event
	}{
		{"lifecycle event", call{StageForward, "lost", 2, true, 0, ""},
			&Event{Stage: StageForward, State: "lost", Retry: 2, Uncertain: true}},
		{"request status", call{StageRequest, "failed", 0, false, 503, "unavailable"},
			&Event{Stage: StageRequest, State: "failed", Status: 503, ErrorCategory: "unavailable"}},
		{"unknown stage", call{Stage("arbitrary"), "connected", 0, false, 0, ""}, nil},
		{"state naming a token", call{StageConnect, "token", 0, false, 0, ""}, nil},
		{"category naming a secret", call{StageConnect, "connected", 0, false, 0, "secret"}, nil},
		{"free text state", call{StageConnect, "Bearer abc", 0, false, 0, ""}, nil},
		{"capitalised state", call{StageConnect, "Connected", 0, false, 0, ""}, nil},
		{"negative retry", call{StageConnect, "connected", -1, false, 0, ""}, nil},
		{"negative status", call{StageConnect, "connected", 0, false, -1, ""}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			New(&b).Emit(c.call.stage, c.call.state, c.call.retry, c.call.uncertain, c.call.status, c.call.category)
			if c.want == nil {
				if b.Len() != 0 {
					t.Fatalf("emitted %q, want nothing", b.String())
				}
				return
			}
			var got Event
			if err := json.Unmarshal(b.Bytes(), &got); err != nil {
				t.Fatalf("output %q is not one JSON event: %v", b.String(), err)
			}
			if got.Time.IsZero() {
				t.Error("the event has no timestamp")
			}
			got.Time = time.Time{}
			if got != *c.want {
				t.Errorf("event = %+v, want %+v", got, *c.want)
			}
		})
	}
}

// A sink with no writer drops every event instead of failing.
func TestSinkWithoutAWriterIsSilent(t *testing.T) {
	New(nil).Emit(StageConnect, "connected", 0, false, 0, "")
	New(nil).EmitRequest(Request{Endpoint: "list"})
	var s *Sink
	s.Emit(StageConnect, "connected", 0, false, 0, "")
	s.EmitRequest(Request{Endpoint: "list"})
	s.EmitSpan("detail_open", time.Second, false)
}

// EmitRequest writes one timing line and drops a timing whose endpoint is
// caller text or whose counts are negative.
func TestEmitRequest(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want *Event
	}{
		{"new connection", Request{Endpoint: "list", Status: 200, Connect: 1500 * time.Microsecond, TTFB: 20 * time.Millisecond, Total: 25 * time.Millisecond, Bytes: 512},
			&Event{Stage: StageRequest, State: "ok", Status: 200, Endpoint: "list", Conn: "new", ConnectMS: 1.5, TTFBMS: 20, TotalMS: 25, Bytes: 512}},
		{"reused connection", Request{Endpoint: "gate", Status: 200, Reused: true, TTFB: time.Millisecond, Total: time.Millisecond},
			&Event{Stage: StageRequest, State: "ok", Status: 200, Endpoint: "gate", Conn: "reused", TTFBMS: 1, TotalMS: 1}},
		{"failed request", Request{Endpoint: "get", Failed: true, Total: 3 * time.Second},
			&Event{Stage: StageRequest, State: "failed", Endpoint: "get", Conn: "new", TotalMS: 3000}},
		{"no endpoint", Request{Status: 200}, nil},
		{"path as endpoint", Request{Endpoint: "/api/v1/workflows/team-a"}, nil},
		{"endpoint naming a token", Request{Endpoint: "token"}, nil},
		{"negative bytes", Request{Endpoint: "list", Bytes: -1}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			New(&b).EmitRequest(c.req)
			if c.want == nil {
				if b.Len() != 0 {
					t.Fatalf("emitted %q, want nothing", b.String())
				}
				return
			}
			var got Event
			if err := json.Unmarshal(b.Bytes(), &got); err != nil {
				t.Fatalf("output %q is not one JSON event: %v", b.String(), err)
			}
			got.Time = time.Time{}
			if got != *c.want {
				t.Errorf("event = %+v, want %+v", got, *c.want)
			}
		})
	}
}

// EmitSpan writes one span line and drops a span whose name is caller text
// or whose duration is negative.
func TestEmitSpan(t *testing.T) {
	cases := []struct {
		name   string
		span   string
		d      time.Duration
		failed bool
		want   *Event
	}{
		{"answered", "detail_open", 1500 * time.Microsecond, false, &Event{Stage: StageSpan, State: "ok", Span: "detail_open", TotalMS: 1.5}},
		{"failed", "first_list", 2 * time.Second, true, &Event{Stage: StageSpan, State: "failed", Span: "first_list", TotalMS: 2000}},
		{"one word", "allns", time.Millisecond, false, &Event{Stage: StageSpan, State: "ok", Span: "allns", TotalMS: 1}},
		{"no name", "", time.Millisecond, false, nil},
		{"spaces", "detail open", time.Millisecond, false, nil},
		{"capitals", "Detail_open", time.Millisecond, false, nil},
		{"leading underscore", "_open", time.Millisecond, false, nil},
		{"name with a token", "token_open", time.Millisecond, false, nil},
		{"negative duration", "detail_open", -time.Millisecond, false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			New(&b).EmitSpan(c.span, c.d, c.failed)
			if c.want == nil {
				if b.Len() != 0 {
					t.Fatalf("emitted %q, want nothing", b.String())
				}
				return
			}
			var got Event
			if err := json.Unmarshal(b.Bytes(), &got); err != nil {
				t.Fatalf("output %q is not one JSON event: %v", b.String(), err)
			}
			got.Time = time.Time{}
			if got != *c.want {
				t.Errorf("event = %+v, want %+v", got, *c.want)
			}
		})
	}
}
