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
		labelled("etl", map[string]string{"team": "data-eng", "workflows.argoproj.io/cron-workflow": "etl-hourly"}),
	}, testkit.FixtureEpoch)
	return m
}

var (
	tabKey      = tea.KeyPressMsg{Code: tea.KeyTab}
	shiftTabKey = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
)

// Tab completes the word at the cursor from the filter's fields and the
// loaded workflows' values. With more than three values a picker opens and
// stays open as they narrow: up, down and tab move, enter accepts.
func TestFilterCompletion(t *testing.T) {
	up, down := tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyDown}
	for _, c := range []struct {
		typed string
		keys  []tea.KeyPressMsg
		want  string
		shown string
	}{
		{"lab", []tea.KeyPressMsg{tabKey}, "label:", "▌ stack="},
		{"lab", []tea.KeyPressMsg{tabKey, enterKey()}, "label:stack=", "▌ reporting-analytics"},
		{"label:", nil, "label:", "  4/4"},
		{"label:", []tea.KeyPressMsg{down}, "label:", "▌ team="},
		{"label:", []tea.KeyPressMsg{tabKey}, "label:", "▌ team="},
		{"label:", []tea.KeyPressMsg{tabKey, shiftTabKey}, "label:", "▌ stack="},
		{"label:", []tea.KeyPressMsg{down, enterKey()}, "label:team=", "▌ data"},
		{"label:", []tea.KeyPressMsg{up, enterKey()}, "label:workflows.argoproj.io/workflow-template=", ""},
		{"label:te", nil, "label:te", "  2/4"},
		{"label:team=x", nil, "label:team=x", "  0/4"},
		{"label:stack=rep", nil, "label:stack=rep", "▌ reporting-support"},
		{"label:stack=rep", []tea.KeyPressMsg{down, enterKey()}, "label:stack=reporting-analytics", "(enter apply, esc cancel"},
		{"label:!te", []tea.KeyPressMsg{enterKey()}, "label:!team", ""},
		{"phase!=fa", []tea.KeyPressMsg{enterKey()}, "phase!=failed", ""},
		{"label:", []tea.KeyPressMsg{escKey()}, "label:", "(enter apply, esc cancel, ↓ clear)"},
		// Fewer than four values that never opened the picker cycle inline.
		{"label:stack=rep", []tea.KeyPressMsg{escKey(), tabKey}, "label:stack=reporting-support", "tab: [reporting-support]  reporting-analytics"},
		{"label:stack=rep", []tea.KeyPressMsg{escKey(), tabKey, tabKey}, "label:stack=reporting-analytics", ""},
		{"label:stack=rep", []tea.KeyPressMsg{escKey(), tabKey, tabKey, tabKey}, "label:stack=reporting-support", ""},
		{"label:stack=rep", []tea.KeyPressMsg{escKey(), shiftTabKey}, "label:stack=reporting-analytics", ""},
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

	// Enter picks, then applies once the word reads the picked value.
	m := completionList(t)
	m.Update(runeKey('/'))
	typeText(&m, "label:stack=sup")
	m.Update(enterKey())
	m.Update(enterKey())
	if m.SearchOn || !reflect.DeepEqual(rowIDs(m.Rows()), []string{"support"}) {
		t.Fatalf("enter did not pick and then apply: open %v rows %v", m.SearchOn, rowIDs(m.Rows()))
	}
	// A value typed in full applies on the first enter, even when a longer
	// value also matches.
	m.Update(runeKey('/'))
	m.Update(down)
	typeText(&m, "label:team=data")
	m.Update(enterKey())
	if m.SearchOn || m.Query() != "label:team=data" {
		t.Fatalf("enter did not apply the typed value: open %v query %q", m.SearchOn, m.Query())
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
