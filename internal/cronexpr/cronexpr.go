// Package cronexpr evaluates the schedule expressions of Argo CronWorkflows:
// when does this schedule fire next?
//
// It reads the dialect the Argo controller runs, which is the standard
// five-field parser of github.com/robfig/cron/v3 (cron.ParseStandard) with
// the controller's own "CRON_TZ=<zone> " prefix for spec.timezone:
//
//   - five fields: minute, hour, day of month, month, day of week
//   - each field is a comma list of `*`, `?`, a value, a range `a-b`, and an
//     optional step `/n`; `a/n` means `a-max/n`
//   - months and weekdays also take their three-letter English names, in any
//     case; day of week runs 0-6 with Sunday as 0
//   - the descriptors @yearly (@annually), @monthly, @weekly, @daily
//     (@midnight), @hourly and @every <duration>
//   - a leading `TZ=<zone>` or `CRON_TZ=<zone>` sets the time zone
//
// Day of month and day of week combine the way that parser combines them:
// when either field is a bare `*` or `?` the day must match both, otherwise it
// may match either. A stepped star such as `*/2` counts as a restriction, not
// as a star, because the parser clears its star marker for any step above 1.
//
// Times are computed on the wall clock of the schedule's zone, so a daily
// 02:30 stays at 02:30 across a daylight-saving change. A wall time that does
// not exist on a spring-forward day is skipped, and one that happens twice on
// a fall-back day fires twice, which is what the controller does.
//
// The package is pure: no I/O, no clock of its own. Parsing never guesses;
// an expression the controller would refuse is an error here too, and the
// error says which field and why.
package cronexpr

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	// The zone database is embedded so a schedule's timezone resolves the
	// same way on every machine, including minimal containers and Windows
	// hosts that ship no zoneinfo files. Without it a valid zone such as
	// Europe/Berlin would read as unknown there.
	_ "time/tzdata"
)

// searchYears bounds how far ahead Next looks. It is the controller parser's
// own horizon: a schedule with no activation in five years (February 30th)
// never fires there either.
const searchYears = 5

// Schedule is one parsed expression.
type Schedule struct {
	// expr is the expression as written, for messages.
	expr string
	loc  *time.Location
	// every is the interval of an @every schedule; zero for a calendar one.
	every time.Duration
	// The calendar fields as bit sets: bit n set means value n matches.
	minute, hour, dom, month, dow uint64
	// domStar and dowStar record a bare `*` or `?` in the day fields, which
	// switches the day rule from "either" to "both".
	domStar, dowStar bool
}

// field describes one of the five positions.
type field struct {
	name     string
	min, max int
	names    map[string]int
}

var (
	minuteField = field{name: "minute", min: 0, max: 59}
	hourField   = field{name: "hour", min: 0, max: 23}
	domField    = field{name: "day of month", min: 1, max: 31}
	monthField  = field{name: "month", min: 1, max: 12, names: map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}}
	dowField = field{name: "day of week", min: 0, max: 6, names: map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}}
)

// descriptors maps each calendar descriptor to its five-field form.
var descriptors = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// Parse reads one expression. loc is the zone the schedule runs in, from
// spec.timezone; nil means UTC. A TZ= or CRON_TZ= prefix in the expression
// takes precedence over loc.
func Parse(expr string, loc *time.Location) (*Schedule, error) {
	if loc == nil {
		loc = time.UTC
	}
	spec := strings.TrimSpace(expr)
	if spec == "" {
		return nil, errors.New("the schedule is empty")
	}
	if strings.HasPrefix(spec, "TZ=") || strings.HasPrefix(spec, "CRON_TZ=") {
		eq := strings.Index(spec, "=")
		sp := strings.IndexAny(spec, " \t")
		if sp < 0 {
			return nil, errors.New("a time zone prefix needs a schedule after it")
		}
		zone := spec[eq+1 : sp]
		l, err := LoadZone(zone)
		if err != nil {
			return nil, err
		}
		loc = l
		spec = strings.TrimSpace(spec[sp:])
	}
	s := &Schedule{expr: strings.TrimSpace(expr), loc: loc}
	if strings.HasPrefix(spec, "@") {
		if rest, ok := strings.CutPrefix(spec, "@every "); ok {
			d, err := time.ParseDuration(strings.TrimSpace(rest))
			if err != nil {
				return nil, fmt.Errorf("@every needs a duration such as 90m or 1h30m, not %q", strings.TrimSpace(rest))
			}
			if d < time.Second {
				// The controller's parser raises anything shorter to one
				// second and drops sub-second parts.
				d = time.Second
			}
			s.every = d - d%time.Second
			return s, nil
		}
		five, ok := descriptors[strings.ToLower(spec)]
		if !ok {
			return nil, fmt.Errorf("unknown descriptor %q (known: @yearly @monthly @weekly @daily @hourly @every)", spec)
		}
		spec = five
	}
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return nil, fmt.Errorf("expected 5 fields (minute hour day-of-month month day-of-week), found %d", len(parts))
	}
	var err error
	if s.minute, _, err = parseField(parts[0], minuteField); err != nil {
		return nil, err
	}
	if s.hour, _, err = parseField(parts[1], hourField); err != nil {
		return nil, err
	}
	if s.dom, s.domStar, err = parseField(parts[2], domField); err != nil {
		return nil, err
	}
	if s.month, _, err = parseField(parts[3], monthField); err != nil {
		return nil, err
	}
	if s.dow, s.dowStar, err = parseField(parts[4], dowField); err != nil {
		return nil, err
	}
	return s, nil
}

