package detail

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// handleSummaryKey pans and wraps the summary's lines, and reports whether
// it took key.
func (m *Model) handleSummaryKey(key string) bool {
	switch key {
	case "w":
		m.summaryWrap = !m.summaryWrap
		m.summaryLeft = 0
		m.jumpTo(m.summaryTop)
	case "h", "left":
		m.summaryPan(-m.summaryStep())
	case "l", "right":
		m.summaryPan(m.summaryStep())
	case "0":
		m.summaryLeft = 0
	case "$":
		m.summaryPan(m.summaryWidest())
	default:
		return false
	}
	return true
}

// summaryStep is one horizontal pan: half the pane, so a cut word stays in
// view.
func (m *Model) summaryStep() int {
	return max(1, m.width/2)
}

// summaryPan moves the left edge, stopping where the widest line's end meets
// the right edge of the pane. Wrapped lines ignore it.
func (m *Model) summaryPan(delta int) {
	if m.summaryWrap {
		return
	}
	m.summaryLeft = min(max(m.summaryLeft+delta, 0), max(m.summaryWidest()-m.width, 0))
}

// summaryWidest returns the display width of the summary's widest line.
func (m *Model) summaryWidest() int {
	w := 0
	for _, l := range m.summaryLines() {
		w = max(w, ansi.StringWidth(l))
	}
	return w
}

// summaryLines is the summary as screen rows: one per line, or with wrapping
// on, as many as each line needs at the pane width.
func (m *Model) summaryLines() []string {
	lines := strings.Split(strings.TrimRight(m.summaryText(), "\n"), "\n")
	if !m.summaryWrap || m.width < 1 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.Split(ansi.Hardwrap(l, m.width, true), "\n")...)
	}
	return out
}

// summaryWindow is the rows on screen, with the cells left of the pan
// position cut off.
func (m *Model) summaryWindow() []string {
	all := m.summaryLines()
	// The width, the reveal state and a refresh all change the row count
	// under the anchor, so the anchor is clamped where the rows are drawn.
	m.summaryTop = min(m.summaryTop, max(len(all)-m.viewRows(), 0))
	lines := sliceLines(all, m.summaryTop, m.viewRows())
	if m.summaryLeft > 0 {
		for i, l := range lines {
			lines[i] = ansi.TruncateLeft(l, m.summaryLeft, "")
		}
	}
	return lines
}

// summaryCut reports whether a line is wider than the pane, so panning or
// wrapping would show more of it.
func (m *Model) summaryCut() bool {
	return m.width > 0 && m.summaryWidest() > m.width
}

func (m *Model) summaryStatusLine() string {
	s := "summary"
	if m.summaryLeft > 0 {
		s += " · col " + strconv.Itoa(m.summaryLeft+1)
	}
	return m.theme.Dim.Render(s + " · tab changes section")
}

func (m *Model) summaryHints() string {
	pan := ""
	switch {
	case m.summaryWrap:
		pan = "  w unwrap"
	case m.summaryCut():
		pan = "  h/l pan  w wrap"
	}
	return "tab section  1-9 jump" + pan + "  v reveal  y copy  a actions  f raw  r refresh  esc back"
}
