package kindlist

import (
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// Each argument form renders on one line: values and defaults redacted until
// reveal, the source of a run-time value, the allowed values always, and the
// description last. Only the first line carries the label.
func TestArgumentFields(t *testing.T) {
	cases := []struct {
		arg                core.Argument
		redacted, revealed string
	}{
		{core.Argument{Name: "a", Value: "secret", HasValue: true}, "a = [REDACTED]", "a = secret"},
		{core.Argument{Name: "b", Value: "", HasValue: true}, "b = [REDACTED]", "b = "},
		{core.Argument{Name: "c", ValueFrom: "configmap x key y"}, "c = (from configmap x key y)", "c = (from configmap x key y)"},
		{core.Argument{Name: "d", Default: "dflt"}, "d = (the default) · default [REDACTED]", "d = (the default) · default dflt"},
		{core.Argument{Name: "e"}, "e = (no value: supplied at submission)", "e = (no value: supplied at submission)"},
		{core.Argument{Name: "f", Value: "eu", HasValue: true, Enum: []string{"eu", "us"}, Description: "region"},
			"f = [REDACTED] · one of: eu, us — region", "f = eu · one of: eu, us — region"},
	}
	var args []core.Argument
	for _, c := range cases {
		args = append(args, c.arg)
	}
	for _, reveal := range []bool{false, true} {
		fields := ArgumentFields(args, reveal)
		if len(fields) != len(cases) {
			t.Fatalf("reveal %v: %d fields, want %d", reveal, len(fields), len(cases))
		}
		for i, c := range cases {
			want, label := c.redacted, ""
			if reveal {
				want = c.revealed
			}
			if i == 0 {
				label = "Arguments"
			}
			if fields[i] != (Field{Label: label, Value: want}) {
				t.Errorf("reveal %v, %s: %+v, want %q %q", reveal, c.arg.Name, fields[i], label, want)
			}
		}
	}
	if got := ArgumentFields(nil, false); len(got) != 1 || got[0] != (Field{Label: "Arguments", Value: "none"}) {
		t.Errorf("no arguments = %+v", got)
	}
}