// LoadZone resolves a spec.timezone value. Empty is UTC: the controller then
// uses its own local time, which is UTC in the images Argo ships.
func LoadZone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", name)
	}
	return loc, nil
}

// parseField reads one comma list into a bit set. star reports a bare `*` or
// `?` anywhere in the list.
func parseField(text string, f field) (bits uint64, star bool, err error) {
	for _, part := range strings.Split(text, ",") {
		b, st, err := parseRange(part, f)
		if err != nil {
			return 0, false, err
		}
		bits |= b
		star = star || st
	}
	return bits, star, nil
}

// parseRange reads one list element: `*`, `?`, `v`, `a-b`, each with an
// optional `/step`.
func parseRange(part string, f field) (uint64, bool, error) {
	if part == "" {
		return 0, false, fmt.Errorf("%s field has an empty list element", f.name)
	}
	rangeAndStep := strings.Split(part, "/")
	if len(rangeAndStep) > 2 {
		return 0, false, fmt.Errorf("%s field: %q has more than one /", f.name, part)
	}
	lowHigh := strings.Split(rangeAndStep[0], "-")
	if len(lowHigh) > 2 {
		return 0, false, fmt.Errorf("%s field: %q has more than one -", f.name, part)
	}
	var start, end int
	star := false
	if lowHigh[0] == "*" || lowHigh[0] == "?" {
		if len(lowHigh) > 1 {
			return 0, false, fmt.Errorf("%s field: %q cannot range from *", f.name, part)
		}
		start, end, star = f.min, f.max, true
	} else {
		var err error
		if start, err = parseValue(lowHigh[0], f); err != nil {
			return 0, false, err
		}
		end = start
		if len(lowHigh) == 2 {
			if end, err = parseValue(lowHigh[1], f); err != nil {
				return 0, false, err
			}
		}
	}
	step := 1
	if len(rangeAndStep) == 2 {
		n, err := strconv.Atoi(rangeAndStep[1])
		if err != nil || n <= 0 {
			return 0, false, fmt.Errorf("%s field: step %q must be a positive number", f.name, rangeAndStep[1])
		}
		step = n
		// "a/n" is "a-max/n".
		if len(lowHigh) == 1 && !star {
			end = f.max
		}
		if step > 1 {
			star = false
		}
	}
	switch {
	case start < f.min:
		return 0, false, fmt.Errorf("%s field: %d is below the minimum %d", f.name, start, f.min)
	case end > f.max:
		return 0, false, fmt.Errorf("%s field: %d is above the maximum %d%s", f.name, end, f.max, maxHint(f))
	case start > end:
		return 0, false, fmt.Errorf("%s field: range %d-%d runs backwards", f.name, start, end)
	}
	var bits uint64
	for v := start; v <= end; v += step {
		bits |= 1 << uint(v)
	}
	return bits, star, nil
}

// maxHint explains the one limit that differs from other cron dialects.
func maxHint(f field) string {
	if f.name == dowField.name {
		return " (Sunday is 0; 7 is not accepted)"
	}
	return ""
}

// parseValue reads a number or, where the field has them, a name.
func parseValue(s string, f field) (int, error) {
	if f.names != nil {
		if v, ok := f.names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s field: %q is not a number%s", f.name, s, nameHint(f))
	}
	return n, nil
}

func nameHint(f field) string {
	switch f.name {
	case monthField.name:
		return " or a month name (jan-dec)"
	case dowField.name:
		return " or a weekday name (sun-sat)"
	}
	return ""
}

