package logs

import (
	"os"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// testRef is the synthetic workflow every model test attaches to.
func testRef() core.Ref {
	return core.Ref{Namespace: "ns", Name: "wf-1", UID: "u1"}
}

// testModel builds a workflow-wide pane with the plain theme, sized 80×24.
func testModel(t *testing.T) *Model {
	t.Helper()
	return sizedModel(NewModel(testRef(), "", "main"))
}

// sizedModel gives m the plain theme and an 80×24 pane.
func sizedModel(m *Model) *Model {
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(80, 24)
	return m
}

// colorModel is testModel with the coloured theme, for the tests that check
// a style was applied and where.
func colorModel(t *testing.T) *Model {
	t.Helper()
	os.Unsetenv("NO_COLOR")
	m := NewModel(testRef(), "", "main")
	m.SetTheme(shared.NewTheme(false))
	m.SetSize(80, 24)
	return m
}

// pane is everything the shell draws for the logs route, in order: the
// border title, the body, the footer hints and the status cell.
func pane(m *Model) string {
	return m.PaneTitle() + "\n" + strings.Join(m.BodyLines(), "\n") + "\n" + m.Hints() + "\n" + m.PaneStatus()
}

// body is the pane body as one string.
func body(m *Model) string { return strings.Join(m.BodyLines(), "\n") }

// press sends one printable key through Update and returns the command.
func press(m *Model, r rune) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
}

// pressKey sends a named (non-printable) key through Update.
func pressKey(m *Model, k rune) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: k})
}

// ctrlT is the timestamps toggle.
func ctrlT() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl} }

// Special-key runes (the bubbletea constants' values).
const (
	keyEnter     = tea.KeyEnter
	keyEscape    = tea.KeyEscape
	keyBackspace = tea.KeyBackspace
)

// typeInto types s into the focused editor.
func typeInto(m *Model, s string) {
	for _, r := range s {
		press(m, r)
	}
}

// rec is one record from pod-1.
func rec(content string) core.LogRecord { return podRec("pod-1", content) }

// podRec is one record from the named pod.
func podRec(pod, content string) core.LogRecord {
	return core.LogRecord{PodName: pod, Container: "main", Content: content, ReceivedAt: testkit.FixtureEpoch}
}

// recs builds n distinct records, "line-0" to "line-<n-1>".
func recs(n int) []core.LogRecord {
	out := make([]core.LogRecord, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rec("line-"+itoa(i)))
	}
	return out
}

// itoa formats test line numbers.
var itoa = strconv.Itoa

// windowTexts is the text of each row in the scroll window.
func windowTexts(m *Model) []string {
	w := m.window()
	out := make([]string, 0, len(w))
	for _, r := range w {
		out = append(out, r.text)
	}
	return out
}
