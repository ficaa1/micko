// Package shared holds UI primitives used by every view: theme, keys and
// the terminal-attack sanitizer, which every view applies to untrusted text
// before rendering it, not only logs.
package shared

// Sanitize neutralizes terminal control sequences in untrusted text before
// it reaches the terminal:
//
//   - C0 control characters except \n and \t are removed (including CR, so
//     carriage-return rewriting cannot redraw a line).
//   - C1 controls (0x80–0x9F as Unicode code points) are removed.
//   - CSI sequences (ESC [ ... final byte) are removed entirely.
//   - OSC sequences (ESC ] ... BEL or ESC \) are removed entirely — this
//     covers clipboard, title and hyperlink attacks.
//   - Other ESC-led two-byte sequences (ESC X) are removed.
//   - String/DCS/APC/PM introducers (ESC P, ESC X, ESC ^, ESC _) strip
//     through their ST terminator; unterminated ones swallow to end (the
//     safe direction for untrusted input).
//   - \n and \t are preserved (safe newline/tab behavior in multiline
//     views).
//
// The function never panics on malformed UTF-8: it operates on runes and
// invalid bytes are replaced with the Unicode replacement character by the
// range loop (Go semantics), which terminals render harmlessly.
func Sanitize(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	i := 0
	n := len(runes)
	for i < n {
		r := runes[i]
		switch r {
		case '\n', '\t':
			out = append(out, r)
			i++
		case '\r':
			i++ // drop CR entirely (carriage-return rewriting defense)
		case 0x1B: // ESC — sequence dispatcher
			i = skipEscape(runes, i, n, out)
		default:
			if isC0Control(r) || isC1Control(r) {
				i++
				continue
			}
			out = append(out, r)
			i++
		}
	}
	return string(out)
}

// skipEscape removes the escape sequence starting at runes[i] (== ESC) and
// returns the index of the first byte after the sequence. Removed bytes are
// not appended to out.
func skipEscape(runes []rune, i, n int, out []rune) int {
	// i points at ESC
	if i+1 >= n {
		return n // dangling ESC at end — drop
	}
	switch runes[i+1] {
	case '[': // CSI: ESC [ params intermediates final (0x40–0x7E)
		j := i + 2
		for j < n && !isCSIFinal(runes[j]) {
			j++
		}
		if j < n {
			return j + 1
		}
		return n // unterminated CSI: swallow rest
	case ']': // OSC: ESC ] ... BEL or ESC \
		j := i + 2
		for j < n {
			if runes[j] == 0x07 { // BEL terminator
				return j + 1
			}
			if runes[j] == 0x1B && j+1 < n && runes[j+1] == '\\' { // ST
				return j + 2
			}
			j++
		}
		return n // unterminated OSC: swallow rest (safe direction)
	case 'P', 'X', '^', '_': // DCS / SOS / PM / APC ... ST
		j := i + 2
		for j < n {
			if runes[j] == 0x1B && j+1 < n && runes[j+1] == '\\' {
				return j + 2
			}
			// DCS also accepts BEL per some implementations; accept both.
			if runes[j] == 0x07 {
				return j + 1
			}
			j++
		}
		return n
	default:
		// Two-byte ESC sequence (e.g. ESC ( B charset) — drop ESC + one
		// byte for intermediates, then final byte if in 0x30–0x7E.
		j := i + 1
		for j < n && isEscapeIntermediate(runes[j]) {
			j++
		}
		if j < n && isEscapeFinal(runes[j]) {
			return j + 1
		}
		return j // ESC alone or ESC + non-final: drop what was scanned
	}
}

func isC0Control(r rune) bool { return r >= 0x00 && r <= 0x1F }

func isC1Control(r rune) bool { return r >= 0x80 && r <= 0x9F }

func isCSIFinal(r rune) bool { return r >= 0x40 && r <= 0x7E }

func isEscapeIntermediate(r rune) bool { return r >= 0x20 && r <= 0x2F }

func isEscapeFinal(r rune) bool { return r >= 0x30 && r <= 0x7E }

// RedactTokens masks token-shaped secrets in text that might embed them
// (error text surfaces, debug output). It is a defense-in-depth layer on
// top of the discipline that errors never include credentials.
// Patterns:
//   - "Authorization: Bearer <token>" and bare "Bearer <token>" forms
//   - long hex/base64url blobs (>=24 chars) commonly produced by tokens
//
// Replacement preserves type/length hints, not values.
func RedactTokens(s string) string {
	if s == "" {
		return s
	}
	s = replaceBearer(s)
	s = replaceLongSecretRuns(s)
	return s
}

// replaceBearer masks the token after "Bearer" markers, preserving the
// marker itself.
func replaceBearer(s string) string {
	const marker = "Bearer"
	out := s
	var result stringsBuilder
	pos := 0
	for {
		idx := indexOfFold(out[pos:], marker)
		if idx < 0 {
			result.WriteString(out[pos:])
			return result.String()
		}
		abs := pos + idx
		end := abs + len(marker)
		// consume spaces after marker
		j := end
		for j < len(out) && (out[j] == ' ' || out[j] == '	') {
			j++
		}
		// consume token run
		k := j
		for k < len(out) && isTokenByte(out[k]) {
			k++
		}
		if k == j {
			// no token after marker; keep marker text (scrambled to avoid
			// re-matching loops) and continue after it
			result.WriteString(out[pos:end])
			result.WriteString(" ")
			pos = end
			continue
		}
		result.WriteString(out[pos:end])
		result.WriteString(" [REDACTED]")
		pos = k
	}
}

func isTokenByte(b byte) bool {
	return b > 0x20 && b < 0x7F && b != '"' && b != '\'' && b != ')' && b != ',' && b != ';'
}

func indexOfFold(s, sub string) int {
	n := len(sub)
	if n == 0 || len(s) < n {
		return -1
	}
	for i := 0; i+n <= len(s); i++ {
		match := true
		for j := 0; j < n; j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 32
			}
			if 'A' <= b && b <= 'Z' {
				b += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// replaceLongSecretRuns masks long runs of high-entropy-looking bytes.
func replaceLongSecretRuns(s string) string {
	var b stringsBuilder
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		if isSecretRunByte(c) {
			j := i
			for j < n && isSecretRunByte(s[j]) {
				j++
			}
			if j-i >= 24 {
				b.WriteString("[REDACTED]")
			} else {
				b.WriteString(s[i:j])
			}
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func isSecretRunByte(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') ||
		c == '-' || c == '_' || c == '+' || c == '='
}

type stringsBuilder struct{ b []byte }

func (sb *stringsBuilder) WriteString(s string) { sb.b = append(sb.b, s...) }

// WriteByte satisfies the io.ByteWriter convention (vet-checked); the
// error is always nil because the backing append cannot fail.
func (sb *stringsBuilder) WriteByte(c byte) error {
	sb.b = append(sb.b, c)
	return nil
}
func (sb *stringsBuilder) String() string { return string(sb.b) }
