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
	}
}

// PhaseStyle returns the style for a workflow/node phase string. Unknown
// phases get PhaseOther and must remain displayable (LIST-11).
func (t Theme) PhaseStyle(phase string) lipgloss.Style {
	switch phase {
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
