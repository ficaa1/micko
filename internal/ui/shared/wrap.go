package shared

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Wrap word-wraps s to a maximum width of width visible terminal columns,
// breaking at word boundaries. It is the anti-clipping choke point for long
// untrusted-derived text (error messages, server reasons) that would
// otherwise run past the right edge of the terminal and get cut off mid-word
// (live-smoke defect 5).
//
// Rules:
//   - width <= 0 means "no known width" (the value passed by the test harness
//     and golden renders) — the string is returned unchanged so unsized
//     renderings are byte-identical.
//   - Embedded "\n" are preserved and each logical paragraph is wrapped
//     independently.
//   - Words wider than width are NOT hard-split (a single unbreakable token
//     stays on its own line); this keeps content intact and is the safe
//     direction — a whole over-wide token still clips, but ordinary prose and
//     server error sentences wrap correctly.
//   - Cell width is measured with ansi.StringWidth so wide (CJK/emoji) runes
//     count as their true terminal cells.
func Wrap(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	paras := strings.Split(s, "\n")
	lines := make([]string, 0, len(paras))
	for _, para := range paras {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		var cur strings.Builder
		curW := 0
		for _, word := range strings.Fields(para) {
			w := ansi.StringWidth(word)
			if curW > 0 && curW+1+w > width {
				lines = append(lines, cur.String())
				cur.Reset()
				curW = 0
			}
			if curW > 0 {
				cur.WriteByte(' ')
				curW++
			}
			cur.WriteString(word)
			curW += w
		}
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}
