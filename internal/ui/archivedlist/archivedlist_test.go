package archivedlist

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/shared"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func run(name, uid, phase string, ago, dur time.Duration, msg string) core.Workflow {
	start := now.Add(-ago)
	end := start.Add(dur)
	return core.Workflow{
		Summary: core.Summary{
			Ref:   core.Ref{Namespace: "ns", Name: name, UID: uid},
			Phase: phase, Message: msg, CreatedAt: start, StartedAt: &start, FinishedAt: &end,
			Labels: map[string]string{"workflows.argoproj.io/cron-workflow": "etl"},
		},
		Resource: []byte(`{"metadata":{"name":"` + name + `","uid":"` + uid + `"},"status":{"phase":"` + phase + `"}}`),
	}
}

// The columns are the workflow list's, and each cell reads the same.
func TestCells(t *testing.T) {
	w := run("etl-1", "u1", "Failed", 26*time.Hour, 5*time.Minute, "child 'check' failed")
	checks := map[string]string{"name": "etl-1", "phase": "✗ Failed", "age": "1d2h", "duration": "5m", "message": "child 'check' failed"}
	for col, want := range checks {
		if got := cell(w, col, now); got != want {
			t.Errorf("%s = %q, want %q", col, got, want)
		}
	}
	w.Summary.FinishedAt = nil
	if got := cell(w, "duration", now); got != "unfinished" {
		t.Errorf("duration without an end = %q", got)
	}
	w.Summary.StartedAt, w.Summary.CreatedAt = nil, time.Time{}
	if cell(w, "age", now) != "-" || cell(w, "duration", now) != "-" {
		t.Errorf("no timestamps: age %q duration %q", cell(w, "age", now), cell(w, "duration", now))
	}
}

// Two archived runs of one name are two rows, and the cursor follows the
// UID; the default order is newest first; enter opens the selected run.
func TestIdentityOrderAndOpen(t *testing.T) {
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetSize(120, 20)
	m.SetItems([]core.Workflow{
		run("etl", "old", "Succeeded", 48*time.Hour, time.Minute, ""),
		run("etl", "new", "Failed", 24*time.Hour, time.Minute, "boom"),
	}, now)
	rows := m.Rows()
	if len(rows) != 2 || rows[0].Summary.Ref.UID != "new" {
		t.Fatalf("rows = %+v", rows)
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	sel, _ := m.Selected()
	if sel.Summary.Ref.UID != "old" {
		t.Fatalf("j selected %q", sel.Summary.Ref.UID)
	}
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	open, ok := cmd().(OpenMsg)
	if !ok || open.Ref.UID != "old" {
		t.Fatalf("enter = %#v", cmd())
	}
	if !strings.Contains(m.Hints(), "enter open") {
		t.Fatalf("hints = %q", m.Hints())
	}
}

// The layout fits every width and keeps MESSAGE from 80 cells.
func TestColumnsFit(t *testing.T) {
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetItems([]core.Workflow{run("a-long-archived-workflow-name-from-a-generator-x7k2p", "u", "Failed", time.Hour, time.Minute, strings.Repeat("why ", 40))}, now)
	for _, w := range []int{60, 76, 80, 100, 136} {
		m.SetSize(w, 10)
		lines := m.BodyLines(now)
		for _, l := range lines {
			if n := ansi.StringWidth(ansi.Strip(l)); n > w {
				t.Fatalf("width %d: %d cells: %q", w, n, l)
			}
		}
		head := ansi.Strip(lines[1])
		if (w >= 80) != strings.Contains(head, "MESSAGE") {
			t.Errorf("width %d head %q", w, head)
		}
	}
}

// The panel says where the record comes from and that actions do not apply.
func TestInfo(t *testing.T) {
	w := run("etl-1", "u1", "Failed", 26*time.Hour, 5*time.Minute, "child 'check' failed")
	w.Summary.Progress = "1/2"
	var b strings.Builder
	for _, f := range info(w, false, now) {
		b.WriteString(f.Label + " | " + f.Value + "\n")
	}
	got := b.String()
	for _, want := range []string{"Source | the workflow archive", "actions do not apply", "Phase | ✗ Failed",
		"Message | child 'check' failed", "Duration | 5m", "Progress | 1/2", "UID | u1",
		"Labels | workflows.argoproj.io/cron-workflow=etl"} {
		if !strings.Contains(got, want) {
			t.Errorf("panel lacks %q:\n%s", want, got)
		}
	}
}

// An empty archive says that a server without one answers the same way.
func TestEmptyNote(t *testing.T) {
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetSize(140, 10)
	m.SetItems(nil, now)
	m.SetStatus(kindlist.StatusIdle, "", 0)
	if out := strings.Join(m.BodyLines(now), "\n"); !strings.Contains(out, "no workflow archive configured answers the same way") {
		t.Fatalf("empty state:\n%s", out)
	}
}
