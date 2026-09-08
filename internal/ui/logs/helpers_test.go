package logs

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

// testRef is the synthetic workflow every model test attaches to.
func testRef() core.Ref {
	return core.Ref{Namespace: "ns", Name: "wf-1", UID: "u1"}
}

// testModel builds the standard log-viewer model sized like the
// acceptance-matrix default terminal (80×24). Viewport rows: 24-4 = 20.
func testModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(testRef(), "", "main")
	m.SetNoColor(true)
	m.SetSize(80, 24)
	return m
}

// runeKey builds a KeyPressMsg for a printable character.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// press sends one key through the model's Update loop and returns any
// command the model produced.
func press(m *Model, r rune) tea.Cmd {
	return m.Update(runeKey(r))
}

// pressKey sends a named (non-printable) key through Update and returns
// any command the model produced.
func pressKey(m *Model, k rune) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: k})
}

// Special-key runes (uv/ansi values; identical to the bubbletea constants).
const (
	keyEnter     = 13  // ansi.CR / tea.KeyEnter
	keyEscape    = 27  // ansi.ESC / tea.KeyEscape
	keyBackspace = 127 // ansi.DEL / tea.KeyBackspace
)

// recs builds n distinct synthetic records ("line-00".."line-n-1").
func recs(n int) []core.LogRecord {
	out := make([]core.LogRecord, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rec("line-"+itoa(i)))
	}
	return out
}

// rec builds one synthetic record (testkit.FixtureEpoch pins determinism).
func rec(content string) core.LogRecord {
	return core.LogRecord{PodName: "pod-1", Container: "main", Content: content, ReceivedAt: testkit.FixtureEpoch}
}

// itoa is the test-formatting helper (strconv at call sites in tests).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// typeInto types s into the focused entry widget (search or context).
func typeInto(m *Model, s string) {
	for _, r := range s {
		press(m, r)
	}
}

// viewportRows is the expected window height for the standard test model.
func viewportRows(m *Model) int {
	return m.viewRows()
}
