package workflowlist

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
)

// demoSummaries is the demo namespace's list, as the demo serves it, with the
// clock it was built against.
func demoSummaries(t *testing.T) ([]core.Summary, time.Time) {
	t.Helper()
	now := testkit.FixtureEpoch
	f := testkit.DemoReader(testkit.NewFakeClock(now))
	var out []core.Summary
	for _, ref := range f.Order {
		// The demo namespace's workflows: the list a session opens on.
		if ref.Namespace == testkit.DemoNamespace {
			out = append(out, f.Workflows[ref].Summary)
		}
	}
	return out, now
}

// matching returns the names q selects from items, in input order.
func matching(t *testing.T, q string, items []core.Summary, now time.Time) []string {
	t.Helper()
	m, err := ParseQuery(q)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", q, err)
	}
	var out []string
	for _, s := range items {
		if m.Match(s, now) {
			out = append(out, s.Ref.Name)
		}
	}
	return out
}

// Each operator parses to its canonical form. The canonical form is what
// the toolbar shows, so it must also parse back to the same filter.
func TestParseQueryCanonicalForms(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"etl", "etl"},
		{"ETL", "ETL"},
		{"etl  report", "etl & report"},
		{"a|b", "a|b"},
		{"!a", "!a"},
		{"!a|b", "!a|b"},
		{"/^demo-.*-\\d+$/", "/^demo-.*-\\d+$/"},
		{"/a b|c/", "/a b|c/"},
		{"/a\\/b/", "/a\\/b/"},
		{"~dpl", "~dpl"},
		{"~DPL", "~dpl"},
		{"phase=failed", "phase=Failed"},
		{"PHASE=FAILED", "phase=Failed"},
		{"phase!=succeeded", "phase!=Succeeded"},
		{"phase=suspended", "phase=Suspended"},
		{"phase=WeirdFuture", "phase=weirdfuture"},
		{"age<2h", "age<2h"},
		{"age>1d", "age>1d"},
		{"age<120m", "age<2h"},
		{"dur>90s", "dur>1m30s"},
		{"dur>1d12h", "dur>1d12h"},
		{"label:team=data", "label:team=data"},
		{"label:team", "label:team"},
		{"label:!team", "label:!team"},
		{"label:app.kubernetes.io/name=x", "label:app.kubernetes.io/name=x"},
		{"tmpl=nightly-report", "tmpl=nightly-report"},
		{"tmpl!=deploy", "tmpl!=deploy"},
		{"cron=demo-etl-hourly", "cron=demo-etl-hourly"},
		{"!phase=failed|~etl age<1d", "!phase=Failed|~etl & age<1d"},
		{"a & b", "a & b"},
		{"& a &", "a"},
	}
	for _, c := range cases {
		m, err := ParseQuery(c.in)
		if err != nil {
			t.Errorf("ParseQuery(%q): %v", c.in, err)
			continue
		}
		if got := m.String(); got != c.want {
			t.Errorf("ParseQuery(%q).String() = %q, want %q", c.in, got, c.want)
			continue
		}
		again, err := ParseQuery(m.String())
		if err != nil || again.String() != c.want {
			t.Errorf("canonical %q does not parse back: %q, %v", c.want, again.String(), err)
		}
	}
}

// Every malformed term is an error that names what is wrong, never a
// filter that silently matches nothing.
func TestParseQueryErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"phase=", "phase= needs a value"},
		{"phase!=", "phase!= needs a value"},
		{"phase<2", "phase takes != or ="},
		{"age=2h", "age takes < or >"},
		{"age<", "age< needs a value"},
		{"age<2", `bad duration "2"`},
		{"age<2x", `bad duration "2x"`},
		{"age<h", `bad duration "h"`},
		{"dur>1.5h", `bad duration "1.5h"`},
		{"age<99999999999d", "too long"},
		{"owner=me", `unknown field "owner"`},
		{"x:y", `unknown field "x"`},
		{"=x", `"=x" is not a term`},
		{"label:", "label: needs a value"},
		{"label:=x", "label: needs a key"},
		{"label:k=", "label:k= needs a value"},
		{"label:!", "label:! takes a key alone"},
		{"label:!k=v", "label:! takes a key alone"},
		{"label:k=v=w", "is not key, key=value or !key"},
		{"tmpl=", "tmpl= needs a value"},
		{"phase=failed<2h", `unexpected "<"`},
		{"/a(/", "bad regex /a(/: missing closing )"},
		{"/abc", "unterminated regex /abc"},
		{"/a/b", "text after the closing / of /a/"},
		{"//", "empty regex //"},
		{"~", "~ needs letters after it"},
		{"!", "! needs a term after it"},
		{"! a", "! needs a term after it"},
		{"!!a", "!! is not a term"},
		{"a|", "empty alternative after |"},
		{"a||b", "empty alternative after |"},
		{"|a", "empty alternative before |"},
		{"a |b", "empty alternative before |"},
		{"good phase=", "phase= needs a value"},
	}
	for _, c := range cases {
		_, err := ParseQuery(c.in)
		if err == nil {
			t.Errorf("ParseQuery(%q) parsed; want an error containing %q", c.in, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseQuery(%q) error = %q, want it to contain %q", c.in, err, c.want)
		}
	}
}

