package detail

import (
	"strings"
	"testing"
)

// The digits jump to a section by its position in the strip, from 1, and a
// section's letter jumps to it. A digit past the last section and any other
// key jump nowhere.
func TestSectionForKey(t *testing.T) {
	cases := map[string]string{
		"1": "summary", "2": "nodes", "3": "timeline", "4": "explain", "5": "resource",
		"T": "timeline", "X": "explain",
		"9": "", "0": "", "t": "", "x": "", "enter": "",
	}
	for key, want := range cases {
		got, ok := sectionForKey(key)
		if got != want || ok != (want != "") {
			t.Errorf("sectionForKey(%q) = %q, %v; want %q", key, got, ok, want)
		}
	}
}

// tab and shift+tab walk the strip in order and wrap at both ends.
func TestSectionCycleWraps(t *testing.T) {
	var fwd, back []string
	for cur, i := "summary", 0; i < len(sections); i++ {
		cur = nextTab(cur)
		fwd = append(fwd, cur)
	}
	for cur, i := "summary", 0; i < len(sections); i++ {
		cur = prevTab(cur)
		back = append(back, cur)
	}
	if got := strings.Join(fwd, " "); got != "nodes timeline explain resource summary" {
		t.Errorf("forward %q", got)
	}
	if got := strings.Join(back, " "); got != "resource explain timeline nodes summary" {
		t.Errorf("backward %q", got)
	}
	if nextTab("bogus") != "summary" || prevTab("bogus") != "resource" {
		t.Error("an unknown section does not cycle to an end of the strip")
	}
}

// SetSection accepts the known sections only.
func TestSetSection(t *testing.T) {
	m := New()
	if !m.SetSection("timeline") || m.Section() != "timeline" {
		t.Fatalf("SetSection(timeline): section %q", m.Section())
	}
	if m.SetSection("bogus") || m.Section() != "timeline" {
		t.Fatalf("SetSection(bogus) changed the section to %q", m.Section())
	}
}
