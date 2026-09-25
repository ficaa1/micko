package cronlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/kindlist"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// now is a Tuesday, 12:00 UTC.
var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func cron(name string, schedules ...string) core.CronWorkflow {
	return core.CronWorkflow{Namespace: "ns", Name: name, Schedules: schedules}
}

func fieldText(fs []kindlist.Field) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Label + " | " + f.Value + "\n")
	}
	return b.String()
}

// The plan lists the next five times of every schedule merged, in the
// object's zone.
func TestPlanMergesSchedulesInZone(t *testing.T) {
	cw := cron("twice", "0 9 * * *", "0 18 * * *")
	cw.Timezone = "Asia/Tokyo"
	p := PlanOf(cw, now)
	if p.Err != "" || len(p.Next) != 5 {
		t.Fatalf("plan = %+v", p)
	}
	// 12:00 UTC is 21:00 in Tokyo: the next run is 09:00 Tokyo, 00:00 UTC.
	if !p.Next[0].Equal(time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first = %s", p.Next[0])
	}
	if !p.Next[1].Equal(time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("second = %s, want 18:00 Tokyo", p.Next[1])
	}
}

// A schedule or zone that does not parse makes the plan unknown, with the
// reason; a date that never comes is "never"; nothing is guessed.
func TestPlanNeverGuesses(t *testing.T) {
	cases := []struct {
		cw   core.CronWorkflow
		err  string
		cell string
	}{
		{cron("bad", "0 * * * 7"), "day of week field", "?"},
		{cron("one-bad", "0 1 * * *", "61 * * * *"), "61 * * * *: minute field", "?"},
		{func() core.CronWorkflow { c := cron("tz", "@daily"); c.Timezone = "Mars/Base"; return c }(), "unknown time zone", "?"},
		{cron("empty"), "no schedule", "?"},
		{cron("every-unknown", "@every 1h"), "none is recorded", "?"},
		{func() core.CronWorkflow {
			c := cron("every-late", "@every 1h")
			c.LastScheduledTime = ptr(now.Add(-3 * time.Hour))
			return c
		}(), "was due 2h ago", "?"},
	}
	for _, c := range cases {
		p := PlanOf(c.cw, now)
		if !strings.Contains(p.Err, c.err) || len(p.Next) != 0 {
			t.Errorf("%s: plan = %+v, want error containing %q", c.cw.Name, p, c.err)
		}
		if got := cell(c.cw, "next", now); got != c.cell {
			t.Errorf("%s: NEXT RUN = %q, want %q", c.cw.Name, got, c.cell)
		}
	}
	never := cron("feb30", "0 0 30 2 *")
	if p := PlanOf(never, now); !p.Never || p.Err != "" {
		t.Fatalf("feb 30 plan = %+v", p)
	}
	if got := cell(never, "next", now); got != "never" {
		t.Fatalf("feb 30 NEXT RUN = %q", got)
	}
}

// @every counts from the last recorded run.
func TestPlanEvery(t *testing.T) {
	c := cron("every", "@every 90m")
	c.LastScheduledTime = ptr(now.Add(-30 * time.Minute))
	p := PlanOf(c, now)
	if p.Err != "" || len(p.Next) != 5 || !p.Next[0].Equal(now.Add(time.Hour)) || !p.Next[1].Equal(now.Add(150*time.Minute)) {
		t.Fatalf("plan = %+v", p)
	}
}

// The cells: TZ only when it is not UTC, the state as glyph and word, the
// active count, the last run's age, the next run, the policy with its
// default.
func TestCells(t *testing.T) {
	c := cron("etl", "0 * * * *")
	c.LastScheduledTime = ptr(now.Add(-65 * time.Minute))
	c.Active = []core.Ref{{Name: "etl-1"}, {Name: "etl-2"}}
	checks := map[string]string{
		"name": "etl", "schedule": "0 * * * *", "tz": "", "suspended": "● no",
		"active": "2", "last": "1h5m", "next": "in 1h", "policy": "Allow",
	}
	for col, want := range checks {
		if got := cell(c, col, now); got != want {
			t.Errorf("%s = %q, want %q", col, got, want)
		}
	}
	c.Timezone = "Etc/UTC"
	if got := cell(c, "tz", now); got != "" {
		t.Errorf("Etc/UTC shown as %q", got)
	}
	c.Timezone = "Europe/Berlin"
	if got := cell(c, "tz", now); got != "Europe/Berlin" {
		t.Errorf("tz = %q", got)
	}
	c.Suspend = true
	if cell(c, "suspended", now) != "◐ yes" || cell(c, "next", now) != "-" {
		t.Errorf("suspended cells = %q %q", cell(c, "suspended", now), cell(c, "next", now))
	}
	c.Suspend, c.Phase = false, "Stopped"
	if cell(c, "suspended", now) != "■ stopped" || cell(c, "next", now) != "-" {
		t.Errorf("stopped cells = %q %q", cell(c, "suspended", now), cell(c, "next", now))
	}
	never := cron("new", "@daily")
	if got := cell(never, "last", now); got != "never" {
		t.Errorf("last run of a new cron = %q", got)
	}
	multi := cron("multi", "0 9 * * *", "0 18 * * *")
	if got := cell(multi, "schedule", now); got != "0 9 * * *, 0 18 * * *" {
		t.Errorf("schedules = %q", got)
	}
}

