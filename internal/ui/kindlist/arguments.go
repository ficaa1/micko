package kindlist

import (
	"strings"

	"github.com/ficaa1/argo-tui/internal/core"
)

// Redacted is what a parameter value shows until the reader reveals it. It is
// the resource tab's marker, so every pane redacts alike.
const Redacted = "[REDACTED]"

// ArgumentFields renders workflow arguments for an info panel, one parameter
// per line: its value, or where the value comes from, then its default and
// its allowed values when it has them, then its description.
//
// Values and defaults are redacted unless reveal is on; the allowed values
// are shown either way, because they are part of the template's interface,
// not data passed through it.
func ArgumentFields(args []core.Argument, reveal bool) []Field {
	if len(args) == 0 {
		return []Field{{Label: "Arguments", Value: "none"}}
	}
	var f []Field
	for i, a := range args {
		label := ""
		if i == 0 {
			label = "Arguments"
		}
		f = append(f, Field{Label: label, Value: argumentLine(a, reveal)})
	}
	return f
}

func argumentLine(a core.Argument, reveal bool) string {
	var b strings.Builder
	b.WriteString(a.Name + " = ")
	switch {
	case a.HasValue && reveal:
		b.WriteString(a.Value)
	case a.HasValue:
		b.WriteString(Redacted)
	case a.ValueFrom != "":
		b.WriteString("(from " + a.ValueFrom + ")")
	case a.Default != "":
		b.WriteString("(the default)")
	default:
		b.WriteString("(no value: supplied at submission)")
	}
	if a.Default != "" {
		d := Redacted
		if reveal {
			d = a.Default
		}
		b.WriteString(" · default " + d)
	}
	if len(a.Enum) > 0 {
		b.WriteString(" · one of: " + strings.Join(a.Enum, ", "))
	}
	if a.Description != "" {
		b.WriteString(" — " + a.Description)
	}
	return b.String()
}
