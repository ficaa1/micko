package shared

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Mićko is the project's mascot, an eastern rosella: red body, blue wings and
// tail, a back scalloped in black, and a pale beak and cheek. He is drawn as
// ASCII art with a colour mask of the same shape, so the drawing stays plain
// text and only the theme decides whether it is coloured.
//
// A mask cell names the part the character above it belongs to:
//
//	r body   b wing and tail   k scallops, eye and feet   w beak and cheek
//
// A space in the mask is not part of Mićko. What shows through there is the
// caller's: blank cells above the pane, the border line under his feet, the
// project name beside him.

// MickoPerch is Mićko perched on the pane's top border with his head bowed
// over the edge, the way he rests his beak on a monitor. The last row is
// drawn onto the border itself: only its masked cells replace border line.
// It is his resting pose; the other poses (MickoPerchBeats) share its width
// and its feet, so he never shifts on the border as he moves.
var MickoPerch = Art{
	Lines: []string{
		`      _.-~~~~-._      `,
		"====-'^v^v^v^v^v `.   ",
		"      `-.\\\\\\\\\\__ o )  ",
		`       /_/    \v/     `,
	},
	Mask: []string{
		`      rrrrrrrrrr      `,
		`bbbbrrrkrkrkrkrk rr   `,
		`      rrrbbbbbrr k r  `,
		`       kkk    www     `,
	},
}

// MickoPerchLeft and MickoPerchRight are Mićko with his head lifted off the
// pane, looking left toward the pane's title and right past its count. His
// beak leaves the border, so the border line shows again where it was.
var (
	MickoPerchLeft = Art{
		Lines: []string{
			`      _.-~~~~-._ .-.  `,
			`====-'^v^v^v^v^<( o ) `,
			"      `-.\\\\\\\\\\__.-'   ",
			`       /_/            `,
		},
		Mask: []string{
			`      rrrrrrrrrr rrr  `,
			`bbbbrrrkrkrkrkrwrrkrr `,
			`      rrrbbbbbrrrww   `,
			`       kkk            `,
		},
	}
	MickoPerchRight = Art{
		Lines: []string{
			`      _.-~~~~-._ .-.  `,
			`====-'^v^v^v^v^v( o )>`,
			"      `-.\\\\\\\\\\__.-'   ",
			`       /_/            `,
		},
		Mask: []string{
			`      rrrrrrrrrr rrr  `,
			`bbbbrrrkrkrkrkrkrrkrrw`,
			`      rrrbbbbbrrrww   `,
			`       kkk            `,
		},
	}
)

// Beat is one step of Mićko's animation: a pose and how long he holds it.
type Beat struct {
	Pose Art
	Hold time.Duration
}

// MickoPerchBeats is Mićko's perched routine, played on a loop. He spends
// most of it resting his beak on the pane, blinking now and then and once
// dozing off, then lifts his head to look left and right before settling
// back down. A pass takes about 45 seconds.
var MickoPerchBeats = func() []Beat {
	rest, shut := MickoPerch, closedEye(MickoPerch)
	left, right := MickoPerchLeft, MickoPerchRight
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	return []Beat{
		{rest, ms(5000)}, {shut, ms(150)},
		{rest, ms(6000)}, {shut, ms(150)},
		{rest, ms(4000)}, {shut, ms(2000)}, // dozing
		{rest, ms(7000)}, {shut, ms(150)},
		{rest, ms(8000)},
		{right, ms(1400)}, {closedEye(right), ms(150)}, {right, ms(900)},
		{left, ms(1400)}, {right, ms(1000)}, {left, ms(900)},
		{closedEye(left), ms(150)}, {left, ms(700)},
	}
}()

// closedEye is a with its eye shut.
func closedEye(a Art) Art {
	lines := make([]string, len(a.Lines))
	for i, l := range a.Lines {
		lines[i] = strings.Replace(l, " o ", " - ", 1)
	}
	return Art{Lines: lines, Mask: a.Mask}
}

// MickoWordmark is Mićko facing the reader beside the project name.
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
// ok is false for a cell outside the drawing or not masked as part of Mićko.
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

// RenderRow draws one row of the drawing. Mićko's cells take his colours;
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
	// space for a blank, or 't' for text beside Mićko.
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
