package detail

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// sections.go is the detail pane's section list: the order of the tab strip,
// which tab and shift+tab cycle through and which 1..9 index.

// section is one detail section: the ID the model keeps as its tab, the
// title the strip shows, and the capital letter that jumps to it from
// anywhere in the detail pane and opens the workflow on it from the list.
type section struct {
	id    string
	title string
	key   string
}

// sections is the tab strip, in order. The position is the digit key.
var sections = []section{
	{id: "summary", title: "Summary"},
	{id: "nodes", title: "Nodes"},
	{id: "timeline", title: "Timeline", key: "T"},
	{id: "explain", title: "Explain", key: "X"},
	{id: "events", title: "Events", key: "E"},
	{id: "resource", title: "Resource"},
}

// sectionIndex is id's position in the strip, and -1 for an unknown ID.
func sectionIndex(id string) int {
	for i, s := range sections {
		if s.id == id {
			return i
		}
	}
	return -1
}

// HasSection reports whether id names a detail section.
func HasSection(id string) bool { return sectionIndex(id) >= 0 }

// nextTab is the section after cur, wrapping to the first. An unknown
// section moves to the first.
func nextTab(cur string) string {
	i := sectionIndex(cur)
	return sections[(i+1)%len(sections)].id
}

// prevTab is the section before cur, wrapping to the last. An unknown
// section moves to the last.
func prevTab(cur string) string {
	i := sectionIndex(cur)
	if i <= 0 {
		return sections[len(sections)-1].id
	}
	return sections[i-1].id
}

// sectionForKey is the section a key jumps to: a digit by position, from
// 1, or a section's letter. ok is false for any other key.
func sectionForKey(key string) (string, bool) {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		i := int(key[0] - '1')
		if i < len(sections) {
			return sections[i].id, true
		}
		return "", false
	}
	for _, s := range sections {
		if s.key != "" && s.key == key {
			return s.id, true
		}
	}
	return "", false
}

// tabStrip marks the active section. Text carries the state, not color: the
// active tab is the one wrapped in brackets, so a mono terminal keeps it. The
// theme only repeats it, drawing the active tab in the accent and the others
// muted.
func tabStrip(active string, t shared.Theme) string {
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		if strings.EqualFold(s.id, active) {
			out = append(out, t.TabActive.Render("["+s.title+"]"))
			continue
		}
		out = append(out, t.TabInactive.Render(" "+s.title+" "))
	}
	return strings.Join(out, " ")
}

// tabStripFit is the tab strip within width cells. The full strip is drawn
// when it fits. Otherwise the tabs close up to one space apart, and when
// even that is too wide, the strip shows the tabs around the active one
// with "…" where tabs are left out, so the active tab is always on screen
// and the strip never runs past the pane. A zero width is unbounded.
func tabStripFit(active string, t shared.Theme, width int) string {
	full := tabStrip(active, t)
	if width <= 0 || cellWidth(full) <= width {
		return full
	}
	type tab struct {
		text  string
		style lipgloss.Style
	}
	tabs := make([]tab, len(sections))
	for i, s := range sections {
		tabs[i] = tab{s.title, t.TabInactive}
		if strings.EqualFold(s.id, active) {
			tabs[i] = tab{"[" + s.title + "]", t.TabActive}
		}
	}
	cur := max(sectionIndex(active), 0)
	lo, hi := 0, len(tabs)-1
	need := func(lo, hi int) int {
		w := 0
		for i := lo; i <= hi; i++ {
			w += cellWidth(tabs[i].text) + 1
		}
		if lo > 0 {
			w += 2
		}
		if hi < len(tabs)-1 {
			w += 2
		}
		return w - 1
	}
	if need(lo, hi) > width {
		lo, hi = cur, cur
		for grown := true; grown; {
			grown = false
			if hi+1 < len(tabs) && need(lo, hi+1) <= width {
				hi++
				grown = true
			}
			if lo > 0 && need(lo-1, hi) <= width {
				lo--
				grown = true
			}
		}
	}
	var p pieces
	if lo > 0 {
		p.add("… ", t.TabInactive)
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			p.add(" ", lipgloss.NewStyle())
		}
		p.add(tabs[i].text, tabs[i].style)
	}
	if hi < len(tabs)-1 {
		p.add(" …", t.TabInactive)
	}
	return p.truncate(width).render()
}
