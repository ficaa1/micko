package workflowlist

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

func labelled(uid string, labels map[string]string) core.Summary {
	s := summary(uid, "Running")
	s.Labels = labels
	return s
}

func completionList(t *testing.T) Model {
	t.Helper()
	m := newList(t)
	m.SetItems([]core.Summary{
		labelled("support", map[string]string{"stack": "reporting-support", "team": "data"}),
		labelled("analytics", map[string]string{"stack": "reporting-analytics", "team": "infra"}),
		labelled("api", map[string]string{"stack": "search-api", "team": "web", "workflows.argoproj.io/workflow-template": "nightly-build"}),
		labelled("etl", map[string]string{"team": "ml", "workflows.argoproj.io/cron-workflow": "etl-hourly"}),
	}, testkit.FixtureEpoch)
	return m
}

var (
	tabKey      = tea.KeyPressMsg{Code: tea.KeyTab}
	shiftTabKey = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
)

// Tab completes the word at the cursor from the filter's fields and the
// loaded workflows' values, cycling through the matches.
func TestFilterCompletion(t *testing.T) {
	for _, c := range []struct {
		typed string
		keys  []tea.KeyPressMsg
		want  string
		shown string
	}{
		// The picker opens only once the reader types, and esc closes it.
		{"label:", []tea.KeyPressMsg{escKey()}, "label:", "(enter apply, esc cancel, ↓ clear)"},
		{"lab", []tea.KeyPressMsg{tabKey}, "label:", "▌ stack="},
		{"lab", []tea.KeyPressMsg{tabKey, tabKey}, "label:stack=", "tab: reporting-analytics  reporting-support  search-api"},
		{"label:", nil, "label:", "  4/4"},
		{"label:", []tea.KeyPressMsg{{Code: tea.KeyDown}}, "label:", "▌ team="},
		{"label:", []tea.KeyPressMsg{{Code: tea.KeyDown}, tabKey}, "label:team=", "▌ data"},
		{"label:", []tea.KeyPressMsg{{Code: tea.KeyUp}, tabKey}, "label:workflows.argoproj.io/workflow-template=", ""},
		{"label:", []tea.KeyPressMsg{tabKey, {Code: 'p', Text: "p"}}, "label:stack=p", "tab: search-api"},
		{"label:sta", []tea.KeyPressMsg{tabKey}, "label:stack=", ""},
		{"label:stack=rep", nil, "label:stack=rep", "tab: reporting-support  reporting-analytics"},
		{"label:stack=rep", []tea.KeyPressMsg{tabKey}, "label:stack=reporting-support", "tab: [reporting-support]  reporting-analytics"},
		{"label:stack=rep", []tea.KeyPressMsg{tabKey, tabKey}, "label:stack=reporting-analytics", "tab: reporting-support  [reporting-analytics]"},
		{"label:stack=rep", []tea.KeyPressMsg{tabKey, tabKey, tabKey}, "label:stack=reporting-support", ""},
		{"label:stack=rep", []tea.KeyPressMsg{shiftTabKey}, "label:stack=reporting-analytics", ""},
		{"label:stack=sup", []tea.KeyPressMsg{tabKey}, "label:stack=reporting-support", ""},
		{"label:!te", []tea.KeyPressMsg{tabKey}, "label:!team", ""},
		{"phase!=fa", []tea.KeyPressMsg{tabKey}, "phase!=failed", ""},
		{"etl !tmpl=ni", []tea.KeyPressMsg{tabKey}, "etl !tmpl=nightly-build", ""},
		{"cron=", []tea.KeyPressMsg{tabKey}, "cron=etl-hourly", ""},
		{"/lab/", []tea.KeyPressMsg{tabKey}, "/lab/", ""},
		{"lab phase=failed", []tea.KeyPressMsg{{Code: tea.KeyHome}, {Code: tea.KeyRight}, {Code: tea.KeyRight}, {Code: tea.KeyRight}, tabKey}, "label: phase=failed", ""},
	} {
		m := completionList(t)
		m.Update(runeKey('/'))
		typeText(&m, c.typed)
		for _, k := range c.keys {
			m.Update(k)
		}
		if m.searchBuf != c.want {
			t.Errorf("%q then %v: buffer %q, want %q", c.typed, c.keys, m.searchBuf, c.want)
		}
		if c.shown != "" && !strings.Contains(body(&m), c.shown) {
			t.Errorf("%q then %v: toolbar lacks %q:\n%s", c.typed, c.keys, c.shown, body(&m))
		}
	}

	m := completionList(t)
	m.Update(runeKey('/'))
	typeText(&m, "label:stack=sup")
	m.Update(tabKey)
	if !reflect.DeepEqual(rowIDs(m.Rows()), []string{"support"}) {
		t.Fatalf("a completed filter did not apply: rows %v", rowIDs(m.Rows()))
	}
}

// Up and down in the filter walk this session's applied filters, the text
// being typed and, past it, an empty filter.
func TestFilterHistory(t *testing.T) {
	m := completionList(t)
	for _, q := range []string{"phase=failed", "etl", "phase=failed"} {
		editQuery(&m, q)
		m.Update(enterKey())
	}
	up, down := tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyDown}
	m.Update(runeKey('/'))
	for _, c := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{down, ""},
		{down, ""},
		{up, "phase=failed"},
		{up, "etl"},
		{up, "etl"},
		{down, "phase=failed"},
		{down, ""},
		{up, "phase=failed"},
		{runeKey('x'), "phase=failedx"},
		{up, "phase=failed"},
		{down, "phase=failedx"},
	} {
		m.Update(c.key)
		if m.searchBuf != c.want {
			t.Fatalf("after %s the filter reads %q, want %q", c.key, m.searchBuf, c.want)
		}
	}
	m.Update(down)
	if m.Query() != "" || len(m.Rows()) != 4 {
		t.Fatalf("down did not clear the filter: query %q rows %v", m.Query(), rowIDs(m.Rows()))
	}
	m.Update(up)
	m.Update(up)
	if m.Query() != "phase=failed" || !strings.Contains(body(&m), "↑ history") {
		t.Fatalf("a recalled filter did not apply: query %q\n%s", m.Query(), body(&m))
	}
}