// Spaces AND terms, | ORs alternatives within one, and ! binds to the one
// alternative it precedes.
func TestParseQueryPrecedence(t *testing.T) {
	names := func(ns ...string) []core.Summary {
		var out []core.Summary
		for _, n := range ns {
			out = append(out, core.Summary{Ref: core.Ref{Name: n, UID: n}})
		}
		return out
	}
	items := names("ab", "a", "b", "c")
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"a b", []string{"ab"}},
		{"a|b", []string{"ab", "a", "b"}},
		{"a|b c", nil},
		{"!a", []string{"b", "c"}},
		{"!a|b", []string{"ab", "b", "c"}}, // (not a) or b
		{"!a !b", []string{"c"}},           // neither
		{"a|b !b", []string{"a"}},
		{"/^a$|^c$/", []string{"a", "c"}}, // | inside a regex is the regex's
		{"/^A/", []string{"ab", "a"}},     // case-insensitive
		{"~ab", []string{"ab"}},
		{"~ba", nil}, // order matters
	} {
		if got := matching(t, c.q, items, time.Time{}); !slices.Equal(got, c.want) {
			t.Errorf("%q matched %v, want %v", c.q, got, c.want)
		}
	}
}

// The matcher answers the questions an operator asks of the demo's twelve
// workflows. The demo builds its ages from the clock it is given, so these
// are exact.
func TestMatcherAgainstTheDemo(t *testing.T) {
	items, now := demoSummaries(t)
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"etl", []string{"demo-etl-hourly-1790000000", "demo-etl-hourly-1789996400", "demo-etl-hourly-1789992800"}},
		{"ETL-HOURLY-179000", []string{"demo-etl-hourly-1790000000"}},
		{"phase=failed", []string{"demo-nightly-report", "demo-oom-backfill", "demo-etl-hourly-1790000000"}},
		{"phase=failed|phase=error", []string{"demo-nightly-report", "demo-oom-backfill", "demo-param-check", "demo-etl-hourly-1790000000"}},
		{"phase=suspended", []string{"demo-release-gate"}},
		// The gate's workflow is Running to the server, so it is running.
		{"phase=running", []string{"demo-train-pipeline", "demo-data-pull", "demo-release-gate"}},
		{"phase=running !phase=suspended", []string{"demo-train-pipeline", "demo-data-pull"}},
		{"phase!=succeeded phase!=failed", []string{"demo-train-pipeline", "demo-data-pull", "demo-cleanup", "demo-release-gate", "demo-param-check"}},
		{"age<15m", []string{"demo-train-pipeline", "demo-cleanup", "demo-release-gate"}},
		{"age>5h", []string{"demo-param-check"}},
		{"age>1d", nil},
		// A running workflow's run time counts until now; the pending one
		// has not started and has no run time at all.
		{"dur>30m", []string{"demo-data-pull"}},
		{"dur<5m", []string{"demo-hello-world", "demo-nightly-report", "demo-train-pipeline", "demo-param-check"}},
		{"tmpl=nightly-report", []string{"demo-nightly-report"}},
		{"tmpl=NIGHTLY-REPORT", []string{"demo-nightly-report"}},
		{"cron=demo-etl-hourly phase=failed", []string{"demo-etl-hourly-1790000000"}},
		{"label:team=platform", []string{"demo-release-gate", "demo-deploy-multi-layer"}},
		{"label:team=Platform", nil}, // label values are exact
		{"label:env", []string{"demo-release-gate", "demo-deploy-multi-layer"}},
		{"label:!team", []string{"demo-hello-world", "demo-data-pull", "demo-cleanup", "demo-oom-backfill", "demo-param-check"}},
		{"~ddp", []string{"demo-data-pull", "demo-deploy-multi-layer"}},
		{"/-(pull|gate)$/", []string{"demo-data-pull", "demo-release-gate"}},
		{"!etl !demo-d", []string{"demo-hello-world", "demo-nightly-report", "demo-train-pipeline", "demo-cleanup", "demo-release-gate", "demo-oom-backfill", "demo-param-check"}},
	} {
		if got := matching(t, c.q, items, now); !slices.Equal(got, c.want) {
			t.Errorf("%q matched\n  %v\nwant\n  %v", c.q, got, c.want)
		}
	}
}

// A workflow with nothing to measure matches neither side of a bound, so
// "age<2h" and "age>2h" together never claim the same row.
func TestTimePredicatesSkipMissingTimestamps(t *testing.T) {
	now := testkit.FixtureEpoch
	noTimes := core.Summary{Ref: core.Ref{Name: "x", UID: "x"}}
	for _, q := range []string{"age<2h", "age>2h", "dur<2h", "dur>2h"} {
		if got := matching(t, q, []core.Summary{noTimes}, now); got != nil {
			t.Errorf("%q matched a workflow with no timestamps", q)
		}
	}
	started := now.Add(-time.Hour)
	running := core.Summary{Ref: core.Ref{Name: "r", UID: "r"}, StartedAt: &started}
	if got := matching(t, "dur>30m", []core.Summary{running}, time.Time{}); got != nil {
		t.Error("a running workflow's run time was measured without a clock")
	}
}
