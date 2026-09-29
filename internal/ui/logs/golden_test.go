package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// golden compares got with testdata/<name>.golden. Regenerate with
//
//	UPDATE_GOLDEN=1 go test ./internal/ui/logs -run Golden
//
// and review the diff: a golden is the pane exactly as the shell draws it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run UPDATE_GOLDEN=1 once and review)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// Every lifecycle state and editor, plain. Each golden pins the pane's
// wording for that state: the status badge, the stream header and its
// count, labels, the search scope and position, the editors' prompts, and
// the visible truncation marker.
func TestGoldenSnapshots(t *testing.T) {
	cases := map[string]func() *Model{
		"empty": func() *Model { return testModel(t) },
		"following": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("one"), rec("日本語 ünïcode ✓")})
			return m
		},
		"paused": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("line one"), rec("line two")})
			press(m, ' ')
			return m
		},
		"stream-ends": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("step: starting"), rec("step: done")})
			m.SetPhase(PhaseEnded)
			return m
		},
		"unavailable": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("partial output before failure")})
			m.SetError("pod logs unavailable: pod deleted (log source gone)")
			return m
		},
		"canceled": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("so far, so good")})
			m.SetPhase(PhaseCanceled)
			return m
		},
		"oversize": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec(strings.Repeat("x", maxLineBytes+42))})
			m.SetPhase(PhaseEnded)
			return m
		},
		"pod-scoped": func() *Model {
			m := sizedModel(NewModel(testRef(), "mypod-abc", "sidecar"))
			m.ApplyRecords([]core.LogRecord{podRec("mypod-abc", "sidecar says hi")})
			return m
		},
		"search": func() *Model {
			m := testModel(t)
			m.ApplyRecords(recs(6))
			press(m, '/')
			typeInto(m, "line-2")
			pressKey(m, keyEnter)
			return m
		},
		"context-editor": func() *Model {
			m := testModel(t)
			m.ApplyRecords([]core.LogRecord{rec("buffered while editing")})
			press(m, 'c')
			return m
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) { golden(t, name, pane(build())) })
	}
}
