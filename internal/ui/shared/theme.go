package shared

import (
	"os"

	"charm.land/lipgloss/v2"
)

// Theme is the frozen styling surface for view packages. Colors are
// dark-friendly but never assume a black background; every colored status
// is also carried by text (plan §2: "Status text accompanies colors").
//
// NO_COLOR (https://no-color.org) is respected: when set to any non-empty
// value, all styles degrade to plain text.
type Theme struct {
	// Phase accent colors.
	PhaseRunning   lipgloss.Style
	PhaseSucceeded lipgloss.Style
	PhaseFailed    lipgloss.Style
	PhasePending   lipgloss.Style
	PhaseOther     lipgloss.Style

	// Structure.
	Header    lipgloss.Style
	Selected  lipgloss.Style
	Dim       lipgloss.Style
	Warning   lipgloss.Style
	ErrorText lipgloss.Style

	// Shell chrome: the frame border, the pane title inside it, and the
	// key-hint footer band. Kept separate from Header so the pane title can
	// be emphasised without restyling table column heads.
	Border lipgloss.Style
	Title  lipgloss.Style
	Footer lipgloss.Style
}

// NewTheme builds the default theme; noColor forces plain output.
func NewTheme(noColor bool) Theme {
	if noColor || hasNoColorEnv() {
		return plainTheme()
	}
	return coloredTheme()
}

func hasNoColorEnv() bool {
	v, ok := os.LookupEnv("NO_COLOR")
	return ok && v != ""
}

func coloredTheme() Theme {
	return Theme{
		PhaseRunning:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
		PhaseSucceeded: lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
		PhaseFailed:    lipgloss.NewStyle().Foreground(lipgloss.Color("1")), // red
		PhasePending:   lipgloss.NewStyle().Foreground(lipgloss.Color("7")), // light gray
		PhaseOther:     lipgloss.NewStyle().Foreground(lipgloss.Color("5")), // magenta
		Header:         lipgloss.NewStyle().Bold(true),
		Selected:       lipgloss.NewStyle().Reverse(true),
		Dim:            lipgloss.NewStyle().Faint(true),
		Warning:        lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
		ErrorText:      lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		Border:         lipgloss.NewStyle().Foreground(lipgloss.Color("8")), // dim gray
		Title:          lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		Footer:         lipgloss.NewStyle().Faint(true),
	}
}

func plainTheme() Theme {
	return Theme{
		PhaseRunning:   lipgloss.NewStyle(),
		PhaseSucceeded: lipgloss.NewStyle(),
		PhaseFailed:    lipgloss.NewStyle(),
		PhasePending:   lipgloss.NewStyle(),
		PhaseOther:     lipgloss.NewStyle(),
		Header:         lipgloss.NewStyle(),
		Selected:       lipgloss.NewStyle(),
		Dim:            lipgloss.NewStyle(),
		Warning:        lipgloss.NewStyle(),
		ErrorText:      lipgloss.NewStyle(),
		Border:         lipgloss.NewStyle(),
		Title:          lipgloss.NewStyle(),
		Footer:         lipgloss.NewStyle(),
	}
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
		return "\u25d0" // half-filled circle: waiting on a person
	case "Running":
		return "\u25cf" // filled circle: work in progress
	case "Succeeded":
		return "\u2713" // check
	case "Failed", "Error":
		return "\u2717" // ballot X
	case "Pending":
		return "\u25cb" // hollow circle: not started
	default:
		// LIST-11: a phase the server invented still needs a visible cell.
		return "\u2022"
	}
}
