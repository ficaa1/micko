package workflowlist

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func newList(t *testing.T) Model {
	t.Helper()
	m := New(shared.NewTheme(true))
	m.SetSize(100, 20)
	return m
}

func testTheme() shared.Theme {
	return shared.NewTheme(true)
}

func summariesFrom(wfs []core.Workflow) []core.Summary {
	var out []core.Summary
	for _, wf := range wfs {
		out = append(out, wf.Summary)
	}
	return out
}

func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func spaceKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
}

func escKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}

func enterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}

func backspaceKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyBackspace}
}

func typeText(m *Model, text string) {
	for _, r := range text {
		if r == ' ' {
			m.Update(spaceKey())
		} else {
			m.Update(runeKey(r))
		}
	}
}

func editQuery(m *Model, q string) {
	if m.SearchOn {
		m.Update(escKey())
	}
	m.Update(runeKey('/'))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	for range []rune(m.searchBuf) {
		m.Update(backspaceKey())
	}
	typeText(m, q)
}

func body(m *Model) string {
	return strings.Join(m.BodyLines(testkit.FixtureEpoch), "\n")
}

func rowNames(rows []core.Summary) []string {
	var out []string
	for _, s := range rows {
		out = append(out, s.Ref.Name)
	}
	return out
}

func rowIDs(rows []core.Summary) []string {
	var out []string
	for _, s := range rows {
		out = append(out, s.Ref.UID)
	}
	return out
}

func summary(uid, phase string) core.Summary {
	return core.Summary{Ref: core.Ref{Namespace: "ns", Name: uid, UID: uid}, Phase: phase}
}

// Selection keys emit the selected workflow reference and section, and empty lists emit no selection intent.
func TestListIntents(t *testing.T) {
	ref := core.Ref{Namespace: "ns", Name: "target", UID: "target-uid"}
	for _, c := range []struct {
		key  tea.KeyPressMsg
		want tea.Msg
	}{
		{enterKey(), shared.OpenWorkflowMsg{Ref: ref}},
		{runeKey('l'), shared.OpenLogsMsg{Ref: ref, Container: "main"}},
		{runeKey('T'), shared.OpenWorkflowMsg{Ref: ref, Section: shared.SectionTimeline}},
		{runeKey('X'), shared.OpenWorkflowMsg{Ref: ref, Section: shared.SectionExplain}},
		{runeKey('E'), shared.OpenWorkflowMsg{Ref: ref, Section: shared.SectionEvents}},
		{runeKey('r'), RefreshListMsg{}},
	} {
		t.Run(c.key.String(), func(t *testing.T) {
			m := newList(t)
			m.SetItems([]core.Summary{summary("a-decoy", ""), {Ref: ref}}, testkit.FixtureEpoch)
			m.Update(runeKey('j'))
			cmd := m.Update(c.key)
			var got tea.Msg
			if cmd != nil {
				got = cmd()
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("key %q intent = %#v, want %#v", c.key.String(), got, c.want)
			}
			m.SetItems(nil, testkit.FixtureEpoch)
			if c.key.String() != "r" {
				if cmd := m.Update(c.key); cmd != nil {
					t.Fatalf("empty selection key %q emitted %#v, want no intent", c.key.String(), cmd())
				}
			}
		})
	}
}

// Selection stays on its workflow UID across reordering and falls back when that UID disappears.
func TestSelectionAcrossSnapshots(t *testing.T) {
	m := newList(t)
	a, b, c := summary("a", "Running"), summary("b", "Running"), summary("c", "Running")
	m.SetItems([]core.Summary{a, b, c}, testkit.FixtureEpoch)
	m.Update(runeKey('j'))
	b.Phase = "Succeeded"
	m.SetItems([]core.Summary{c, b, a}, testkit.FixtureEpoch)
	if got := rowIDs(m.Rows()); !reflect.DeepEqual(got, []string{"a", "c", "b"}) || m.SelectedRef().UID != "b" {
		t.Fatalf("reordered rows %v selection %+v", got, m.SelectedRef())
	}
	m.SetItems([]core.Summary{c, a}, testkit.FixtureEpoch)
	if m.SelectedRef().UID != "a" {
		t.Fatalf("deletion fallback %+v", m.SelectedRef())
	}
	replacement := a
	replacement.Ref.UID = "replacement"
	replacement.Phase = "Succeeded"
	m.SetItems([]core.Summary{replacement, c}, testkit.FixtureEpoch)
	if m.SelectedRef().UID != "c" {
		t.Fatalf("selection followed name to replacement: %+v", m.SelectedRef())
	}
	m.SetItems(nil, testkit.FixtureEpoch)
	if m.SelectedRef() != (core.Ref{}) {
		t.Fatalf("empty snapshot selection = %+v, want a zero reference", m.SelectedRef())
	}
}