// The default order: runs coming up soonest first, then schedules with no
// readable next time, then the ones that start nothing.
func TestDefaultOrder(t *testing.T) {
	suspended := cron("a-suspended", "* * * * *")
	suspended.Suspend = true
	items := []core.CronWorkflow{
		suspended,
		cron("b-broken", "0 * * * 7"),
		cron("c-daily", "0 0 * * *"),
		cron("d-soon", "5 12 * * *"),
	}
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetItems(items, now)
	var got []string
	for _, r := range m.Rows() {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != "d-soon,c-daily,b-broken,a-suspended" {
		t.Fatalf("order = %v", got)
	}
}

// Every layout fits its width, and the narrow ones keep NAME, SCHEDULE,
// SUSPENDED and NEXT RUN.
func TestColumnsFit(t *testing.T) {
	c := cron("demo-weekly-compaction-with-a-long-name", "0 3 * * sun")
	c.Timezone = "America/Argentina/Buenos_Aires"
	c.LastScheduledTime = ptr(now.Add(-17 * 24 * time.Hour))
	m := kindlist.New(Spec(), shared.NewTheme(true))
	m.SetItems([]core.CronWorkflow{c}, now)
	for _, w := range []int{60, 76, 96, 116, 136, 160} {
		m.SetSize(w, 12)
		out := m.BodyLines(now)
		for _, l := range out {
			if n := ansi.StringWidth(ansi.Strip(l)); n > w {
				t.Fatalf("width %d: line of %d cells: %q", w, n, l)
			}
		}
		head := ansi.Strip(out[1])
		for _, col := range []string{"NAME", "SCHEDULE", "SUSPENDED", "NEXT RUN"} {
			if !strings.Contains(head, col) {
				t.Errorf("width %d: no %s in %q", w, col, head)
			}
		}
		if w >= 120 && (!strings.Contains(head, "TZ") || !strings.Contains(head, "POLICY")) {
			t.Errorf("width %d: wide layout lacks TZ or POLICY: %q", w, head)
		}
	}
}

// The panel shows the schedule, the zone, five run times, the policy, the
// deadline, the limits, the state, the last run, the active runs and the
// arguments, redacted until reveal.
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
	text := fieldText(info(c, false, now))
	for _, want := range []string{
		"Schedule | 30 6 * * mon-fri", "Timezone | Europe/Berlin",
		"Wed 2026-09-09 06:30 CEST", "Tue 2026-09-15 06:30 CEST",
		"Concurrency | Forbid", "up to 120s late", "keeps 5 succeeded, 1 (default) failed",
		"Suspended | no", "Last run | Tue 2026-09-08 12:00 CEST (2h ago)", "Active | etl-run-1",
		"SubmissionError: quota", "Entrypoint | main",
		"token = [REDACTED]", "region = (from configmap cfg key region)", "date = (no value: supplied at submission)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("panel lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "CEST  (in ") != 5 {
		t.Errorf("want five run times:\n%s", text)
	}
	if strings.Contains(text, "hunter2") {
		t.Fatal("a value shown without reveal")
	}
	if !strings.Contains(fieldText(info(c, true, now)), "token = hunter2") {
		t.Fatal("reveal did not show the value")
	}

	c.Suspend = true
	if text := fieldText(info(c, false, now)); !strings.Contains(text, "none start while suspended") {
		t.Errorf("suspended panel does not say no runs start:\n%s", text)
	}
	bad := cron("bad", "0 * * * 7")
	if text := fieldText(info(bad, false, now)); !strings.Contains(text, "? 0 * * * 7: day of week field: 7 is above the maximum 6") {
		t.Errorf("parse failure not explained:\n%s", text)
	}
	unset := cron("unset", "@daily")
	if text := fieldText(info(unset, false, now)); !strings.Contains(text, "not set: the controller's local time") {
		t.Errorf("unset zone not explained:\n%s", text)
	}
}

// enter narrows the workflow list by the controller's cron label, in the
// cron workflow's own namespace.
func TestDrill(t *testing.T) {
	d := Spec().Drill(core.CronWorkflow{Namespace: "team", Name: "etl"})
	if d.Namespace != "team" || d.Selector != "workflows.argoproj.io/cron-workflow=etl" || d.Title != "cron etl" {
		t.Fatalf("drill = %+v", d)
	}
}
