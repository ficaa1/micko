package logs

import (
	"hash/fnv"
	"regexp"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// decorate.go — the styling a log line gets on its way to the screen: the
// source label in front of it, the level word, the search matches, and the
// cut into screen lines when wrapping is on.
//
// Every decoration is an overlay on the sanitized text. None of it is added
// to the retained line, so RawLines, the clipboard and the pipe still carry
// the line exactly as the server sent it, and the styling of one span never
// spills onto the rest of the line.

// labelWidth is the width of the source label column. It is fixed, so the
// log text starts in the same column on every line whatever pods have
// spoken so far; 16 cells hold the step and task names Argo workflows
// typically use ("train-shard(3:3)", "incremental-sync").
const labelWidth = 16

// labelGap separates the label column from the text.
const labelGap = 1

// sourceLabel is the short name of the pod a line came from: the node's
// display name when the pod maps to a node, otherwise the pod name without
// the workflow's own name in front (every pod of the workflow shares it),
// clipped to the label column. It is sanitized: node names come from the
// server.
func (m *Model) sourceLabel(pod string) string {
	if pod == "" {
		return ""
	}
	name, ok := m.sources[pod]
	if !ok || name == "" {
		name = strings.TrimPrefix(pod, m.ref.Name+"-")
	}
	return clipCells(shared.Sanitize(name), labelWidth)
}

// labelPalette is the set of styles source labels are coloured from: the
// theme's phase and accent colours, minus the failure red, which would make
// an ordinary pod's label read as an error. The label text is always shown,
// so the colour only helps the eye group lines; it never carries meaning
// alone.
func (m *Model) labelPalette() []lipgloss.Style {
	t := m.theme
	return []lipgloss.Style{t.PhaseRunning, t.PhaseSucceeded, t.PhaseOther, t.Warning, t.Marked, t.Title}
}

// labelStyle picks the label's colour from a hash of the pod name, so a
// pod keeps its colour for the whole session, across reopened streams and
// whichever pods speak first.
func (m *Model) labelStyle(pod string) lipgloss.Style {
	palette := m.labelPalette()
	h := fnv.New32a()
	_, _ = h.Write([]byte(pod))
	return palette[h.Sum32()%uint32(len(palette))]
}

// labelsShown reports whether rows carry the source label column.
func (m *Model) labelsShown() bool { return m.labels }

// labelCell renders the label column for a row from pod: the coloured label
// padded to the column, or blank padding for a line with no pod.
func (m *Model) labelCell(pod string) string {
	label := m.sourceLabel(pod)
	pad := strings.Repeat(" ", labelWidth-ansi.StringWidth(label)+labelGap)
	if label == "" {
		return pad
	}
	return m.labelStyle(pod).Render(label) + pad
}

// clipCells clips s to width cells, with an ellipsis when cut.
func clipCells(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// level is the severity a line announces.
type level int

const (
	levelNone level = iota
	levelError
	levelWarn
	levelDebug
)

// levelOf classifies a severity word, case-insensitively.
func levelOf(word string) level {
	switch strings.ToUpper(word) {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return levelError
	case "WARN", "WARNING":
		return levelWarn
	case "DEBUG", "TRACE":
		return levelDebug
	}
	return levelNone
}

// levelStyle is the style for a level word.
func (m *Model) levelStyle(l level) lipgloss.Style {
	switch l {
	case levelError:
		return m.theme.ErrorText
	case levelWarn:
		return m.theme.Warning
	default:
		return m.theme.Dim
	}
}

// timestampRe matches the leading timestamp forms a log line commonly
// carries: an RFC 3339 instant (the server's, with timestamps on, or the
// program's own), a date, or a time of day, optionally in brackets.
var timestampRe = regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}([.,]\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?|\d{2}:\d{2}:\d{2}([.,]\d+)?(Z|[+-]\d{2}:?\d{2})?)\]?$`)

// jsonLevelRe finds the value of a "level" or "severity" field in a JSON
// line. The line is not decoded: only the value's position is needed, and
// a regular expression finds it without allocating the whole object.
var jsonLevelRe = regexp.MustCompile(`(?i)"(?:level|severity)"\s*:\s*"([A-Za-z]+)"`)

// maxLeadingStamps is how many timestamps may precede the level word. Two
// covers the common case of the server's timestamp in front of the
// program's own.
const maxLeadingStamps = 2

// levelSpan finds the level word of a line and returns its byte range, or
// ok=false. Only the first word-ish token after up to two timestamps
// counts, so "found 3 errors" and "errors: 0" are not error lines. A word
// in capitals counts on its own; a word in lower or mixed case counts only
// when it is decorated as a level ("[error]", "error:", "<warn>"), because
// "trace id=…" or "Error rate is fine" are ordinary sentences. A JSON line
// counts when its "level" or "severity" field names a level.
func levelSpan(text string) (start, end int, l level, ok bool) {
	i := 0
	for n := 0; n <= maxLeadingStamps; n++ {
		// Skip spaces to the next token.
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i >= len(text) {
			return 0, 0, levelNone, false
		}
		if text[i] == '{' {
			mm := jsonLevelRe.FindStringSubmatchIndex(text[i:])
			if mm == nil {
				return 0, 0, levelNone, false
			}
			s, e := i+mm[2], i+mm[3]
			if l := levelOf(text[s:e]); l != levelNone {
				return s, e, l, true
			}
			return 0, 0, levelNone, false
		}
		j := strings.IndexByte(text[i:], ' ')
		if j < 0 {
			j = len(text) - i
		}
		tok := text[i : i+j]
		if n < maxLeadingStamps && timestampRe.MatchString(tok) {
			i += j
			continue
		}
		word, off := trimDecoration(tok)
		l := levelOf(word)
		if l == levelNone {
			return 0, 0, levelNone, false
		}
		if word != strings.ToUpper(word) && len(word) == len(tok) {
			return 0, 0, levelNone, false // bare lowercase word
		}
		return i + off, i + off + len(word), l, true
	}
	return 0, 0, levelNone, false
}

// trimDecoration strips the punctuation a level word is commonly wrapped in
// ("[ERROR]", "ERROR:", "<warn>", "(debug)") and returns the word and its
// offset inside tok.
func trimDecoration(tok string) (string, int) {
	const deco = "[]()<>:|"
	start := 0
	for start < len(tok) && strings.IndexByte(deco, tok[start]) >= 0 {
		start++
	}
	end := len(tok)
	for end > start && strings.IndexByte(deco, tok[end-1]) >= 0 {
		end--
	}
	return tok[start:end], start
}

// span is one styled byte range of a line.
type span struct {
	start, end int
	style      lipgloss.Style
}

// matchSpans returns the byte ranges of every occurrence of the search term
// in text. Case-insensitive matching goes through a regular expression over
// the original text, so the ranges are right even where lowercasing a rune
// changes its length.
func (m *Model) matchSpans(text string, style lipgloss.Style) []span {
	re := m.searchRegexp()
	if re == nil || text == "" {
		return nil
	}
	var out []span
	for _, loc := range re.FindAllStringIndex(text, -1) {
		if loc[1] > loc[0] {
			out = append(out, span{loc[0], loc[1], style})
		}
	}
	return out
}

// searchRegexp compiles the committed search term for highlighting, once
// per term.
func (m *Model) searchRegexp() *regexp.Regexp {
	term := m.search.term
	if term == "" {
		return nil
	}
	if m.searchRe != nil && m.searchReFor == term {
		return m.searchRe
	}
	pat := regexp.QuoteMeta(term)
	if !m.search.caseSensitive {
		pat = "(?i)" + pat
	}
	m.searchRe, m.searchReFor = regexp.MustCompile(pat), term
	return m.searchRe
}

// lineSpans is every styled range of one log line: its level word, then the
// search matches. A match wins where the two overlap, because it is what
// the reader asked to see.
func (m *Model) lineSpans(text string, current bool) []span {
	hitStyle := m.theme.Warning
	if current {
		hitStyle = m.theme.Selected
	}
	hits := m.matchSpans(text, hitStyle)
	s, e, l, ok := levelSpan(text)
	if !ok {
		return hits
	}
	lv := []span{{s, e, m.levelStyle(l)}}
	for _, h := range hits {
		var next []span
		for _, p := range lv {
			if h.end <= p.start || h.start >= p.end {
				next = append(next, p)
				continue
			}
			if p.start < h.start {
				next = append(next, span{p.start, h.start, p.style})
			}
			if h.end < p.end {
				next = append(next, span{h.end, p.end, p.style})
			}
		}
		lv = next
	}
	out := append(lv, hits...)
	// Insertion sort: a line has one level span and a handful of hits.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].start < out[j-1].start; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// renderRange renders text[from:to] with the spans that fall inside it.
// Each span is rendered on its own, so a style never continues past its
// range or across a wrapped line's break.
func renderRange(text string, from, to int, spans []span) string {
	var b strings.Builder
	at := from
	for _, s := range spans {
		if s.end <= from || s.start >= to {
			continue
		}
		start, end := max(s.start, from), min(s.end, to)
		if start < at {
			start = at
		}
		if start >= end {
			continue
		}
		b.WriteString(text[at:start])
		b.WriteString(s.style.Render(text[start:end]))
		at = end
	}
	b.WriteString(text[at:to])
	return b.String()
}

// wrapBreaks cuts text into screen lines of at most width cells and
// returns the byte offset each line starts at. It cuts between runes, never
// inside one, and a rune wider than the line still gets a line of its own.
func wrapBreaks(text string, width int) []int {
	starts := []int{0}
	if width < 1 {
		return starts
	}
	cells := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		w := ansi.StringWidth(string(r))
		if cells+w > width && cells > 0 {
			starts = append(starts, i)
			cells = 0
		}
		cells += w
		i += size
	}
	return starts
}
