package cronlist

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

// now is a Tuesday, 12:00 UTC.
var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func cron(name string, schedules ...string) core.CronWorkflow {
	return core.CronWorkflow{Namespace: "ns", Name: name, Schedules: schedules}
}

// with returns c after edit.
func with(c core.CronWorkflow, edit func(*core.CronWorkflow)) core.CronWorkflow {
	edit(&c)
	return c
}

// pane is the app's cron list at w×h holding items.
func pane(w, h int, items ...core.CronWorkflow) *kindlist.Model[core.CronWorkflow] {
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetSize(w, h)
	m.SetItems(items, now)
	m.SetStatus(kindlist.StatusIdle, "", 0)
	return m
}

// press sends each key in turn, drawing the pane first as the shell does.
func press(m *kindlist.Model[core.CronWorkflow], keys ...string) tea.Cmd {
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
func lines(m *kindlist.Model[core.CronWorkflow]) []string {
	var out []string
	for _, l := range m.BodyLines(now) {
		out = append(out, strings.Join(strings.Fields(ansi.Strip(l)), " "))
	}
	return out
}

// panel is the info panel of a cron list holding only c.
func panel(c core.CronWorkflow, keys ...string) string {
	m := pane(100, 80, c)
	m.SetRedact(true)
	press(m, append([]string{"i"}, keys...)...)
	return strings.Join(lines(m), "\n")
}

// NEXT RUN says how long until the next run of all the schedules merged, in
// the object's zone; the panel lists the times or says why there are none.
// Nothing is guessed: one unreadable schedule makes the whole plan unknown.
func TestNextRun(t *testing.T) {
	cases := []struct {
		name      string
		cw        core.CronWorkflow
		wantCell  string
		wantPanel []string
	}{
		{"two schedules in Tokyo", with(cron("twice", "0 9 * * *", "0 18 * * *"), func(c *core.CronWorkflow) { c.Timezone = "Asia/Tokyo" }),
			"in 12h", []string{"Next runs Wed 2026-09-09 09:00 JST (in 12h)", "Wed 2026-09-09 18:00 JST (in 21h)", "Fri 2026-09-11 09:00 JST (in 2d12h)"}},
		{"@every from the last run", with(cron("every", "@every 90m"), func(c *core.CronWorkflow) { c.LastScheduledTime = ptr(now.Add(-30 * time.Minute)) }),
			"in 1h", []string{"Next runs Tue 2026-09-08 13:00 UTC (in 1h)", "Tue 2026-09-08 14:30 UTC (in 2h30m)"}},
		{"bad day of week", cron("bad", "0 * * * 7"), "?", []string{"Next runs ? 0 * * * 7: day of week field: 7 is above the maximum 6"}},
		{"one bad schedule of two", cron("one-bad", "0 1 * * *", "61 * * * *"), "?", []string{"? 61 * * * *: minute field"}},
		{"unknown zone", with(cron("tz", "@daily"), func(c *core.CronWorkflow) { c.Timezone = "Mars/Base" }), "?", []string{"unknown time zone"}},
		{"no schedule", cron("empty"), "?", []string{"Schedule none", "? the object has no schedule"}},
		{"@every with no run", cron("every-unknown", "@every 1h"), "?", []string{"none is recorded"}},
		{"@every overdue", with(cron("every-late", "@every 1h"), func(c *core.CronWorkflow) { c.LastScheduledTime = ptr(now.Add(-3 * time.Hour)) }),
			"?", []string{"was due 2h ago"}},
		{"a date that never comes", cron("feb30", "0 0 30 2 *"), "never", []string{"never: the schedule matches no time in the next five years"}},
		{"suspended", with(cron("paused", "0 * * * *"), func(c *core.CronWorkflow) { c.Suspend = true }),
			"-", []string{"none start while suspended; the schedule would fire at:", "Suspended yes: no new runs start"}},
		{"stopped", with(cron("done", "0 * * * *"), func(c *core.CronWorkflow) { c.Phase = "Stopped" }),
			"-", []string{"none: the stop expression has stopped scheduling", "Phase Stopped"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := lines(pane(160, 10, c.cw))[2]
			if !strings.HasSuffix(row, " "+c.wantCell+" Allow") {
				t.Errorf("row %q, want NEXT RUN %q", row, c.wantCell)
			}
			got := panel(c.cw)
			for _, want := range c.wantPanel {
				if !strings.Contains(got, want) {
					t.Errorf("panel lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

// A row reads left to right on the widest layout: name, schedules, zone
// unless UTC, state as glyph and word, active count, last run, next run and
// policy with its default.
func TestRows(t *testing.T) {
	etl := cron("etl", "0 * * * *")
	etl.LastScheduledTime = ptr(now.Add(-65 * time.Minute))
	etl.Active = []core.Ref{{Name: "etl-1"}, {Name: "etl-2"}}
	cases := []struct {
		name string
		cw   core.CronWorkflow
		want string
	}{
		{"UTC", etl, "etl 0 * * * * ● no 2 1h5m in 1h Allow"},
		{"Etc/UTC", with(etl, func(c *core.CronWorkflow) { c.Timezone = "Etc/UTC" }), "etl 0 * * * * ● no 2 1h5m in 1h Allow"},
		{"zone", with(etl, func(c *core.CronWorkflow) { c.Timezone = "Europe/Berlin" }), "etl 0 * * * * Europe/Berlin ● no 2 1h5m in 1h Allow"},
		{"suspended", with(etl, func(c *core.CronWorkflow) { c.Suspend = true }), "etl 0 * * * * ◐ yes 2 1h5m - Allow"},
		{"stopped", with(etl, func(c *core.CronWorkflow) { c.Phase = "Stopped" }), "etl 0 * * * * ■ stopped 2 1h5m - Allow"},
		{"policy", with(etl, func(c *core.CronWorkflow) { c.ConcurrencyPolicy = "Forbid" }), "etl 0 * * * * ● no 2 1h5m in 1h Forbid"},
		{"never run", cron("new", "@daily"), "new @daily ● no 0 never in 12h Allow"},
		{"two schedules", cron("multi", "0 9 * * *", "0 18 * * *"), "multi 0 9 * * *, 0 18 * * * ● no 0 never in 6h Allow"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lines(pane(160, 10, c.cw))[2]; got != c.want {
				t.Errorf("row = %q, want %q", got, c.want)
			}
		})
	}
}

// The default order puts runs coming up soonest first, then schedules with
// no readable next time, then the ones that start nothing; s cycles to name
// and to the newest last run.
func TestSort(t *testing.T) {
	items := []core.CronWorkflow{
		with(cron("a-suspended", "* * * * *"), func(c *core.CronWorkflow) {
			c.Suspend, c.LastScheduledTime = true, ptr(now.Add(-time.Minute))
		}),
		cron("b-broken", "0 * * * 7"),
		with(cron("c-daily", "0 0 * * *"), func(c *core.CronWorkflow) { c.LastScheduledTime = ptr(now.Add(-12 * time.Hour)) }),
		cron("d-soon", "5 12 * * *"),
	}
	cases := []struct {
		keys []string
		want string
	}{
		{nil, "d-soon,c-daily,b-broken,a-suspended"},
		{[]string{"s"}, "a-suspended,b-broken,c-daily,d-soon"},
		{[]string{"s", "s"}, "a-suspended,c-daily,b-broken,d-soon"},
	}
	for _, c := range cases {
		m := pane(100, 10, items...)
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

// Every layout fits its width and keeps NAME, SCHEDULE, SUSPENDED and NEXT
// RUN; the wide ones add TZ and POLICY.
func TestLayout(t *testing.T) {
	c := cron("demo-weekly-compaction-with-a-long-name", "0 3 * * sun")
	c.Timezone = "America/Argentina/Buenos_Aires"
	c.LastScheduledTime = ptr(now.Add(-17 * 24 * time.Hour))
	cases := []struct {
		width int
		wide  bool
	}{
		{60, false}, {76, false}, {96, false}, {116, false}, {136, true}, {160, true},
	}
	for _, tc := range cases {
		m := pane(tc.width, 12, c)
		for _, l := range m.BodyLines(now) {
			if n := ansi.StringWidth(l); n > tc.width {
				t.Errorf("width %d: line of %d cells: %q", tc.width, n, ansi.Strip(l))
			}
		}
		head := lines(m)[1]
		for _, col := range []string{"NAME", "SCHEDULE", "SUSPENDED", "NEXT RUN"} {
			if !strings.Contains(head, col) {
				t.Errorf("width %d: no %s in %q", tc.width, col, head)
			}
		}
		if got := strings.Contains(head, "TZ") && strings.Contains(head, "POLICY"); got != tc.wide {
			t.Errorf("width %d: head %q, want TZ and POLICY: %v", tc.width, head, tc.wide)
		}
	}
}

// The panel shows the schedule, the zone, five run times, the policy, the
// deadline, the limits, the state, the last run, the active runs and the
// arguments, redacted until v.
func TestInfoPanel(t *testing.T) {
	c := cron("etl", "30 6 * * mon-fri")
	c.Timezone = "Europe/Berlin"
	c.ConcurrencyPolicy = "Forbid"
	c.StartingDeadlineSeconds = ptr(int64(120))
	c.SuccessfulJobsHistoryLimit = ptr(int64(5))
	c.LastScheduledTime = ptr(now.Add(-2 * time.Hour))
	c.Active = []core.Ref{{Name: "etl-run-1"}}
	c.Entrypoint = "main"
	c.Arguments = []core.Argument{
		{Name: "token", Value: "hunter2", HasValue: true},
		{Name: "region", ValueFrom: "configmap cfg key region"},
		{Name: "date"},
	}
	c.Conditions = []core.Condition{{Type: "SubmissionError", Message: "quota"}}
	text := panel(c)
	for _, want := range []string{
		"Schedule 30 6 * * mon-fri", "Timezone Europe/Berlin",
		"Next runs Wed 2026-09-09 06:30 CEST (in 16h30m)", "Tue 2026-09-15 06:30 CEST (in 6d16h)",
		"Concurrency Forbid", "Deadline a missed run may start up to 120s late", "History keeps 5 succeeded, 1 (default) failed",
		"Suspended no", "Last run Tue 2026-09-08 12:00 CEST (2h ago)", "Active etl-run-1",
		"Conditions SubmissionError: quota", "Entrypoint main",
		"Arguments token = [REDACTED]", "region = (from configmap cfg key region)", "date = (no value: supplied at submission)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("panel lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "CEST (in "); n != 5 {
		t.Errorf("%d run times, want five:\n%s", n, text)
	}
	if strings.Contains(text, "hunter2") {
		t.Error("a value shown before v")
	}
	if !strings.Contains(panel(c, "v"), "token = hunter2") {
		t.Error("v did not show the value")
	}
	if text := panel(cron("unset", "@daily")); !strings.Contains(text, "Timezone not set: the controller's local time") {
		t.Errorf("unset zone not explained:\n%s", text)
	}
}

// enter narrows the workflow list by the controller's cron label, in the
// cron workflow's own namespace.
func TestDrill(t *testing.T) {
	want := kindlist.DrillMsg{Namespace: "team", Selector: "workflows.argoproj.io/cron-workflow=etl", Title: "cron etl"}
	if got := press(pane(100, 10, core.CronWorkflow{Namespace: "team", Name: "etl"}), "enter")(); got != want {
		t.Errorf("enter = %#v, want %#v", got, want)
	}
}
