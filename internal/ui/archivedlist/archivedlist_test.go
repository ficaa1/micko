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

// pane is the app's archive list at w×h holding items.
func pane(w, h int, items ...core.Workflow) *kindlist.Model[core.Workflow] {
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetSize(w, h)
	m.SetItems(items, now)
	m.SetStatus(kindlist.StatusIdle, "", 0)
	return m
}

// press sends each key in turn, drawing the pane first as the shell does.
func press(m *kindlist.Model[core.Workflow], keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		m.BodyLines(now)
		msg := tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		if k == "enter" {
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
		cmd = m.Update(msg)
	}
	return cmd
}

// lines is the pane's body with styling removed and runs of spaces collapsed.
func lines(m *kindlist.Model[core.Workflow]) []string {
	var out []string
	for _, l := range m.BodyLines(now) {
		out = append(out, strings.Join(strings.Fields(ansi.Strip(l)), " "))
	}
	return out
}

// A row reads like the workflow list's: name, phase, age, duration and
// message, with a dash for a time the record lacks.
func TestRows(t *testing.T) {
	failed := run("etl-1", "u1", "Failed", 26*time.Hour, 5*time.Minute, "child 'check' failed")
	undated := run("etl-3", "u3", "", 0, 0, "")
	undated.Summary.StartedAt, undated.Summary.FinishedAt, undated.Summary.CreatedAt = nil, nil, time.Time{}
	cases := []struct {
		name string
		w    core.Workflow
		want string
	}{
		{"finished", failed, "etl-1 ✗ Failed 1d2h 5m child 'check' failed"},
		{"no timestamps", undated, "etl-3 • (no phase) - -"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lines(pane(120, 10, c.w))[2]; got != c.want {
				t.Errorf("row = %q, want %q", got, c.want)
			}
		})
	}
}

// Two archived runs of one name are two rows, newest first; the cursor
// follows the UID and enter opens the run it is on.
func TestIdentityOrderAndOpen(t *testing.T) {
	m := pane(120, 20,
		run("etl", "old", "Succeeded", 48*time.Hour, time.Minute, ""),
		run("etl", "new", "Failed", 24*time.Hour, time.Minute, "boom"),
	)
	if got := lines(m)[2:4]; got[0] != "etl ✗ Failed 1d 1m boom" || got[1] != "etl ✓ Succeeded 2d 1m" {
		t.Fatalf("rows = %q, want the newer run first", got)
	}
	press(m, "j")
	m.SetItems([]core.Workflow{
		run("etl", "new", "Failed", 24*time.Hour, time.Minute, "boom"),
		run("etl", "old", "Succeeded", 48*time.Hour, time.Minute, ""),
	}, now)
	want := OpenMsg{Ref: core.Ref{Namespace: "ns", Name: "etl", UID: "old"}}
	if got := press(m, "enter")(); got != want {
		t.Errorf("enter = %#v, want %#v", got, want)
	}
	if !strings.HasPrefix(m.Hints(), "enter open") {
		t.Errorf("hints = %q", m.Hints())
	}
}

// s cycles to failures first, then to name order.
func TestSort(t *testing.T) {
	items := []core.Workflow{
		run("b-ok", "1", "Succeeded", time.Hour, time.Minute, ""),
		run("c-failed", "2", "Failed", 3*time.Hour, time.Minute, ""),
		run("a-running", "3", "Running", 2*time.Hour, time.Minute, ""),
	}
	cases := []struct {
		keys []string
		want string
	}{
		{nil, "b-ok,a-running,c-failed"},
		{[]string{"s"}, "c-failed,a-running,b-ok"},
		{[]string{"s", "s"}, "a-running,b-ok,c-failed"},
	}
	for _, c := range cases {
		m := pane(120, 10, items...)
		press(m, c.keys...)
		var got []string
		for _, l := range lines(m)[2:] {
			if f := strings.Fields(l); len(f) > 0 {
				got = append(got, f[0])
			}
		}
		if strings.Join(got, ",") != c.want {
			t.Errorf("keys %v: order %v, want %s", c.keys, got, c.want)
		}
	}
}

// The layout fits every width and adds MESSAGE from 80 cells.
func TestLayout(t *testing.T) {
	w := run("a-long-archived-workflow-name-from-a-generator-x7k2p", "u", "Failed", time.Hour, time.Minute, strings.Repeat("why ", 40))
	cases := []struct {
		width int
		head  string
	}{
		{60, "NAME PHASE AGE DURATION"},
		{76, "NAME PHASE AGE DURATION"},
		{80, "NAME PHASE AGE DURATION MESSAGE"},
		{136, "NAME PHASE AGE DURATION MESSAGE"},
	}
	for _, c := range cases {
		m := pane(c.width, 10, w)
		if got := lines(m)[1]; got != c.head {
			t.Errorf("width %d: head %q, want %q", c.width, got, c.head)
		}
		for _, l := range m.BodyLines(now) {
			if n := ansi.StringWidth(l); n > c.width {
				t.Errorf("width %d: line of %d cells: %q", c.width, n, ansi.Strip(l))
			}
		}
	}
}

// The panel says where the record comes from and that actions do not apply,
// then the run's state, times, identity and labels.
func TestInfoPanel(t *testing.T) {
	w := run("etl-1", "u1", "Failed", 26*time.Hour, 5*time.Minute, "child 'check' failed")
	w.Summary.Progress = "1/2"
	m := pane(100, 40, w)
	press(m, "i")
	got := strings.Join(lines(m), "\n")
	for _, want := range []string{
		"Source the workflow archive: a record kept after the run; actions do not apply",
		"Phase ✗ Failed", "Message child 'check' failed",
		"Started 2026-09-07 10:00 UTC (1d2h ago)", "Finished 2026-09-07 10:05 UTC", "Duration 5m",
		"Progress 1/2", "UID u1", "Labels workflows.argoproj.io/cron-workflow=etl",
		"Open enter reads the whole run from the archive",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel lacks %q:\n%s", want, got)
		}
	}
}

// An empty archive says that a server without one answers the same way.
func TestEmptyNote(t *testing.T) {
	got := strings.Join(lines(pane(140, 10)), "\n")
	if !strings.Contains(got, "no archived workflows in this namespace (a server with no workflow archive configured answers the same way)") {
		t.Fatalf("empty state:\n%s", got)
	}
}
