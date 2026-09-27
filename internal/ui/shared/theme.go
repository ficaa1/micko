package shared

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is the styling surface for view packages: one semantic token per
// role, so a view asks for "the accent" or "a muted label" and never names a
// colour. A skin (skins.go) fills every token from one palette.
//
// Colour is never the only carrier of a status. Every coloured phase is also
// a glyph and a word, and every badge is also a word, so a mono terminal,
// NO_COLOR and a colour-blind reader lose nothing but the hue.
//
// NO_COLOR (https://no-color.org) is respected: when set to any non-empty
// value, every style degrades to plain text whatever skin was asked for.
type Theme struct {
	// Skin names the palette this theme was built from. The plain theme
	// reports "plain".
	Skin string

	// Phase accent colors.
	PhaseRunning   lipgloss.Style
	PhaseSucceeded lipgloss.Style
	PhaseFailed    lipgloss.Style
	PhasePending   lipgloss.Style
	PhaseOther     lipgloss.Style

	// Structure.
	//
	// Header styles standalone pane headings. Dim is the same style as Muted;
	// both names stay because both read naturally at their call sites.
	Header    lipgloss.Style
	Selected  lipgloss.Style
	Dim       lipgloss.Style
	Warning   lipgloss.Style
	ErrorText lipgloss.Style

	// Text roles. Text is ordinary body text; it matters where a background
	// is painted underneath (the header band), because the terminal's own
	// foreground may not suit the skin's band colour. Muted is secondary
	// text: labels, hint descriptions, annotations. Accent is the skin's
	// signature colour.
	Text   lipgloss.Style
	Muted  lipgloss.Style
	Accent lipgloss.Style

	// Tables and rows. TableHeader styles column heads. Marked styles a row
	// the reader has marked for a bulk operation; a mark is also a glyph, so
	// the style only repeats it. TreeGuide styles the connectors of a tree
	// (├─ └─ │), which must read dimmer than the names they join.
	TableHeader lipgloss.Style
	Marked      lipgloss.Style
	TreeGuide   lipgloss.Style

	// Bars. BarFill is the finished part of a progress or timeline bar,
	// BarRunning the part still in progress, and BarEmpty the track behind
	// both. Bars are drawn with glyphs, so these set foregrounds.
	BarFill    lipgloss.Style
	BarEmpty   lipgloss.Style
	BarRunning lipgloss.Style

	// Shell chrome: the frame border, the pane title inside it, and the
	// key-hint footer band. Kept separate from Header so the pane title can
	// be emphasised without restyling table column heads. BorderShape holds
	// the border's characters; a zero value draws square corners.
	Border      lipgloss.Style
	BorderShape lipgloss.Border
	Title       lipgloss.Style
	Footer      lipgloss.Style

	// Band is the header band's base: when it sets a background, the whole
	// row is painted and every segment on it keeps that background.
	// AppName styles the program name and version at its left end.
	Band    lipgloss.Style
	AppName lipgloss.Style

	// The safety-mode badge at the right of the header band. Actions
	// enabled is the state that can change a cluster, so it gets the louder
	// style; the words READ ONLY and ACTIONS ENABLED carry the meaning.
	BadgeReadOnly lipgloss.Style
	BadgeActions  lipgloss.Style

	// Key hints: the key a reader presses and what it does.
	HintKey  lipgloss.Style
	HintDesc lipgloss.Style

	// Section tabs, such as the detail pane's Summary / Nodes / Resource.
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	// Mićko, the mascot (micko.go): his body, his wings and tail, the dark
	// of his scallops, eye and feet, and his beak and cheek. He is
	// decoration, so these tokens carry no meaning a mono terminal loses.
	MickoBody lipgloss.Style
	MickoWing lipgloss.Style
	MickoDark lipgloss.Style
	MickoBeak lipgloss.Style
}

// NewTheme builds the default skin's theme; noColor forces plain output.
func NewTheme(noColor bool) Theme {
	if noColor || hasNoColorEnv() {
		return plainTheme()
	}
	return defaultTheme()
}

func hasNoColorEnv() bool {
	v, ok := os.LookupEnv("NO_COLOR")
	return ok && v != ""
}

// defaultTheme is the ANSI-16 skin. It names palette indexes rather than
// colours, so the terminal's own palette decides the actual hues and the
// skin suits a dark or a light background alike.
func defaultTheme() Theme {
	ansi := func(n string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(n)) }
	faint := lipgloss.NewStyle().Faint(true)
	return Theme{
		Skin:           SkinDefault,
		PhaseRunning:   ansi("6"), // cyan
		PhaseSucceeded: ansi("2"), // green
		PhaseFailed:    ansi("1"), // red
		PhasePending:   ansi("7"), // light gray
		PhaseOther:     ansi("5"), // magenta
		Header:         lipgloss.NewStyle().Bold(true),
		Selected:       lipgloss.NewStyle().Reverse(true),
		Dim:            faint,
		Warning:        ansi("3"), // yellow
		ErrorText:      ansi("1").Bold(true),
		// Text is the terminal's own foreground: the default skin paints no
		// background, so there is nothing for the text to be matched to.
		Text:          lipgloss.NewStyle(),
		Muted:         faint,
		Accent:        ansi("6"),
		TableHeader:   lipgloss.NewStyle().Bold(true),
		Marked:        ansi("5").Bold(true),
		TreeGuide:     ansi("8"),
		BarFill:       ansi("2"),
		BarEmpty:      ansi("8"),
		BarRunning:    ansi("6"),
		Border:        ansi("8"), // dim gray
		BorderShape:   lipgloss.NormalBorder(),
		Title:         ansi("6").Bold(true),
		Footer:        faint,
		Band:          lipgloss.NewStyle(),
		AppName:       ansi("6").Bold(true),
		BadgeReadOnly: ansi("2").Bold(true),
		BadgeActions:  ansi("1").Bold(true).Reverse(true),
		HintKey:       ansi("6").Bold(true),
		HintDesc:      faint,
		TabActive:     ansi("6").Bold(true),
		TabInactive:   faint,
		MickoBody:     ansi("1"), // red
		MickoWing:     ansi("4"), // blue
		MickoDark:     ansi("8"), // dark gray: black would vanish on a dark terminal
		MickoBeak:     ansi("7"), // light gray
	}
}