// Expr is the expression as written.
func (s *Schedule) Expr() string { return s.expr }

// Location is the zone the schedule runs in.
func (s *Schedule) Location() *time.Location { return s.loc }

// Every is the interval of an @every schedule, zero for a calendar one. An
// @every schedule counts from the controller's previous run of it, so its
// next time is known only relative to that run: Next does not answer for it.
func (s *Schedule) Every() time.Duration { return s.every }

// Next returns the first activation strictly after t, and false when there is
// none within five years or the schedule is an @every one.
func (s *Schedule) Next(t time.Time) (time.Time, bool) {
	if s.every > 0 {
		return time.Time{}, false
	}
	local := t.In(s.loc)
	y, mo, d := local.Date()
	// The day iterator is a civil date, kept at noon UTC so stepping it by
	// one day can never be pulled across a date line by a zone change.
	day := time.Date(y, mo, d, 12, 0, 0, 0, time.UTC)
	// A local day can start on the previous UTC date, so the first
	// candidate day is the one before t's.
	day = day.AddDate(0, 0, -1)
	for i := 0; i < searchYears*366+2; i++ {
		cy, cm, cd := day.Date()
		if s.month&(1<<uint(cm)) != 0 && s.dayMatches(cd, day.Weekday()) {
			if at, ok := s.firstOnDay(cy, cm, cd, t); ok {
				return at, true
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// dayMatches applies the day-of-month / day-of-week rule described in the
// package comment.
func (s *Schedule) dayMatches(dom int, dow time.Weekday) bool {
	domOK := s.dom&(1<<uint(dom)) != 0
	dowOK := s.dow&(1<<uint(dow)) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// firstOnDay returns the earliest activation on the given local date that is
// after t.
//
// Every matching wall time of the day is tried under each UTC offset the zone
// uses around that date, and kept only when it reads back as that same wall
// time. A time in a spring-forward gap reads back as another hour under both
// offsets and drops out; a time in a fall-back overlap reads back correctly
// under both and is kept twice, as two instants an hour apart.
func (s *Schedule) firstOnDay(y int, mo time.Month, d int, t time.Time) (time.Time, bool) {
	offsets := s.offsetsAround(y, mo, d)
	var best time.Time
	found := false
	for h := 0; h < 24; h++ {
		if s.hour&(1<<uint(h)) == 0 {
			continue
		}
		for m := 0; m < 60; m++ {
			if s.minute&(1<<uint(m)) == 0 {
				continue
			}
			wall := time.Date(y, mo, d, h, m, 0, 0, time.UTC)
			for _, off := range offsets {
				at := wall.Add(-time.Duration(off) * time.Second)
				l := at.In(s.loc)
				ly, lmo, ld := l.Date()
				if ly != y || lmo != mo || ld != d || l.Hour() != h || l.Minute() != m {
					continue
				}
				if !at.After(t) {
					continue
				}
				if !found || at.Before(best) {
					best, found = at, true
				}
			}
		}
		// Within one hour of wall time, the first match is the earliest
		// instant unless the day has an overlap; the full scan of the
		// remaining hours is needed only then.
		if found && len(offsets) == 1 {
			break
		}
	}
	return best.In(t.Location()), found
}

// offsetsAround lists the distinct UTC offsets, in seconds, the zone uses
// during the given local date. A local date spans at most the UTC window from
// 14 hours before it to 12 hours after it, and that window is probed every
// few hours so a change of offset anywhere in the day is seen.
func (s *Schedule) offsetsAround(y int, mo time.Month, d int) []int {
	start := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Add(-14 * time.Hour)
	seen := map[int]bool{}
	var out []int
	for probe := start; probe.Before(start.Add(52 * time.Hour)); probe = probe.Add(4 * time.Hour) {
		_, off := probe.In(s.loc).Zone()
		if !seen[off] {
			seen[off] = true
			out = append(out, off)
		}
	}
	sort.Ints(out)
	return out
}

// Upcoming merges the next n activations of several schedules after t, in
// order, with an activation two schedules share listed once. Schedules that
// cannot answer (@every, or no time in five years) contribute nothing.
func Upcoming(schedules []*Schedule, t time.Time, n int) []time.Time {
	var out []time.Time
	seen := map[int64]bool{}
	for _, s := range schedules {
		at := t
		for i := 0; i < n; i++ {
			next, ok := s.Next(at)
			if !ok {
				break
			}
			if !seen[next.Unix()] {
				seen[next.Unix()] = true
				out = append(out, next)
			}
			at = next
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
