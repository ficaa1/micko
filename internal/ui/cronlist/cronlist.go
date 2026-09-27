// Package cronlist is the cron workflow kind of the list pane: its columns,
// its sort orders, the schedule arithmetic behind NEXT RUN and the info
// panel. The list behaviour itself is kindlist's.
package cronlist

import (
	"cmp"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/cronexpr"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// OwnerLabel is the label the controller puts on every workflow a cron
// workflow starts, with the cron workflow's name as its value. The drill-down
// asks the server for the workflows that carry it.
const OwnerLabel = "workflows.argoproj.io/cron-workflow"

// upcomingCount is how many run times the info panel lists.
const upcomingCount = 5

// Spec is the cron workflow kind.
func Spec() kindlist.Spec[core.CronWorkflow] {
	return kindlist.Spec[core.CronWorkflow]{
		Noun:       "cron workflows",
		Title:      "Cron workflows",
		Namespaced: true,
		Key:        func(c core.CronWorkflow) (string, string) { return c.Namespace, c.Name },
		Columns:    columns,
		Cell:       cell,
		RowStyle:   rowStyle,
		Sorts: []kindlist.SortOrder[core.CronWorkflow]{
			{Label: "next run, suspended last", Compare: byNextRun},
			{Label: "name", Compare: func(a, b core.CronWorkflow, _ time.Time) int {
				return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
			}},
			{Label: "last run, newest first", Compare: byLastRun},
		},
		Info:     info,
		Manifest: func(c core.CronWorkflow) []byte { return c.Resource },
		Drill: func(c core.CronWorkflow) kindlist.DrillMsg {
			return kindlist.DrillMsg{
				Namespace: c.Namespace,
				Selector:  OwnerLabel + "=" + c.Name,
				Title:     "cron " + c.Name,
			}
		},
	}
}

// Plan is what the schedule says about the runs to come.
type Plan struct {
	// Next are the coming run times, soonest first, at most upcomingCount.
	Next []time.Time
	// Err is why no time can be given: a schedule or zone that does not
	// parse, or an @every schedule with no run to count from. Empty when
	// Next is set or the schedule never fires.
	Err string
	// Never marks schedules that parse but fire on no date in the next five
	// years (February 30th).
	Never bool
	// Zone is the zone the schedule runs in.
	Zone *time.Location
}

// PlanOf evaluates cw's schedules at now.
//
// Any schedule that does not parse makes the whole plan unknown. The
// controller schedules a cron workflow's expressions together and reports a
// bad one as a SpecError on the object, so listing the times of the others
// would present a schedule that is not running as if it were.
func PlanOf(cw core.CronWorkflow, now time.Time) Plan {
	loc, err := cronexpr.LoadZone(cw.Timezone)
	if err != nil {
		return Plan{Err: err.Error()}
	}
	p := Plan{Zone: loc}
	if len(cw.Schedules) == 0 {
		p.Err = "the object has no schedule"
		return p
	}
	var cal []*cronexpr.Schedule
	var every []*cronexpr.Schedule
	for _, expr := range cw.Schedules {
		s, err := cronexpr.Parse(expr, loc)
		if err != nil {
			p.Err = expr + ": " + err.Error()
			return p
		}
		if s.Every() > 0 {
			every = append(every, s)
		} else {
			cal = append(cal, s)
		}
	}
	times := cronexpr.Upcoming(cal, now, upcomingCount)
	for _, s := range every {
		// @every counts from the controller's previous run of it. With the
		// last run known and the next one still ahead, the times follow;
		// otherwise the controller's own clock decides and nothing here
		// can say when.
		if cw.LastScheduledTime == nil {
			p.Err = s.Expr() + ": counts from the controller's last run, and none is recorded"
			return p
		}
		next := cw.LastScheduledTime.Add(s.Every())
		if !next.After(now) {
			p.Err = s.Expr() + ": was due " + kindlist.HumanDuration(now.Sub(next)) + " ago and the controller has not recorded a run"
			return p
		}
		for i := 0; i < upcomingCount; i++ {
			times = append(times, next.Add(time.Duration(i)*s.Every()))
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	if len(times) > upcomingCount {
		times = times[:upcomingCount]
	}
	p.Next = times
	p.Never = len(times) == 0
	return p
}

// halted reports whether the controller starts no runs of cw at all: it is
// suspended, or its stop expression has stopped it.
func halted(cw core.CronWorkflow) bool {
	return cw.Suspend || cw.Phase == "Stopped"
}

// columns lays the table out. NAME takes what the fixed columns leave,
// within bounds; narrower panes drop the columns the info panel also shows,
// TZ and POLICY first, then ACTIVE and LAST RUN.
func columns(w int) []kindlist.Column {
	name := func(fixed int) int {
		n := w - fixed
		if n > 40 {
			n = 40
		}
		if n < 12 {
			n = 12
		}
		return n
	}
	switch {
	case w >= 120:
		fixed := 18 + 14 + 10 + 6 + 8 + 9 + 8 + 7*2
		n := name(fixed)
		sched := 18 + (w - fixed - n)
		if sched > 34 {
			sched = 34
		}
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: n},
			{ID: "schedule", Title: "SCHEDULE", Width: sched},
			{ID: "tz", Title: "TZ", Width: 14},
			{ID: "suspended", Title: "SUSPENDED", Width: 10},
			{ID: "active", Title: "ACTIVE", Width: 6, Right: true},
			{ID: "last", Title: "LAST RUN", Width: 8, Right: true},
			{ID: "next", Title: "NEXT RUN", Width: 9, Right: true},
			{ID: "policy", Title: "POLICY", Width: 8},
		}
	case w >= 96:
		fixed := 16 + 10 + 6 + 8 + 9 + 5*2
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: name(fixed + 2)},
			{ID: "schedule", Title: "SCHEDULE", Width: 16},
			{ID: "suspended", Title: "SUSPENDED", Width: 10},
			{ID: "active", Title: "ACTIVE", Width: 6, Right: true},
			{ID: "last", Title: "LAST RUN", Width: 8, Right: true},
			{ID: "next", Title: "NEXT RUN", Width: 9, Right: true},
		}
	case w >= 72:
		fixed := 14 + 10 + 8 + 9 + 4*2
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: name(fixed)},
			{ID: "schedule", Title: "SCHEDULE", Width: 14},
			{ID: "suspended", Title: "SUSPENDED", Width: 10},
			{ID: "last", Title: "LAST RUN", Width: 8, Right: true},
			{ID: "next", Title: "NEXT RUN", Width: 9, Right: true},
		}
	default:
		fixed := 12 + 9 + 9 + 3*2
		return []kindlist.Column{
			{ID: "name", Title: "NAME", Width: name(fixed)},
			{ID: "schedule", Title: "SCHEDULE", Width: 12},
			{ID: "suspended", Title: "SUSPENDED", Width: 9},
			{ID: "next", Title: "NEXT RUN", Width: 9, Right: true},
		}
	}
}

