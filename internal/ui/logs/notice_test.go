package logs

import (
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// A notice explains an empty stream, wrapped to the pane, and disappears as
// soon as a line is retained; an error takes its place on the status line.
func TestNoticeOnlyWhileEmpty(t *testing.T) {
	m := NewModel(testRef(), "pod-1", "main")
	m.SetSize(40, 20)
	m.SetPhase(PhaseEnded)
	m.SetNotice("no log lines: the pods of this run are gone and nothing was archived")
	body := strings.Join(m.BodyLines(), "\n")
	if !strings.Contains(body, "no log lines: the pods of this run are") || !strings.Contains(body, "archived") {
		t.Fatalf("notice missing:\n%s", body)
	}
	for _, l := range m.BodyLines()[1:3] {
		if len(l) > 40 {
			t.Fatalf("notice not wrapped: %q", l)
		}
	}
	m.ApplyRecords([]core.LogRecord{{PodName: "pod-1", Container: "main", Content: "hello"}})
	if strings.Contains(strings.Join(m.BodyLines(), "\n"), "no log lines") {
		t.Fatal("the notice stayed after a line arrived")
	}

	e := NewModel(testRef(), "pod-1", "main")
	e.SetNotice("no log lines")
	e.SetError("stream failed")
	if strings.Contains(strings.Join(e.BodyLines(), "\n"), "no log lines") {
		t.Fatal("the notice was shown beside an error")
	}
}
