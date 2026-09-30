package cronexpr

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string, loc *time.Location) *Schedule {
	t.Helper()
	s, err := Parse(expr, loc)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return s
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := LoadZone(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// nextN walks Next n times from start.
func nextN(t *testing.T, s *Schedule, start time.Time, n int) []time.Time {
	t.Helper()
	var out []time.Time
	at := start
	for i := 0; i < n; i++ {
		next, ok := s.Next(at)
		if !ok {
			t.Fatalf("%q: no activation after %s", s.Expr(), at)
		}
		out = append(out, next)
		at = next
	}
	return out
}

func utc(y int, mo time.Month, d, h, m int) time.Time {
	return time.Date(y, mo, d, h, m, 0, 0, time.UTC)
}

// Next is strictly after its argument, lands on the fields' values, and
// covers every field form: values, ranges, steps, lists, N/step, names and
// the descriptors.
func TestNextFieldForms(t *testing.T) {
	from := utc(2026, time.September, 8, 12, 0) // a Tuesday
	cases := []struct {
		expr string
		want []time.Time
	}{
		{"0 * * * *", []time.Time{utc(2026, 9, 8, 13, 0), utc(2026, 9, 8, 14, 0)}},
		{"*/20 * * * *", []time.Time{utc(2026, 9, 8, 12, 20), utc(2026, 9, 8, 12, 40), utc(2026, 9, 8, 13, 0)}},
		{"5/20 * * * *", []time.Time{utc(2026, 9, 8, 12, 5), utc(2026, 9, 8, 12, 25), utc(2026, 9, 8, 12, 45)}},
		{"10-12 3 * * *", []time.Time{utc(2026, 9, 9, 3, 10), utc(2026, 9, 9, 3, 11), utc(2026, 9, 9, 3, 12), utc(2026, 9, 10, 3, 10)}},
		{"0 8,17 * * *", []time.Time{utc(2026, 9, 8, 17, 0), utc(2026, 9, 9, 8, 0)}},
		{"0 0-12/6 * * *", []time.Time{utc(2026, 9, 9, 0, 0), utc(2026, 9, 9, 6, 0), utc(2026, 9, 9, 12, 0), utc(2026, 9, 10, 0, 0)}},
		{"30 6 * * mon-fri", []time.Time{utc(2026, 9, 9, 6, 30), utc(2026, 9, 10, 6, 30), utc(2026, 9, 11, 6, 30), utc(2026, 9, 14, 6, 30)}},
		{"0 0 1 JAN,Jul *", []time.Time{utc(2027, 1, 1, 0, 0), utc(2027, 7, 1, 0, 0)}},
		{"0 12 * * SUN", []time.Time{utc(2026, 9, 13, 12, 0), utc(2026, 9, 20, 12, 0)}},
		{"0 12 ? * 0", []time.Time{utc(2026, 9, 13, 12, 0)}},
		{"@hourly", []time.Time{utc(2026, 9, 8, 13, 0)}},
		{"@daily", []time.Time{utc(2026, 9, 9, 0, 0)}},
		{"@midnight", []time.Time{utc(2026, 9, 9, 0, 0)}},
		{"@weekly", []time.Time{utc(2026, 9, 13, 0, 0)}},
		{"@monthly", []time.Time{utc(2026, 10, 1, 0, 0)}},
		{"@yearly", []time.Time{utc(2027, 1, 1, 0, 0)}},
		{"@annually", []time.Time{utc(2027, 1, 1, 0, 0)}},
		{"@HOURLY", []time.Time{utc(2026, 9, 8, 13, 0)}},
		// Exactly on an activation: the next one is the following one.
		{"0 12 * * *", []time.Time{utc(2026, 9, 9, 12, 0)}},
	}
	for _, c := range cases {
		s := mustParse(t, c.expr, nil)
		got := nextN(t, s, from, len(c.want))
		for i := range c.want {
			if !got[i].Equal(c.want[i]) {
				t.Errorf("%q #%d = %s, want %s", c.expr, i, got[i], c.want[i])
			}
		}
	}
}

// Day of month and day of week combine as the controller's parser combines
// them: either one matching when both are restricted, both when either is a
// bare star. A stepped star is a restriction.
func TestDayOfMonthAndWeekRule(t *testing.T) {
	from := utc(2026, time.September, 8, 12, 0) // Tuesday the 8th
	cases := []struct {
		expr string
		want time.Time
	}{
		// Restricted both: the 15th OR a Friday; Friday the 11th is first.
		{"0 0 15 * fri", utc(2026, 9, 11, 0, 0)},
		// Star in day of week: only the 15th.
		{"0 0 15 * *", utc(2026, 9, 15, 0, 0)},
		// Star in day of month: only Fridays.
		{"0 0 * * fri", utc(2026, 9, 11, 0, 0)},
		// ? is a star.
		{"0 0 15 * ?", utc(2026, 9, 15, 0, 0)},
		// */2 in day of week is a restriction, so this is the 15th OR
		// Sun/Tue/Thu/Sat: Thursday the 10th comes first.
		{"0 0 15 * */2", utc(2026, 9, 10, 0, 0)},
		// */1 keeps the star, so it is the 15th only.
		{"0 0 15 * */1", utc(2026, 9, 15, 0, 0)},
	}
	for _, c := range cases {
		got, ok := mustParse(t, c.expr, nil).Next(from)
		if !ok || !got.Equal(c.want) {
			t.Errorf("%q = %s %v, want %s", c.expr, got, ok, c.want)
		}
	}
}

// Expressions the controller refuses are refused here, with a reason that
// names the field.
func TestParseErrors(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"", "empty"},
		{"* * * *", "expected 5 fields"},
		{"* * * * * *", "expected 5 fields"},
		{"60 * * * *", "minute field: 60 is above the maximum 59"},
		{"* 24 * * *", "hour field: 24 is above the maximum 23"},
		{"* * 0 * *", "day of month field: 0 is below the minimum 1"},
		{"* * 32 * *", "day of month field: 32 is above"},
		{"* * * 13 *", "month field: 13 is above"},
		{"* * * * 7", "Sunday is 0"},
		{"* * * foo *", "month name"},
		{"* * * * funday", "weekday name"},
		{"5-1 * * * *", "runs backwards"},
		{"*/0 * * * *", "positive number"},
		{"*/x * * * *", "positive number"},
		{"1-2-3 * * * *", "more than one -"},
		{"1/2/3 * * * *", "more than one /"},
		{"1,,2 * * * *", "empty list element"},
		{"*-5 * * * *", "cannot range from *"},
		{"@fortnightly", "unknown descriptor"},
		{"@every tomorrow", "@every needs a duration"},
		{"CRON_TZ=Mars/Olympus 0 * * * *", "unknown time zone"},
		{"TZ=UTC", "needs a schedule"},
		{"L * * * *", "not a number"},
	}
	for _, c := range cases {
		_, err := Parse(c.expr, nil)
		if err == nil {
			t.Errorf("Parse(%q) accepted it", c.expr)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) = %q, want it to mention %q", c.expr, err, c.want)
		}
	}
}

