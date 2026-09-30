package workflowlist

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

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

func matching(t *testing.T, q string, items []core.Summary, now time.Time) []string {
	t.Helper()
	m := newList(t)
	m.SetItems(items, now)
	editQuery(&m, q)
	m.Update(enterKey())
	if m.SearchOn {
		t.Fatalf("query %q: %s", q, m.queryErr)
	}
	got := rowNames(m.Rows())
	slices.Sort(got)
	return got
}

// Committed queries show a canonical form that can be re-entered without changing the matches.
func TestQueryCanonicalForms(t *testing.T) {
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
		t.Run(c.in, func(t *testing.T) {
			m := newList(t)
			m.SetItems([]core.Summary{summary("etl-report", "Failed"), summary("other", "Running")}, testkit.FixtureEpoch)
			editQuery(&m, c.in)
			m.Update(enterKey())
			if m.SearchOn {
				t.Fatalf("query %q rejected: %s; want a valid committed query", c.in, m.queryErr)
			}
			got := body(&m)
			want := "Search: " + c.want
			if c.want == "" {
				want = "Search: (none)"
			}
			if !strings.Contains(got, want+" ") {
				t.Fatalf("canonical %q missing:\n%s", want, got)
			}
			before := rowIDs(m.Rows())
			editQuery(&m, c.want)
			m.Update(enterKey())
			if m.SearchOn || !slices.Equal(rowIDs(m.Rows()), before) || !strings.Contains(body(&m), want+" ") {
				t.Fatalf("canonical reentry focused=%t rows=%v body=%q, want committed rows %v and %q", m.SearchOn, rowIDs(m.Rows()), body(&m), before, want)
			}
		})
	}
}

// Malformed queries keep the last valid rows and input focus until repaired or cancelled.
func TestQueryErrors(t *testing.T) {
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
		t.Run(c.in, func(t *testing.T) {
			m := newList(t)
			m.SetItems([]core.Summary{summary("good", "Running")}, testkit.FixtureEpoch)
			editQuery(&m, c.in)
			before := rowIDs(m.Rows())
			query := m.Query()
			m.Update(enterKey())
			if !m.SearchOn || !strings.Contains(body(&m), c.want) || !slices.Equal(rowIDs(m.Rows()), before) || m.Query() != query {
				t.Fatalf("bad query committed or lost diagnostic: %q\n%s", c.in, body(&m))
			}
		})
	}
	t.Run("preserve repair cancel", func(t *testing.T) {
		m := newList(t)
		m.SetItems([]core.Summary{summary("etl-hourly", "Failed"), summary("other", "Running")}, testkit.FixtureEpoch)
		editQuery(&m, "etl")
		m.Update(enterKey())
		m.Update(runeKey('/'))
		typeText(&m, " phase=failed")
		typeText(&m, " /hourly(/")
		if m.Query() != "etl phase=failed" || !slices.Equal(rowIDs(m.Rows()), []string{"etl-hourly"}) {
			t.Fatalf("invalid term query=%q rows=%v, want etl phase=failed and [etl-hourly]", m.Query(), rowIDs(m.Rows()))
		}
		m.Update(enterKey())
		if !m.SearchOn {
			t.Fatalf("invalid Enter focused=%t query=%q error=%q, want focused input retaining its parse error", m.SearchOn, m.Query(), m.queryErr)
		}
		m.Update(backspaceKey())
		m.Update(backspaceKey())
		typeText(&m, "/")
		if m.Query() != "etl phase=failed /hourly/" || strings.Contains(body(&m), "✗ bad") {
			t.Fatalf("repaired query=%q error=%q body=%q, want etl phase=failed /hourly/ without an error", m.Query(), m.queryErr, body(&m))
		}
		m.Update(escKey())
		if m.Query() != "etl" || m.queryErr != "" || m.SearchOn {
			t.Fatalf("cancel query=%q error=%q focused=%t, want etl, no error and no focus", m.Query(), m.queryErr, m.SearchOn)
		}
	})
}

