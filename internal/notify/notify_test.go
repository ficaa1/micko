package notify

import "testing"

// A workflow name cannot end the OSC 9 sequence or start another one.
func TestOSC9(t *testing.T) {
	for _, c := range []struct{ name, title, want string }{
		{"plain", "etl", "\x1b]9;etl: Running → Failed\a"},
		{"escape in name", "etl\x1b]52;c;evil\a", "\x1b]9;etl]52;c;evil: Running → Failed\a"},
		{"C1 terminator", "etl\u009c", "\x1b]9;etl: Running → Failed\a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := OSC9(Notice{Title: c.title, Body: "Running → Failed"}); got != c.want {
				t.Fatalf("OSC9 = %q, want %q", got, c.want)
			}
		})
	}
}