func cell(cw core.CronWorkflow, col string, now time.Time) string {
	switch col {
	case "name":
		return cw.Name
	case "schedule":
		return strings.Join(cw.Schedules, ", ")
	case "tz":
		if isUTC(cw.Timezone) {
			return ""
		}
		return cw.Timezone
	case "suspended":
		return stateCell(cw)
	case "active":
		return itoa(len(cw.Active))
	case "last":
		if cw.LastScheduledTime == nil {
			return "never"
		}
		return kindlist.HumanDuration(now.Sub(*cw.LastScheduledTime))
	case "next":
		return nextCell(cw, now)
	case "policy":
		return policy(cw.ConcurrencyPolicy)
	}
	return ""
}

// stateCell is the SUSPENDED cell: a glyph and a word, so the state reads
// without color. A cron workflow its stop expression has stopped starts no
// runs either, and says so here rather than claiming it is not suspended.
func stateCell(cw core.CronWorkflow) string {
	switch {
	case cw.Suspend:
		return "◐ yes"
	case cw.Phase == "Stopped":
		return "■ stopped"
	default:
		return "● no"
	}
}

// nextCell is the NEXT RUN cell: how long until the next run, "-" when no
// run will start, "never" for a date that does not come, and "?" when the
// schedule cannot be read (the info panel says why).
func nextCell(cw core.CronWorkflow, now time.Time) string {
	if halted(cw) {
		return "-"
	}
	p := PlanOf(cw, now)
	switch {
	case p.Err != "":
		return "?"
	case p.Never:
		return "never"
	}
	return "in " + kindlist.HumanDuration(p.Next[0].Sub(now))
}

func policy(p string) string {
	if p == "" {
		return "Allow"
	}
	return p
}

func isUTC(tz string) bool {
	return tz == "" || tz == "UTC" || tz == "Etc/UTC"
}

// rowStyle dims the rows that start no runs and marks the ones whose
// schedule cannot be read.
func rowStyle(cw core.CronWorkflow, th shared.Theme) lipgloss.Style {
	if halted(cw) {
		return th.Dim
	}
	if scheduleErr(cw) != "" {
		return th.PhaseFailed
	}
	return lipgloss.NewStyle()
}

// scheduleErr is why cw's zone or one of its schedules does not parse, or
// empty when all of them do.
func scheduleErr(cw core.CronWorkflow) string {
	loc, err := cronexpr.LoadZone(cw.Timezone)
	if err != nil {
		return err.Error()
	}
	for _, expr := range cw.Schedules {
		if _, err := cronexpr.Parse(expr, loc); err != nil {
			return expr + ": " + err.Error()
		}
	}
	return ""
}

// rank orders the rows by what they will do: a run coming up, then a
// schedule with no readable next time, then the ones that start nothing.
func rank(cw core.CronWorkflow, now time.Time) (int, time.Time) {
	if halted(cw) {
		return 2, time.Time{}
	}
	p := PlanOf(cw, now)
	if len(p.Next) == 0 {
		return 1, time.Time{}
	}
	return 0, p.Next[0]
}

func byNextRun(a, b core.CronWorkflow, now time.Time) int {
	ra, ta := rank(a, now)
	rb, tb := rank(b, now)
	if ra != rb {
		return cmp.Compare(ra, rb)
	}
	return ta.Compare(tb)
}