// Focused search edits Unicode text, filters live, and isolates command keys through commit and cancellation.
func TestSearchEditing(t *testing.T) {
	t.Run("command letters", func(t *testing.T) {
		for _, r := range "wjqrpsT XE" {
			if r == ' ' {
				continue
			}
			m := newList(t)
			m.SetItems([]core.Summary{summary("one", "Running")}, testkit.FixtureEpoch)
			m.Update(runeKey('/'))
			if m.Update(runeKey(r)) != nil || m.searchBuf != string(r) || m.wide || m.MarkCount() != 0 || m.phase != PhaseAll || m.sort != SortPhaseName {
				t.Fatalf("%c dispatched during text entry: %+v", r, m)
			}
		}
	})
	t.Run("cursor and Unicode", func(t *testing.T) {
		m := newList(t)
		m.Update(runeKey('/'))
		if !strings.Contains(body(&m), "[_]  (enter apply, esc cancel)") {
			t.Fatalf("empty input body = %q, want cursor and apply/cancel hint", body(&m))
		}
		typeText(&m, "abc")
		m.Update(backspaceKey())
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		typeText(&m, "X")
		if m.searchBuf != "aXb" {
			t.Fatalf("buffer after cursor insertion = %q, want aXb", m.searchBuf)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if m.searchBuf != "aXb" {
			t.Fatalf("buffer after Tab = %q, want aXb", m.searchBuf)
		}
		m.Update(enterKey())
		if m.SearchOn || m.Query() != "aXb" || m.searchBuf != "" {
			t.Fatalf("commit query=%q focused=%t buffer=%q, want aXb, false and empty", m.Query(), m.SearchOn, m.searchBuf)
		}
		m.Update(runeKey('/'))
		if m.searchBuf != "aXb" {
			t.Fatalf("reopened buffer = %q, want aXb", m.searchBuf)
		}
		typeText(&m, "日本")
		m.Update(backspaceKey())
		if m.searchBuf != "aXb日" {
			t.Fatalf("buffer after Unicode backspace = %q, want aXb日", m.searchBuf)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
		typeText(&m, "z")
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
		typeText(&m, "y")
		if m.searchBuf != "zaXb日y" {
			t.Fatalf("buffer after Home/End edits = %q, want zaXb日y", m.searchBuf)
		}
	})
	t.Run("live commit cancel clear", func(t *testing.T) {
		m := newList(t)
		m.SetItems([]core.Summary{summary("apple", "Running"), summary("banana", "Running")}, testkit.FixtureEpoch)
		editQuery(&m, "app")
		if !reflect.DeepEqual(rowIDs(m.Rows()), []string{"apple"}) {
			t.Fatalf("live query rows = %v, want [apple]", rowIDs(m.Rows()))
		}
		m.Update(enterKey())
		editQuery(&m, "absent")
		if len(m.Rows()) != 0 || m.SelectedRef().UID != "" {
			t.Fatalf("absent query rows=%v selection=%+v, want no rows or selection", rowIDs(m.Rows()), m.SelectedRef())
		}
		m.Update(escKey())
		if m.Query() != "app" || !reflect.DeepEqual(rowIDs(m.Rows()), []string{"apple"}) {
			t.Fatalf("cancel query=%q rows=%v, want app and [apple]", m.Query(), rowIDs(m.Rows()))
		}
		editQuery(&m, "absent")
		m.Update(enterKey())
		m.Update(escKey())
		if m.Query() != "" || len(m.Rows()) != 2 || m.SearchOn {
			t.Fatalf("Escape query=%q rows=%v focused=%t, want empty query, [apple banana] and no focus", m.Query(), rowIDs(m.Rows()), m.SearchOn)
		}
	})
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch:\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func setTimeZone(t *testing.T, loc *time.Location) {
	t.Helper()
	previous := time.Local
	time.Local = loc
	t.Cleanup(func() {
		time.Local = previous
	})
}
