package shared

import "testing"

// Wrapping keeps words and paragraphs in order and measures terminal cells.
func TestWrap(t *testing.T) {
	cases := []struct {
		name, in string
		width    int
		want     string
	}{
		{"unsized", "a long error message", 0, "a long error message"},
		{"negative width", "a long error message", -1, "a long error message"},
		{"words", "server returned an HTML page instead of API data tail-marker", 20, "server returned an\nHTML page instead of\nAPI data tail-marker"},
		{"long error", "server returned an HTML page instead of API data (content-type text/html; charset=UTF-8); this endpoint expects interactive browser login configure a server token do not retry automatically tail-marker", 40, "server returned an HTML page instead of\nAPI data (content-type text/html;\ncharset=UTF-8); this endpoint expects\ninteractive browser login configure a\nserver token do not retry automatically\ntail-marker"},
		{"paragraphs", "one two\n\nthree four", 5, "one\ntwo\n\nthree\nfour"},
		{"wide cells", "界 界 界", 4, "界\n界\n界"},
		{"long token", "abcdefgh next", 4, "abcdefgh\nnext"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Wrap(c.in, c.width); got != c.want {
				t.Errorf("Wrap(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
			}
		})
	}
}
