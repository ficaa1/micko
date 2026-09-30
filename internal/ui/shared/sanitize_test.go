package shared

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Sanitize preserves readable text while removing terminal control sequences.
func TestSanitize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"safe newlines and tabs", "line one\nline two\tcol3", "line one\nline two\tcol3"},
		{"C0", "a\x00b\x07c\x08d\x0be\x0cf", "abcdef"},
		{"carriage return", "OK\rFAKE", "OKFAKE"},
		{"CSI erase", "\x1b[2K", ""},
		{"CSI foreground", "a\x1b[31mred", "ared"},
		{"CSI compound", "\x1b[1;31mX", "X"},
		{"CSI private mode", "a\x1b[?25hb", "ab"},
		{"CSI palette", "a\x1b[38;5;9mZ", "aZ"},
		{"CSI final", "keep\x1b[200Xme", "keepme"},
		{"OSC clipboard", "a\x1b]52;c;base64payload\x07b", "ab"},
		{"OSC title ST", "x\x1b]0;evil title\x1b\\y", "xy"},
		{"OSC hyperlink", "\x1b]8;;http://evil\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"unfinished OSC", "a\x1b]52;c;payload", "a"},
		{"C1", "a\u0085b\u009b31m", "ab31m"},
		{"dangling escape", "dangling\x1b", "dangling"},
		{"charset", "charset\x1b(Bok", "charsetok"},
		{"DCS", "dcs\x1bPpayload\x1b\\end", "dcsend"},
		{"APC", "apc\x1b_payload\x1b\\e", "apce"},
		{"clipboard attack", "\x1b]52;c;Q01EQVRB\x07", ""},
		{"title attack", "\x1b]0;pwned\x07", ""},
		{"alternate screen", "\x1b[?1049h\x1b[?25l", ""},
		{"RGB color", "run \x1b[38;2;255;0;0mred\x1b[0m text", "run red text"},
		{"file hyperlink", "\x1b]8;;file:///etc/passwd\x1b\\x\x1b]8;;\x1b\\", "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sanitize(c.in); got != c.want {
				t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeFuzzNoControlOutput(t *testing.T) {
	// A fixed seed makes generated control-sequence combinations reproducible.
	seed := uint32(0x9e3779b9)
	next := func() byte {
		seed = seed*1664525 + 1013904223
		return byte(seed >> 24)
	}
	alphabet := []rune{'a', 'b', '\n', '\t', 0x1b, '[', ']', 'P', 'X', '^', '_', ';',
		'0', '1', '2', '\\', 0x07, '\r', 'm', '?', '$', ' ', 0x85, 0x9b, 0x9c}
	for iter := 0; iter < 2000; iter++ {
		var b strings.Builder
		length := int(next()%40) + 1
		for i := 0; i < length; i++ {
			b.WriteRune(alphabet[int(next())%len(alphabet)])
		}
		in := b.String()
		got := Sanitize(in)
		if !utf8.ValidString(got) {
			t.Fatalf("iter %d: output not valid UTF-8: %q", iter, got)
		}
		for _, r := range got {
			switch r {
			case '\n', '\t':
				continue
			}
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				t.Fatalf("iter %d: control rune %U survived sanitize of %q -> %q", iter, r, in, got)
			}
		}
	}
}

func TestRedactTokens(t *testing.T) {
	cases := map[string]string{
		"Bearer abc123def456ghi789jkl012":                    "Bearer [REDACTED]",
		"authorization: Bearer   tok-with-dashes_and.dots":   "authorization: Bearer [REDACTED]",
		"BEARER MixedCaseToken123456789012345":               "BEARER [REDACTED]",
		"no secret here":                                     "no secret here",
		"token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9 remains": "[REDACTED] remains",
		"short bearer x":                                     "short bearer [REDACTED]", // conservative: token-shaped run after marker is masked
	}
	for in, want := range cases {
		got := RedactTokens(in)
		if got != want {
			t.Errorf("RedactTokens(%q) = %q, want %q", in, got, want)
		}
	}

}

// Error redaction and terminal sanitization both survive their composition.
func TestRedactThenSanitizeComposition(t *testing.T) {
	in := "Authorization: Bearer deadbeefcafe123456789012 \x1b[31mfailed\x1b[0m"
	want := "Authorization: Bearer [REDACTED] failed"
	if got := Sanitize(RedactTokens(in)); got != want {
		t.Errorf("Sanitize(RedactTokens(%q)) = %q, want %q", in, got, want)
	}
}