// A schedule runs on its zone's wall clock, a zone prefix wins over the zone
// passed in, and the answer is in the caller's zone.
func TestNextInZone(t *testing.T) {
	tokyo := mustZone(t, "Asia/Tokyo")
	cases := []struct {
		name string
		expr string
		loc  *time.Location
		from time.Time
		want time.Time
	}{
		{"09:00 Tokyo is 00:00 UTC", "0 9 * * *", tokyo, utc(2026, 9, 8, 12, 0), utc(2026, 9, 9, 0, 0)},
		{"a local day starting on the previous UTC date", "0 8 * * *", tokyo, utc(2026, 9, 8, 22, 0), utc(2026, 9, 8, 23, 0)},
		{"CRON_TZ over the zone passed in", "CRON_TZ=Asia/Tokyo 0 9 * * *", time.UTC, utc(2026, 9, 8, 12, 0), utc(2026, 9, 9, 0, 0)},
		{"TZ prefix", "TZ=Europe/Berlin @daily", nil, utc(2026, 9, 8, 12, 0), utc(2026, 9, 8, 22, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := mustParse(t, c.expr, c.loc).Next(c.from)
			if !ok || !got.Equal(c.want) {
				t.Fatalf("Next = %s %v, want %s", got.UTC(), ok, c.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("Next answered in %s, want the caller's zone", got.Location())
			}
		})
	}
}

// Spring forward: New York skips 02:00-03:00 on 2026-03-08. A 02:30 job does
// not run that day; the hourly job runs at 01:00 then 03:00 local, one hour
// apart in real time; a 03:30 job keeps its wall time.
func TestSpringForwardGap(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, ny)

	got := nextN(t, mustParse(t, "30 2 * * *", ny), start, 2)
	if w := time.Date(2026, 3, 9, 2, 30, 0, 0, ny); !got[0].Equal(w) {
		t.Fatalf("02:30 across the gap = %s, want the 9th (the 8th has no 02:30)", got[0].In(ny))
	}

	hourly := nextN(t, mustParse(t, "0 * * * *", ny), time.Date(2026, 3, 8, 0, 30, 0, 0, ny), 3)
	want := []string{"01:00 EST", "03:00 EDT", "04:00 EDT"}
	for i, w := range want {
		if g := hourly[i].In(ny).Format("15:04 MST"); g != w {
			t.Fatalf("hourly #%d = %s, want %s", i, g, w)
		}
	}
	if d := hourly[1].Sub(hourly[0]); d != time.Hour {
		t.Fatalf("01:00 EST to 03:00 EDT is %s of real time, want 1h", d)
	}

	got = nextN(t, mustParse(t, "30 3 * * *", ny), start, 2)
	for _, g := range got {
		if g.In(ny).Format("15:04") != "03:30" {
			t.Fatalf("03:30 drifted to %s", g.In(ny).Format("15:04 MST"))
		}
	}
}

