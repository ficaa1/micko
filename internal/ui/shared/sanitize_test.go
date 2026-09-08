package shared

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizePreservesSafeChars(t *testing.T) {
	in := "line one\nline two\tcol3"
	if got := Sanitize(in); got != in {
		t.Errorf("Sanitize(%q) = %q, want unchanged", in, got)
	}
}

func TestSanitizeRemovesC0(t *testing.T) {
	in := "a\x00b\x07c\x08d\x0be\x0cf"
	want := "abcdef"
	if got := Sanitize(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeRemovesCarriageReturnRewrite(t *testing.T) {
	// Classic terminal spoofing: "OK\x1b[2K\rERASED" — CR must not survive.
	in := "OK\rFAKE"
	want := "OKFAKE"
	if got := Sanitize(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeRemovesCSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[2K":         "",
		"a\x1b[31mred":    "ared",
		"\x1b[1;31mX":     "X",
		"a\x1b[?25hb":     "ab",
		"a\x1b[38;5;9mZ":  "aZ",
		"keep\x1b[200Xme": "keepme",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeRemovesOSC(t *testing.T) {
	cases := map[string]string{
		// OSC 52 clipboard exfiltration
		"a\x1b]52;c;base64payload\x07b": "ab",
		// OSC 0 title change with ST terminator
		"x\x1b]0;evil title\x1b\\y": "xy",
		// OSC 8 hyperlink
		"\x1b]8;;http://evil\x1b\\link\x1b]8;;\x1b\\": "link",
		// unterminated OSC swallows to end (safe direction)
		"a\x1b]52;c;payload": "a",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeRemovesC1(t *testing.T) {
	// C1 as Unicode code points (UTF-8 encoded 0x85 NEL, 0x9B CSI-intro)
	in := "a\u0085b\u009b31m"
	want := "ab31m"
	if got := Sanitize(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSanitizeRemovesDanglingAndStringSequences(t *testing.T) {
	cases := map[string]string{
		"dangling\x1b":             "dangling",
		"charset\x1b(Bok":          "charsetok",
		"dcs\x1bPpayload\x1b\\end": "dcsend",
		"apc\x1b_payload\x1b\\e":   "apce",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFuzzNoControlOutput(t *testing.T) {
	// Deterministic pseudo-fuzz: shake inputs through a simple LCG so the
	// test is reproducible (no flaky random seeds in CI).
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
			if r == 0x1b {
				t.Fatalf("iter %d: raw ESC survived: %q", iter, got)
			}
		}
	}
}

func TestSanitizeKnownAttackStrings(t *testing.T) {
	// Record named attacks as a regression table.
	attacks := []string{
		"\x1b]52;c;Q01EQVRB\x07",                          // OSC 52 clipboard write
		"\x1b]0;pwned\x07",                                // title rewrite
		"\x1b[?1049h\x1b[?25l",                            // alt-screen/cursor games
		"run \x1b[38;2;255;0;0mred\x1b[0m text",           // SGR
		"\x1b]8;;file:///etc/passwd\x1b\\x\x1b]8;;\x1b\\", // OSC 8 file link
	}
	for _, a := range attacks {
		got := Sanitize(a)
		if strings.ContainsAny(got, "\x1b\x07\r") {
			t.Errorf("attack %q -> %q still contains ESC/BEL/CR", a, got)
		}
	}
}

func TestRedactTokens(t *testing.T) {
	cases := map[string]string{
		"Bearer abc123def456ghi789jkl012":                  "Bearer [REDACTED]",
		"authorization: Bearer   tok-with-dashes_and.dots": "authorization: Bearer [REDACTED]",
		"BEARER MixedCaseToken123456789012345":             "BEARER [REDACTED]",
		"no secret here":                                   "no secret here",
		"short bearer x":                                   "short bearer [REDACTED]", // conservative: token-shaped run after marker is masked
	}
	for in, want := range cases {
		got := RedactTokens(in)
		if got != want {
			t.Errorf("RedactTokens(%q) = %q, want %q", in, got, want)
		}
	}
	// long base64url-ish blob
	in := "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9 remains"
	got := RedactTokens(in)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiIsInR5") {
		t.Errorf("long token not redacted: %q", got)
	}
}

func TestRedactThenSanitizeComposition(t *testing.T) {
	// Error surfaces apply redaction; views apply sanitization; both must
	// compose without corrupting normal text.
	in := "Authorization: Bearer deadbeefcafe123456789012 failed"
	out := Sanitize(RedactTokens(in))
	if strings.Contains(out, "deadbeefcafe123456789012") {
		t.Fatalf("token survived: %q", out)
	}
	if !strings.Contains(out, "failed") {
		t.Errorf("normal text damaged: %q", out)
	}
}
