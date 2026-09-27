package kindlist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// A kind with Open instead of Drill opens the row on enter and says so; a
// toolbar note is shown until cleared; an empty note is appended to the
// empty-namespace line and wrapped rather than clipped.
func TestOpenNoteAndEmptyNote(t *testing.T) {
	spec := thingSpec(true)
	spec.Drill = nil
	spec.Open = func(t thing) tea.Msg { return "open " + t.name }
	spec.EmptyNote = "(which can also mean the server keeps none of them at all)"
	m := New(spec, shared.NewTheme(true))
	m.SetSize(60, 20)
	m.SetItems(things(), epoch)
	m.SetStatus(StatusIdle, "", 0)
	if got := key(m, "enter")(); got != "open large" {
		t.Fatalf("enter = %v", got)
	}
	if !strings.HasPrefix(m.Hints(), "enter open") {
		t.Fatalf("hints = %q", m.Hints())
	}
	m.SetNote("newest 3 only")
	if !strings.Contains(body(m), "newest 3 only") {
		t.Fatal("note not shown")
	}
	m.Reset()
	m.SetItems(nil, epoch)
	m.SetStatus(StatusIdle, "", 0)
	out := body(m)
	if strings.Contains(out, "newest 3 only") || !strings.Contains(out, "keeps none of them at all)") {
		t.Fatalf("after reset:\n%s", out)
	}
}

// With ID set, two rows of one name are two identities.
func TestIDIdentity(t *testing.T) {
	spec := thingSpec(true)
	spec.ID = func(t thing) string { return t.secret }
	m := New(spec, shared.NewTheme(true))
	m.SetSize(100, 20)
	m.SetItems([]thing{{"a", "same", 2, "id-2"}, {"a", "same", 1, "id-1"}}, epoch)
	key(m, "j")
	if sel, _ := m.Selected(); sel.secret != "id-1" {
		t.Fatalf("j selected %q", sel.secret)
	}
	m.SetItems([]thing{{"a", "same", 1, "id-1"}, {"a", "same", 2, "id-2"}}, epoch)
	if sel, _ := m.Selected(); sel.secret != "id-1" {
		t.Fatalf("after refresh the cursor is on %q", sel.secret)
	}
}
