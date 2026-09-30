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
	var s *Sink
	s.Emit(StageConnect, "connected", 0, false, 0, "")
}
