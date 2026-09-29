package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// ---------------------------------------------------------------------------
// golden tests — deterministic render snapshots for the standard
// terminal cases. Regenerate with:
//
//	UPDATE_GOLDEN=1 go test ./internal/ui/logs -run TestGoldenSnapshots
//
// Goldens pin exactly what reaches the terminal: no color (SetNoColor),
// fixture-epoch timestamps only in ReceivedAt (never rendered).
// ---------------------------------------------------------------------------

func golden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden missing (%s); run UPDATE_GOLDEN=1 once and review: %v", path, err)
		}
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func goldenTestcases() map[string]func(*Model) {
	return map[string]func(*Model){
		// Fresh viewer, no stream attached yet.
		"idle": func(m *Model) { m.SetPhase(PhaseIdle) },
		// A short finished stream: two lines, then end-of-stream.
		"stream-ends": func(m *Model) {
			m.ApplyRecords([]core.LogRecord{
				{PodName: "pod-1", Container: "main", Content: "step: starting", ReceivedAt: testkit.FixtureEpoch},
				{PodName: "pod-1", Container: "main", Content: "step: done", ReceivedAt: testkit.FixtureEpoch},
			})
			m.ApplyMarker(markEnd, "pod-1", "main")
			m.SetPhase(PhaseEnded)
		},
		// Paused viewport (Space).
		"paused": func(m *Model) {
			m.ApplyRecords([]core.LogRecord{rec("line one"), rec("line two")})
			press(m, ' ')
		},
		// Honest unavailability.
		"unavailable": func(m *Model) {
			m.ApplyRecords([]core.LogRecord{rec("partial output before failure")})
			m.SetError("pod logs unavailable: pod deleted (log source gone)")
		},
		// Canceled stream (navigating away or context switch).
		"canceled": func(m *Model) {
			m.ApplyRecords([]core.LogRecord{rec("so far, so good")})
			m.ApplyMarker(markCanceled, "pod-1", "main")
			m.SetPhase(PhaseCanceled)
		},
		// Truncation marker visible on an oversized line.
		"oversize": func(m *Model) {
			long := strings.Repeat("x", maxLineBytes+42)
			m.ApplyRecords([]core.LogRecord{rec(long)})
			m.SetPhase(PhaseEnded)
		},
		// Pod-scoped context (non-default pod/container visible).
		"pod-scoped": func(m *Model) {
			m.podName = "mypod-abc"
			m.container = "sidecar"
			m.labels = false // NewModel's default for one pod's log
			m.headerDone = false
			m.ApplyRecords([]core.LogRecord{rec("sidecar says hi")})
		},
		// Committed search with match overlay + visible scope.
		"search": func(m *Model) {
			m.ApplyRecords(recs(6))
			press(m, '/')
			typeInto(m, "line-2")
			pressKey(m, keyEnter)
		},
		// Context editor open with prefilled editable default.
		"context-editor": func(m *Model) {
			m.ApplyRecords([]core.LogRecord{rec("buffered while editing")})
			press(m, 'c')
		},
	}
}

func TestGoldenSnapshots(t *testing.T) {
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, setup := range goldenTestcases() {
		t.Run(name, func(t *testing.T) {
			m := testModel(t)
			setup(m)
			got := m.View()
			golden(t, filepath.Join(dir, name+".golden"), got)
		})
	}
}
