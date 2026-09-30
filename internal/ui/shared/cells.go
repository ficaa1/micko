package shared

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// PadRight pads text to width terminal cells.
func PadRight(s string, width int) string {
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// PadLeft right-aligns text in width terminal cells.
func PadLeft(s string, width int) string {
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}
