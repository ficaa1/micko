package diagnose

import (
	"regexp"
	"strings"
)

// logs.go picks the lines of a log tail that explain a failure.

// LogTail is how many lines at the end of a failing pod's log are read for
// evidence. The cause of a failure is almost always printed last, and a
// bounded read keeps a chatty pod from filling memory.
const LogTail = 200

// maxLogLines is how many picked lines a card shows, context included.
const maxLogLines = 8

// maxLogLineRunes bounds one shown line; a line past it is cut with "…".
const maxLogLineRunes = 400

// LogLine is one line picked from a log tail.
type LogLine struct {
	// N is the line's position in the tail, from 1.
	N    int
	Text string
	// Match marks a line picked for what it says. The others are context:
	// the line before a match, which often names what was being done.
	Match bool
}

// errorWords are the words that mark a line worth showing. They are whole
// words, so "terror" or "errorless" do not match, and case does not matter.
var errorWords = regexp.MustCompile(`(?i)\b(error|errors|err|fatal|panic|panicked|exception|traceback|killed|oomkilled|out of memory|segmentation fault|sigsegv|sigkill|fail|failed|failure|denied|refused|timed out|no such file|not found|exit status|exit code|abort|aborted|critical)\b`)

// PickLog chooses at most limit lines of a log tail that explain a failure:
// lines with an error word, whole tracebacks, and one line of context
// before each. The matches nearest the end win when there are more than fit,
// since the cause of a failure is usually printed last. A tail with no such
// line gives its last three lines, so the card still shows how the log
// ends.
func PickLog(lines []string, limit int) []LogLine {
	if limit <= 0 || len(lines) == 0 {
		return nil
	}
	match := make([]bool, len(lines))
	found := false
	inTrace := false
	for i, l := range lines {
		body := stripTimestamp(l)
		switch {
		case strings.HasPrefix(body, "Traceback (most recent call last)"):
			inTrace = true
			match[i] = true
		case inTrace && (strings.HasPrefix(body, " ") || strings.HasPrefix(body, "\t")):
			match[i] = true
		case inTrace:
			// The first unindented line after the frames names the
			// exception, which is the line that matters most.
			inTrace = false
			match[i] = true
		default:
			match[i] = errorWords.MatchString(body)
		}
		found = found || match[i]
	}
	keep := make([]bool, len(lines))
	kept := 0
	take := func(i int) {
		if i >= 0 && !keep[i] && kept < limit {
			keep[i] = true
			kept++
		}
	}
	if !found {
		for i := max(len(lines)-3, 0); i < len(lines); i++ {
			take(i)
		}
	} else {
		for i := len(lines) - 1; i >= 0 && kept < limit; i-- {
			if !match[i] {
				continue
			}
			take(i)
			if i > 0 && !match[i-1] {
				take(i - 1)
			}
		}
	}
	out := make([]LogLine, 0, kept)
	for i, k := range keep {
		if k {
			out = append(out, LogLine{N: i + 1, Text: cutRunes(lines[i], maxLogLineRunes), Match: match[i]})
		}
	}
	return out
}

// anyMatch reports whether any picked line was picked for what it says.
func anyMatch(lines []LogLine) bool {
	for _, l := range lines {
		if l.Match {
			return true
		}
	}
	return false
}

var timestampPrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2}) `)

// stripTimestamp drops the RFC 3339 timestamp a log line may start with, so
// a traceback's indentation is seen after it.
func stripTimestamp(l string) string {
	return timestampPrefix.ReplaceAllString(l, "")
}

// cutRunes cuts s to n runes, marking the cut.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
