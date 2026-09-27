package shared

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Micko is the project's mascot, a crimson rosella: red body, blue wings and
// tail, a back scalloped in black, and a pale beak and cheek. He is drawn as
// ASCII art with a colour mask of the same shape, so the drawing stays plain
// text and only the theme decides whether it is coloured.
//
// A mask cell names the part the character above it belongs to:
//
//	r body   b wing and tail   k scallops, eye and feet   w beak and cheek
//
// A space in the mask is not part of Micko. What shows through there is the
// caller's: blank cells above the pane, the border line under his feet, the
// project name beside him.

// MickoPerch is Micko perched on the pane's top border with his head bowed
// over the edge, the way he rests his beak on a monitor. The last row is
// drawn onto the border itself: only its masked cells replace border line.
var MickoPerch = Art{
	Lines: []string{
		`      _.-~~~~-._    `,
		"====-'^v^v^v^v^v `. ",
		"      `-.\\\\\\\\\\__ o )",
		`       /_/    \v/   `,
	},
	Mask: []string{
		`      rrrrrrrrrr    `,
		`bbbbrrrkrkrkrkrk rr `,
		`      rrrbbbbbrr k r`,
		`       kkk    www   `,
	},
}

// MickoWordmark is Micko facing the reader beside the project name.
var MickoWordmark = Art{
	Lines: []string{
		`    .---.                _        _`,
		`   ( o o )    _ __ ___  (_)  ___ | | __  ___`,
		"  ((  V  ))  | '_ ` _ \\ | | / __|| |/ / / _ \\",
		`   (     )   | | | | | || || (__ |   < | (_) |`,
		`  ~~"~~~"~~  |_| |_| |_||_| \___||_|\_\ \___/`,
	},
	Mask: []string{
		`    rrrrr`,
		`   r k k r`,
		`  ww  w  ww`,
		`   r     r`,
		`  kkkkkkkkk`,
	},
}

// Art is a drawing and its colour mask. Lines and Mask have the same number
// of rows; a mask row shorter than its line leaves the rest unmasked. Every
// character is one cell wide.
type Art struct {
	Lines []string
	Mask  []string
}

// Width is the widest row, in cells.
func (a Art) Width() int {
	w := 0
	for _, l := range a.Lines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	return w
}

// Cell reports the character at row, col and the style the theme gives it.
// ok is false for a cell outside the drawing or not masked as part of Micko.
func (a Art) Cell(t Theme, row, col int) (ch string, style lipgloss.Style, ok bool) {
	if row < 0 || row >= len(a.Lines) || col < 0 {
		return "", lipgloss.Style{}, false
	}
	line, mask := []rune(a.Lines[row]), []rune(a.Mask[row])
	if col >= len(line) || col >= len(mask) {
		return "", lipgloss.Style{}, false
	}
	style, ok = t.mickoPart(mask[col])
	return string(line[col]), style, ok
}

// RenderRow draws one row of the drawing. Micko's cells take his colours;
// any other character (the wordmark's text) is drawn in text. Neighbouring
// cells of one part are drawn as one run, so a row costs a few escapes
// rather than one per character.
func (a Art) RenderRow(t Theme, row int, text lipgloss.Style) string {
	line := []rune(a.Lines[row])
	var mask []rune
	if row < len(a.Mask) {
		mask = []rune(a.Mask[row])
	}
	// part is the mask cell a character is drawn as: its own mask cell, a
	// space for a blank, or 't' for text beside Micko.
	part := func(col int) rune {
		if col < len(mask) && mask[col] != ' ' {
			return mask[col]
		}
		if line[col] == ' ' {
			return ' '
		}
		return 't'
	}
	var b strings.Builder
	for start := 0; start < len(line); {
		p := part(start)
		end := start + 1
		for end < len(line) && part(end) == p {
			end++
		}
		run := string(line[start:end])
		switch st, ok := t.mickoPart(p); {
		case ok:
			b.WriteString(st.Render(run))
		case p == 't':
			b.WriteString(text.Render(run))
		default:
			b.WriteString(run)
		}
		start = end
	}
	return b.String()
}

// mickoPart maps a mask cell to its token.
func (t Theme) mickoPart(m rune) (lipgloss.Style, bool) {
	switch m {
	case 'r':
		return t.MickoBody, true
	case 'b':
		return t.MickoWing, true
	case 'k':
		return t.MickoDark, true
	case 'w':
		return t.MickoBeak, true
	}
	return lipgloss.Style{}, false
}
