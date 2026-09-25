package detail

import (
	"strings"
	"testing"
)

// An archived workflow says so in the title and the summary, and the hints
// drop the actions key; a live one opened after it does not.
func TestArchivedMarker(t *testing.T) {
	m := loadedModel(t)
	m.SetArchived(true)
	if !strings.HasSuffix(m.PaneTitle(), "(archived)") {
		t.Fatalf("title = %q", m.PaneTitle())
	}
	body := strings.Join(m.BodyLines(), "\n")
	if !strings.Contains(body, "source:    the workflow archive") {
		t.Fatalf("summary does not say archived:\n%s", body)
	}
	if strings.Contains(m.Hints(), "a actions") {
		t.Fatalf("hints offer actions: %q", m.Hints())
	}
	m.SetArchived(false)
	if strings.Contains(m.PaneTitle(), "archived") || strings.Contains(strings.Join(m.BodyLines(), "\n"), "workflow archive") {
		t.Fatal("the marker outlived SetArchived(false)")
	}
	if !strings.Contains(m.Hints(), "a actions") {
		t.Fatalf("live hints lost actions: %q", m.Hints())
	}
}
