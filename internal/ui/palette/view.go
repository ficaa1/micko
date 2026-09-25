package palette

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// MaxRows is the most suggestions the list shows. More than this and the
// palette hides the pane it is moving around in; the ranking puts the useful
// rows first, so the reader types one more letter instead of scrolling.
const MaxRows = 8

// labelCap bounds the name column so one long argument value cannot push the
// descriptions off a narrow pane.
const labelCap = 28

// Hints is the footer while the palette is open.
func (m *Model) Hints() string {
	return "tab complete  ↑↓ pick  enter run  ctrl+p/n history  esc close"
}

// BodyLines renders the palette: the input line, up to rows suggestions and a
// rule that separates them from the pane underneath. The root puts these
// lines at the top of the active pane and gives the pane what is left, so
// the view the reader is navigating away from stays visible.
//
// Every row is clipped to width cells (0 means unknown and leaves them
// alone). The highlighted row carries a "›" marker as well as the selection
// style, so it stays visible under NO_COLOR.
func (m *Model) BodyLines(width, rows int) []string {
	if rows > MaxRows {
		rows = MaxRows
	}
	lines := []string{m.theme.Title.Render(":") + " " + shared.Sanitize(m.buf) + "_"}

	sugg := m.Suggestions()
	if len(sugg) == 0 {
		lines = append(lines, m.theme.Dim.Render("  "+m.emptyReason()))
	} else {
		sel := m.clampSel(len(sugg))
		// The window follows the highlight, so moving past the last shown
		// row brings the next one into view instead of hiding the cursor.
		start := 0
		if rows > 0 && sel >= rows {
			start = sel - rows + 1
		}
		end := len(sugg)
		if rows > 0 && end > start+rows {
			end = start + rows
		}
		labelW, aliasW := columnWidths(sugg[start:end])
		for i := start; i < end; i++ {
			lines = append(lines, m.row(sugg[i], i == sel, labelW, aliasW))
		}
	}
	rule := ""
	if width > 0 {
		rule = strings.Repeat("─", width)
	}
	lines = append(lines, m.theme.Border.Render(rule))
	if width > 0 {
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, width, "…")
		}
	}
	return lines
}

// row renders one suggestion: marker, label, aliases, description. An
// unselected row dims its description so the command names are what the eye
// lands on; the selected row is styled whole, since a dim span inside it
// would end the selection style halfway along the row.
func (m *Model) row(s Suggestion, selected bool, labelW, aliasW int) string {
	mark := "  "
	if selected {
		mark = "› "
	}
	line := mark + pad(truncate(shared.Sanitize(s.Label), labelW), labelW)
	if aliasW > 0 {
		line += "  " + pad(strings.Join(s.Aliases, ", "), aliasW)
	}
	desc := shared.Sanitize(s.Desc)
	if selected {
		if desc != "" {
			line += "  " + desc
		}
		return m.theme.Selected.Render(line)
	}
	if desc != "" {
		line += "  " + m.theme.Dim.Render(desc)
	}
	return line
}

// emptyReason says why the list is empty, so an empty list never reads as a
// palette that stopped working.
func (m *Model) emptyReason() string {
	word, arg, staged := Parse(m.buf)
	if !staged {
		return "no command matches " + quote(word) + " — esc closes"
	}
	c, ok := Find(m.commands, word)
	switch {
	case !ok:
		return "no command named " + quote(word)
	case c.Arg == "":
		return c.Name + " takes no argument"
	case arg == "":
		return "no " + c.Arg + " names known yet — type one"
	default:
		return "no known " + c.Arg + " matches " + quote(arg)
	}
}

func quote(s string) string { return "“" + shared.Sanitize(s) + "”" }

// columnWidths sizes the label and alias columns to the rows on screen.
func columnWidths(rows []Suggestion) (label, alias int) {
	for _, s := range rows {
		if w := ansi.StringWidth(shared.Sanitize(s.Label)); w > label {
			label = w
		}
		if w := ansi.StringWidth(strings.Join(s.Aliases, ", ")); w > alias {
			alias = w
		}
	}
	if label > labelCap {
		label = labelCap
	}
	return label, alias
}

func pad(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}