// Fall back: New York repeats 01:00-02:00 on 2026-11-01. A 01:30 job runs at
// both 01:30s, an hour apart, in order.
func TestFallBackOverlap(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	got := nextN(t, mustParse(t, "30 1 * * *", ny), time.Date(2026, 10, 31, 12, 0, 0, 0, ny), 3)
	want := []string{"2026-11-01 01:30 EDT", "2026-11-01 01:30 EST", "2026-11-02 01:30 EST"}
	for i, w := range want {
		if g := got[i].In(ny).Format("2006-01-02 15:04 MST"); g != w {
			t.Fatalf("#%d = %s, want %s", i, g, w)
		}
	}
	// Every half hour in the repeated hour, in real-time order.
	q := nextN(t, mustParse(t, "*/30 1 * * *", ny), time.Date(2026, 11, 1, 0, 0, 0, 0, ny), 4)
	wantQ := []string{"01:00 EDT", "01:30 EDT", "01:00 EST", "01:30 EST"}
	for i, w := range wantQ {
		if g := q[i].In(ny).Format("15:04 MST"); g != w {
			t.Fatalf("half hour #%d = %s, want %s", i, g, w)
		}
	}
}

// A date that never comes has no next run, and says so rather than
// inventing one; a leap day waits for its year.
func TestImpossibleAndRareDates(t *testing.T) {
	from := utc(2026, 9, 8, 12, 0)
	if at, ok := mustParse(t, "0 0 30 2 *", nil).Next(from); ok {
		t.Fatalf("February 30th fired at %s", at)
	}
	got, ok := mustParse(t, "0 0 29 2 *", nil).Next(from)
	if !ok || !got.Equal(utc(2028, 2, 29, 0, 0)) {
		t.Fatalf("leap day = %s %v, want 2028-02-29", got, ok)
	}
	got, ok = mustParse(t, "0 0 31 * *", nil).Next(from)
	if !ok || !got.Equal(utc(2026, 10, 31, 0, 0)) {
		t.Fatalf("the 31st after September 8th = %s, want October 31st", got)
	}
}

// @every parses to an interval, rounded down to whole seconds and at least
// one second, and Next does not answer for it.
func TestEvery(t *testing.T) {
	s := mustParse(t, "@every 1h30m", nil)
	if s.Every() != 90*time.Minute {
		t.Fatalf("every = %s", s.Every())
	}
	if _, ok := s.Next(utc(2026, 9, 8, 12, 0)); ok {
		t.Fatal("Next answered for an @every schedule")
	}
	if d := mustParse(t, "@every 1500ms", nil).Every(); d != time.Second {
		t.Fatalf("1500ms = %s, want 1s", d)
	}
	if d := mustParse(t, "@every 10ms", nil).Every(); d != time.Second {
		t.Fatalf("10ms = %s, want 1s", d)
	}
}

// Upcoming merges schedules in time order and lists a shared time once.
func TestUpcomingMerges(t *testing.T) {
	a := mustParse(t, "0 */6 * * *", nil)
	b := mustParse(t, "0 12,15 * * *", nil)
	got := Upcoming([]*Schedule{a, b}, utc(2026, 9, 8, 10, 0), 5)
	want := []time.Time{utc(2026, 9, 8, 12, 0), utc(2026, 9, 8, 15, 0), utc(2026, 9, 8, 18, 0), utc(2026, 9, 9, 0, 0), utc(2026, 9, 9, 6, 0)}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("#%d = %s, want %s", i, got[i], want[i])
		}
	}
	if got := Upcoming([]*Schedule{mustParse(t, "@every 1h", nil)}, utc(2026, 9, 8, 10, 0), 5); len(got) != 0 {
		t.Fatalf("@every contributed %v", got)
	}
}

// Every minute of a busy schedule stays cheap and exact across a day
// boundary.
func TestEveryMinuteAcrossMidnight(t *testing.T) {
	got := nextN(t, mustParse(t, "* * * * *", nil), utc(2026, 12, 31, 23, 58), 3)
	want := []time.Time{utc(2026, 12, 31, 23, 59), utc(2027, 1, 1, 0, 0), utc(2027, 1, 1, 0, 1)}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("#%d = %s, want %s", i, got[i], want[i])
		}
	}
}

// An unknown or empty zone name: empty is UTC, a bad one is an error.
func TestLoadZone(t *testing.T) {
	if loc, err := LoadZone(""); err != nil || loc != time.UTC {
		t.Fatalf("empty zone = %v %v", loc, err)
	}
	if _, err := LoadZone("Not/AZone"); err == nil || !strings.Contains(err.Error(), "unknown time zone") {
		t.Fatalf("bad zone err = %v", err)
	}
}