// Query operators select exact workflow sets with strict time bounds and case-sensitive label values.
func TestQueryMatches(t *testing.T) {
	now := testkit.FixtureEpoch
	start := now.Add(-time.Hour)
	finish := now.Add(-50 * time.Minute)
	recent := now.Add(-time.Minute)
	items := []core.Summary{summary("ab", "Failed"), summary("a", "Running"), summary("b", "Running"), summary("c", "Error")}
	items[0].StartedAt = &start
	items[0].FinishedAt = &finish
	items[0].CreatedAt = now.Add(-2 * time.Hour)
	items[0].Labels = map[string]string{workflowTemplateLabel: "daily", "team": "data", "env": "prod"}
	items[1].Suspended = true
	items[1].StartedAt = &recent
	items[1].Labels = map[string]string{clusterWorkflowTemplateLabel: "cluster"}
	items[2].CreatedAt = start
	items[2].Labels = map[string]string{cronLabel: "hourly"}
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"", []string{"a", "ab", "b", "c"}},
		{"A", []string{"a", "ab"}},
		{"a b", []string{"ab"}},
		{"a|b", []string{"a", "ab", "b"}},
		{"a|b c", nil},
		{"!a", []string{"b", "c"}},
		{"!a|b", []string{"ab", "b", "c"}},
		{"!a !b", []string{"c"}},
		{"a|b !b", []string{"a"}},
		{"/^a$|^c$/", []string{"a", "c"}},
		{"/^A/", []string{"a", "ab"}},
		{"~ab", []string{"ab"}},
		{"~ba", nil},
		{"phase=failed", []string{"ab"}},
		{"phase=failed|phase=error", []string{"ab", "c"}},
		{"phase=suspended", []string{"a"}},
		{"phase=running", []string{"a", "b"}},
		{"phase=running !phase=suspended", []string{"b"}},
		{"phase!=failed phase!=error", []string{"a", "b"}},
		{"age<2m", []string{"a"}},
		{"age>30m", []string{"ab", "b"}},
		{"age<1h", []string{"a"}},
		{"age>1h", nil},
		{"dur<2m", []string{"a"}},
		{"dur>5m", []string{"ab"}},
		{"dur<10m", []string{"a"}},
		{"dur>10m", nil},
		{"tmpl=DAILY", []string{"ab"}},
		{"tmpl=cluster", []string{"a"}},
		{"tmpl!=daily", []string{"a", "b", "c"}},
		{"cron=HOURLY", []string{"b"}},
		{"cron!=hourly", []string{"a", "ab", "c"}},
		{"label:team=data", []string{"ab"}},
		{"label:team=Data", nil},
		{"label:env", []string{"ab"}},
		{"label:!team", []string{"a", "b", "c"}},
	} {
		t.Run(c.q, func(t *testing.T) {
			if got := matching(t, c.q, items, now); !slices.Equal(got, c.want) {
				t.Fatalf("matches %v want %v", got, c.want)
			}
		})
	}
}

// Namespace queries match qualified names across namespaces while anchored patterns still match bare names.
func TestQueriesAcrossNamespaces(t *testing.T) {
	for _, c := range []struct {
		q    string
		all  bool
		want []string
	}{
		{"team-a/", true, []string{"uid-a"}},
		{"etl", true, []string{"uid-a", "uid-b"}},
		{"/^team-b\\//", true, []string{"uid-b"}},
		{"/^etl$/", true, []string{"uid-a", "uid-b"}},
		{"~tma", true, []string{"uid-a"}},
		{"!team-a/", true, []string{"uid-b", "uid-m"}},
		{"team-a", false, nil},
	} {
		m := newList(t)
		m.SetAllNamespaces(c.all)
		m.SetItems(crossNamespaceItems(), testkit.FixtureEpoch)
		editQuery(&m, c.q)
		m.Update(enterKey())
		if !slices.Equal(rowIDs(m.Rows()), c.want) {
			t.Errorf("%q all=%v: %v want %v", c.q, c.all, rowIDs(m.Rows()), c.want)
		}
	}
}
