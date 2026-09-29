package logs

import (
	"strings"
	"testing"
)

// Confirming the context editor switches the pane's container and asks the
// root to reopen the stream for it. Opening the editor asks for nothing.
func TestContextSwitchEmitsIntent(t *testing.T) {
	m := testModel(t)
	if cmd := press(m, 'c'); cmd != nil {
		t.Fatal("c asked the root for something; it only opens the editor")
	}
	typeInto(m, "x") // the editor is prefilled with "main"
	cmd := pressKey(m, keyEnter)
	if cmd == nil || m.contextOn || m.container != "mainx" {
		t.Fatalf("confirm: cmd=%v open=%v container=%q", cmd != nil, m.contextOn, m.container)
	}
	intent, ok := cmd().(SwitchContextIntent)
	if !ok || intent.Container != "mainx" || intent.PodName != "" || intent.Ref != testRef() {
		t.Fatalf("intent = %#v", intent)
	}
}

// An emptied container is refused with the editor kept open: the container
// is never guessed.
func TestContextBlankContainerRefused(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	for range "main" {
		pressKey(m, keyBackspace)
	}
	pressKey(m, keyEnter)
	if !m.contextOn {
		t.Fatal("a blank container closed the editor")
	}
	if body := strings.Join(m.BodyLines(), "\n"); !strings.Contains(body, "must not be empty") {
		t.Fatalf("no refusal on screen:\n%s", body)
	}
}

// Esc closes the editor without applying the edit or asking for a stream.
func TestContextEscCancels(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	typeInto(m, "x")
	if cmd := pressKey(m, keyEscape); cmd != nil || m.contextOn || m.container != "main" {
		t.Fatalf("esc: cmd=%v open=%v container=%q", cmd != nil, m.contextOn, m.container)
	}
}

// While an editor has focus, printable keys are text: they neither pause,
// nor open another editor. Backspace edits.
func TestEditorsConsumeCommandKeys(t *testing.T) {
	for _, c := range []struct {
		name, open string
		key        rune
		buf        func(*Model) string
	}{
		{"space in the context editor", "c", ' ', func(m *Model) string { return m.containerBuf }},
		{"c in the search editor", "/", 'c', func(m *Model) string { return m.searchBuf }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := testModel(t)
			typeInto(m, c.open)
			before := c.buf(m)
			press(m, c.key)
			if m.paused || (c.open == "/" && m.contextOn) {
				t.Fatalf("%q acted as a command", c.key)
			}
			if got := c.buf(m); got != before+string(c.key) {
				t.Fatalf("editor holds %q, want %q", got, before+string(c.key))
			}
			pressKey(m, keyBackspace)
			if got := c.buf(m); got != before {
				t.Fatalf("backspace left %q", got)
			}
		})
	}
}

// The root owns q, n, p and ?: in browse mode the pane lets them through
// untouched.
func TestRootKeysPassThrough(t *testing.T) {
	m := testModel(t)
	for _, k := range []rune{'q', 'n', 'p', '?'} {
		if cmd := press(m, k); cmd != nil {
			t.Fatalf("%q produced a command", k)
		}
	}
	if m.searchOn || m.contextOn || m.pipeOn {
		t.Fatal("a root key opened an editor")
	}
}