func plainTheme() Theme {
	none := lipgloss.NewStyle()
	return Theme{
		Skin:           "plain",
		PhaseRunning:   none,
		PhaseSucceeded: none,
		PhaseFailed:    none,
		PhasePending:   none,
		PhaseOther:     none,
		Header:         none,
		Selected:       none,
		Dim:            none,
		Warning:        none,
		ErrorText:      none,
		Text:           none,
		Muted:          none,
		Accent:         none,
		TableHeader:    none,
		Marked:         none,
		TreeGuide:      none,
		BarFill:        none,
		BarEmpty:       none,
		BarRunning:     none,
		Border:         none,
		BorderShape:    lipgloss.NormalBorder(),
		Title:          none,
		Footer:         none,
		Band:           none,
		AppName:        none,
		BadgeReadOnly:  none,
		BadgeActions:   none,
		HintKey:        none,
		HintDesc:       none,
		TabActive:      none,
		TabInactive:    none,
		MickoBody:      none,
		MickoWing:      none,
		MickoDark:      none,
		MickoBeak:      none,
	}
}

// Borders returns the border characters to draw. A theme built without a
// shape (a zero Theme in a test) gets square corners rather than a border
// made of empty strings, which would shift every body line left.
func (t Theme) Borders() lipgloss.Border {
	if t.BorderShape.TopLeft == "" {
		return lipgloss.NormalBorder()
	}
	return t.BorderShape
}

// IsBlock reports whether s paints the cells behind its text, with a
// background colour or with reverse video. A block style needs its row
// padded to the full width, or the highlight stops where the text does.
func IsBlock(s lipgloss.Style) bool {
	return s.GetReverse() || HasBackground(s)
}

// HasBackground reports whether s sets a background colour.
func HasBackground(s lipgloss.Style) bool {
	return isColor(s.GetBackground())
}

// HasForeground reports whether s sets a foreground colour.
func HasForeground(s lipgloss.Style) bool {
	return isColor(s.GetForeground())
}

func isColor(c color.Color) bool {
	if c == nil {
		return false
	}
	_, none := c.(lipgloss.NoColor)
	return !none
}

// SelectRow styles the selected row of a list. When the selection is a
// block the row is padded to width first, so the highlight is a bar across
// the pane instead of a patch behind the text. A plain selection is left
// unpadded: trailing spaces would change nothing on screen but would be
// copied by a mouse selection.
func (t Theme) SelectRow(line string, width int) string {
	if width > 0 && IsBlock(t.Selected) {
		line = padCells(line, width)
	}
	return t.Selected.Render(line)
}

// padCells pads s with spaces to width terminal cells.
func padCells(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// PhaseStyle returns the style for a workflow/node phase string. Unknown
// phases get PhaseOther and must remain displayable (LIST-11).
func (t Theme) PhaseStyle(phase string) lipgloss.Style {
	switch phase {
	case "Suspended":
		return t.Warning
	case "Running":
		return t.PhaseRunning
	case "Succeeded":
		return t.PhaseSucceeded
	case "Failed", "Error":
		return t.PhaseFailed
	case "Pending":
		return t.PhasePending
	default:
		return t.PhaseOther
	}
}

// PhaseSymbol returns a one-cell glyph for a workflow or node phase.
//
// Color must never be the only carrier of a status (UI-03/07): a mono
// terminal, NO_COLOR, and a color-blind reader all lose the hue but keep the
// glyph. The glyph is supplementary to the phase word, which is still
// rendered in full — it is a third channel, not a replacement.
//
// Every glyph is exactly one rune and one terminal cell, so the PHASE column
// keeps its alignment. Failed and Error deliberately share a glyph: they are
// the same outcome to a reader scanning the list, and the phase word still
// tells them apart.
func PhaseSymbol(phase string) string {
	switch phase {
	case "Suspended":
		// A workflow parked on a manual gate. It is running, but nothing
		// moves until a person resumes it, so it gets its own glyph.
		return "◐" // half-filled circle: waiting on a person
	case "Running":
		return "●" // filled circle: work in progress
	case "Succeeded":
		return "✓" // check
	case "Failed", "Error":
		return "✗" // ballot X
	case "Pending":
		return "○" // hollow circle: not started
	default:
		// LIST-11: a phase the server invented still needs a visible cell.
		return "•"
	}
}
