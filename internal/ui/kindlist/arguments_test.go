package kindlist

import (
	"testing"

	"github.com/ficaa1/micko/internal/core"
)

// Each argument form renders on one line: values and defaults redacted until
// reveal, the source of a run-time value, the allowed values always, and the
// description last.
func TestArgumentFields(t *testing.T) {
	args := []core.Argument{
		{Name: "a", Value: "secret", HasValue: true},
		{Name: "b", Value: "", HasValue: true},
		{Name: "c", ValueFrom: "configmap x key y"},
		{Name: "d", Default: "dflt"},
		{Name: "e"},
		{Name: "f", Value: "eu", HasValue: true, Enum: []string{"eu", "us"}, Description: "region"},
	}
	redacted := []string{
		"a = [REDACTED]",
		"b = [REDACTED]",
		"c = (from configmap x key y)",
		"d = (the default) · default [REDACTED]",
		"e = (no value: supplied at submission)",
		"f = [REDACTED] · one of: eu, us — region",
	}
	revealed := []string{
		"a = secret",
		"b = ",
		"c = (from configmap x key y)",
		"d = (the default) · default dflt",
		"e = (no value: supplied at submission)",
		"f = eu · one of: eu, us — region",
	}
	for i, f := range ArgumentFields(args, false) {
		if f.Value != redacted[i] {
			t.Errorf("redacted #%d = %q, want %q", i, f.Value, redacted[i])
		}
		if (i == 0) != (f.Label == "Arguments") {
			t.Errorf("#%d label %q", i, f.Label)
		}
	}
	for i, f := range ArgumentFields(args, true) {
		if f.Value != revealed[i] {
			t.Errorf("revealed #%d = %q, want %q", i, f.Value, revealed[i])
		}
	}
	if fs := ArgumentFields(nil, false); len(fs) != 1 || fs[0].Value != "none" {
		t.Fatalf("no arguments = %+v", fs)
	}
}