func byLastRun(a, b core.CronWorkflow, _ time.Time) int {
	switch {
	case a.LastScheduledTime == nil && b.LastScheduledTime == nil:
		return 0
	case a.LastScheduledTime == nil:
		return 1
	case b.LastScheduledTime == nil:
		return -1
	}
	return b.LastScheduledTime.Compare(*a.LastScheduledTime)
}

// info is the panel for one cron workflow: the schedule and what it will do,
// the policy, what the controller last did, and the workflow each run starts.
func info(cw core.CronWorkflow, reveal bool, now time.Time) []kindlist.Field {
	var f []kindlist.Field
	add := func(label, value string) { f = append(f, kindlist.Field{Label: label, Value: value}) }
	warn := func(label, value string) { f = append(f, kindlist.Field{Label: label, Value: value, Warn: true}) }

	for i, s := range cw.Schedules {
		label := ""
		if i == 0 {
			label = "Schedules"
			if len(cw.Schedules) == 1 {
				label = "Schedule"
			}
		}
		add(label, s)
	}
	if len(cw.Schedules) == 0 {
		warn("Schedule", "none")
	}
	if cw.Timezone == "" {
		add("Timezone", "not set: the controller's local time (UTC unless it sets TZ)")
	} else {
		add("Timezone", cw.Timezone)
	}

	p := PlanOf(cw, now)
	switch {
	case p.Err != "":
		warn("Next runs", "? "+p.Err)
	case p.Never:
		warn("Next runs", "never: the schedule matches no time in the next five years")
	default:
		label := "Next runs"
		switch {
		case cw.Suspend:
			warn(label, "none start while suspended; the schedule would fire at:")
			label = ""
		case cw.Phase == "Stopped":
			warn(label, "none: the stop expression has stopped scheduling; it would fire at:")
			label = ""
		case cw.When != "":
			add(label, "each also needs `when` to hold:")
			label = ""
		}
		for _, t := range p.Next {
			add(label, runTime(t, p.Zone, now))
			label = ""
		}
	}
	if cw.When != "" {
		add("When", cw.When)
	}
	if cw.StopExpression != "" {
		add("Stop when", cw.StopExpression)
	}

	add("Concurrency", policy(cw.ConcurrencyPolicy))
	if cw.StartingDeadlineSeconds != nil {
		add("Deadline", "a missed run may start up to "+itoa(int(*cw.StartingDeadlineSeconds))+"s late")
	} else {
		add("Deadline", "none: a missed run is not started late")
	}
	add("History", "keeps "+limit(cw.SuccessfulJobsHistoryLimit, 3)+" succeeded, "+limit(cw.FailedJobsHistoryLimit, 1)+" failed")

	switch {
	case cw.Suspend:
		warn("Suspended", "yes: no new runs start")
	default:
		add("Suspended", "no")
	}
	if cw.Phase != "" {
		if cw.Phase == "Stopped" {
			warn("Phase", cw.Phase)
		} else {
			add("Phase", cw.Phase)
		}
	}
	if cw.LastScheduledTime != nil {
		add("Last run", cw.LastScheduledTime.In(zoneOr(p.Zone)).Format("Mon 2006-01-02 15:04 MST")+
			" ("+kindlist.HumanDuration(now.Sub(*cw.LastScheduledTime))+" ago)")
	} else {
		add("Last run", "never")
	}
	if cw.Succeeded != nil || cw.Failed != nil {
		add("Finished", count(cw.Succeeded)+" succeeded, "+count(cw.Failed)+" failed")
	}
	if len(cw.Active) == 0 {
		add("Active", "none")
	}
	for i, a := range cw.Active {
		label := ""
		if i == 0 {
			label = "Active"
		}
		add(label, a.Name)
	}
	for i, c := range cw.Conditions {
		label := ""
		if i == 0 {
			label = "Conditions"
		}
		warn(label, c.Type+": "+c.Message)
	}

	if cw.WorkflowTemplateRef != "" {
		add("Template", cw.WorkflowTemplateRef)
	}
	if cw.Entrypoint != "" {
		add("Entrypoint", cw.Entrypoint)
	}
	f = append(f, kindlist.ArgumentFields(cw.Arguments, reveal)...)
	return f
}

// runTime is one upcoming run: the wall time in the schedule's zone and how
// far off it is.
func runTime(t time.Time, loc *time.Location, now time.Time) string {
	return t.In(zoneOr(loc)).Format("Mon 2006-01-02 15:04 MST") + "  (in " + kindlist.HumanDuration(t.Sub(now)) + ")"
}

func zoneOr(loc *time.Location) *time.Location {
	if loc == nil {
		return time.UTC
	}
	return loc
}

// limit renders a history limit. An unset one is the controller's default,
// which Argo documents as 3 succeeded and 1 failed run.
func limit(n *int64, def int) string {
	if n == nil {
		return itoa(def) + " (default)"
	}
	return itoa(int(*n))
}

func count(n *int64) string {
	if n == nil {
		return "?"
	}
	return itoa(int(*n))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
