package namespaces

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

func newPicker(current string, names ...string) *Model {
	m := New(shared.NewTheme(true))
	m.Open(current)
	m.SetNames(names, "test", nil)
	return m
}

func press(m *Model, s string) tea.Cmd {
	switch s {
	case "enter":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	case "esc":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	case "down":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	case "backspace":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	return m.Update(tea.KeyPressMsg{Code: rune(s[0]), Text: s})
}

// The cursor starts on the namespace in use, so the reader can see where they
// are before they move.
func TestPickerStartsOnTheCurrentNamespace(t *testing.T) {
	m := newPicker("beta", "alpha", "beta", "gamma")
	if got := m.Selected(); got != "beta" {
		t.Fatalf("selected = %q, want the current namespace", got)
	}
}

// Typing narrows the list without a separate search mode to enter and leave.
func TestTypingNarrowsTheList(t *testing.T) {
	m := newPicker("alpha", "batch-cd-prd", "batch-cd-tst", "reports-prd")
	for _, r := range "tst" {
		press(m, string(r))
	}
	if len(m.rows) != 1 || m.rows[0] != "batch-cd-tst" {
		t.Fatalf("filter did not narrow: %v", m.rows)
	}
	press(m, "backspace")
	press(m, "backspace")
	press(m, "backspace")
	if len(m.rows) != 4 { // three plus the current namespace, which is merged in
		t.Fatalf("backspace did not widen the list again: %v", m.rows)
	}
}

// A namespace with no workflows cannot be derived from a list of workflows.
// Enter must still switch to it, or that namespace is unreachable.
func TestTypedNamespaceWithNoMatchStillSwitches(t *testing.T) {
	m := newPicker("alpha", "alpha", "beta")
	for _, r := range "brand-new" {
		press(m, string(r))
	}
	if len(m.rows) != 0 {
		t.Fatalf("expected no match, got %v", m.rows)
	}
	cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a typed namespace emitted nothing")
	}
	msg, ok := cmd().(SwitchMsg)
	if !ok || msg.Namespace != "brand-new" {
		t.Fatalf("switch message = %#v, want brand-new", cmd())
	}
	if m.IsOpen() {
		t.Fatal("the dialog stayed open after switching")
	}
}

// Choosing the namespace already in use is not a switch: it would cancel every
// request and reload the same list.
func TestChoosingTheCurrentNamespaceOnlyCloses(t *testing.T) {
	m := newPicker("alpha", "alpha", "beta")
	if cmd := press(m, "enter"); cmd != nil {
		t.Fatalf("re-selecting the current namespace emitted %#v", cmd())
	}
	if m.IsOpen() {
		t.Fatal("the dialog stayed open")
	}
}

// Esc leaves everything alone.
func TestEscCancels(t *testing.T) {
	m := newPicker("alpha", "alpha", "beta")
	press(m, "down")
	if cmd := press(m, "esc"); cmd != nil {
		t.Fatal("esc emitted a switch")
	}
	if m.IsOpen() {
		t.Fatal("esc did not close the dialog")
	}
}

// A denied request must not look like a cluster with no namespaces, and the
// typed entry has to stay usable.
func TestFailedFetchSaysSoAndKeepsTypedEntry(t *testing.T) {
	m := New(shared.NewTheme(true))
	m.Open("alpha")
	m.SetError("namespaces are forbidden for this token")
	m.SetNames(nil, "", []string{"beta"})
	body := strings.Join(m.BodyLines(), "\n")
	if !strings.Contains(body, "would not list namespaces") {
		t.Fatalf("failure not explained:\n%s", body)
	}
	if !strings.Contains(body, "beta") {
		t.Fatalf("configured namespace not offered:\n%s", body)
	}
}
