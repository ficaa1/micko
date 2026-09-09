package diagnostics

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitIsAllowlistedAndStructured(t *testing.T) {
	var b bytes.Buffer
	New(&b).Emit(StageAction, "accepted", 2, true, 0, "")
	var e Event
	if err := json.Unmarshal(b.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageAction || e.State != "accepted" || e.Retry != 2 || !e.Uncertain {
		t.Fatalf("unexpected event: %+v", e)
	}
	if strings.Contains(b.String(), "credential") {
		t.Fatal("unsafe field emitted")
	}
}

func TestEmitRejectsUnknownOrSensitiveValues(t *testing.T) {
	var b bytes.Buffer
	s := New(&b)
	s.Emit(Stage("arbitrary"), "connected", 0, false, 0, "")
	s.Emit(StageConnect, "token", 0, false, 0, "")
	s.Emit(StageConnect, "connected", 0, false, 0, "secret")
	if b.Len() != 0 {
		t.Fatalf("rejected values emitted: %q", b.String())
	}
}

func TestNilSinkAndNegativeValuesAreSafe(t *testing.T) {
	New(nil).Emit(StageConnect, "connected", -1, false, 0, "")
	var b bytes.Buffer
	New(&b).Emit(StageConnect, "connected", 0, false, -1, "")
	if b.Len() != 0 {
		t.Fatal("invalid event emitted")
	}
}
