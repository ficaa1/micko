package diagnose_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/diagnose"
)

// picked renders picked lines as "N>text" for a match and "N text" for
// context.
func picked(ls []diagnose.LogLine) string {
	var out []string
	for _, l := range ls {
		mark := " "
		if l.Match {
			mark = ">"
		}
		out = append(out, fmt.Sprint(l.N)+mark+l.Text)
	}
	return strings.Join(out, "\n")
}

// A Python traceback is kept whole, frames and exception, with the line
// before it for context; timestamps do not hide the frames' indentation.
func TestPickLogKeepsATraceback(t *testing.T) {
	lines := []string{
		"2026-09-08T10:00:00Z loading",
		"2026-09-08T10:00:01Z applying transforms",
		"2026-09-08T10:00:02Z Traceback (most recent call last):",
		"2026-09-08T10:00:02Z   File \"/app/t.py\", line 88, in <module>",
		"2026-09-08T10:00:02Z     run()",
		"2026-09-08T10:00:02Z ValueError: bad row",
		"2026-09-08T10:00:03Z shutting down",
	}
	want := strings.Join([]string{
		"2 2026-09-08T10:00:01Z applying transforms",
		"3>2026-09-08T10:00:02Z Traceback (most recent call last):",
		"4>2026-09-08T10:00:02Z   File \"/app/t.py\", line 88, in <module>",
		"5>2026-09-08T10:00:02Z     run()",
		"6>2026-09-08T10:00:02Z ValueError: bad row",
	}, "\n")
	if got := picked(diagnose.PickLog(lines, 8)); got != want {
		t.Fatalf("picked:\n%s\nwant:\n%s", got, want)
	}
}

// Error words match whole words in any case, with the line before each
// match as context.
func TestPickLogMatchesErrorWords(t *testing.T) {
	lines := []string{
		"connecting",
		"FATAL: password authentication failed for user app",
		"terror is not an error word inside another word? it is here: error",
		"errorless",
		"partition 8 written",
		"Killed",
		"panic: runtime error: index out of range",
		"goroutine 1 [running]:",
	}
	got := picked(diagnose.PickLog(lines, 8))
	want := strings.Join([]string{
		"1 connecting",
		"2>FATAL: password authentication failed for user app",
		"3>terror is not an error word inside another word? it is here: error",
		"5 partition 8 written",
		"6>Killed",
		"7>panic: runtime error: index out of range",
	}, "\n")
	if got != want {
		t.Fatalf("picked:\n%s\nwant:\n%s", got, want)
	}
}

// With more matches than fit, the ones nearest the end win, and the limit
// counts the context lines too.
func TestPickLogPrefersTheEnd(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		if i%10 == 0 {
			lines = append(lines, fmt.Sprintf("ERROR batch %d failed", i))
		} else {
			lines = append(lines, fmt.Sprintf("batch %d ok", i))
		}
	}
	got := diagnose.PickLog(lines, 8)
	if len(got) != 8 {
		t.Fatalf("picked %d lines, want 8", len(got))
	}
	if got[0].N != 169 || got[len(got)-1].N != 200 {
		t.Fatalf("picked lines %d..%d, want the last four matches from 169 to 200:\n%s", got[0].N, got[len(got)-1].N, picked(got))
	}
}

// A tail without an error word gives its last three lines, and an empty
// tail or a zero limit gives nothing.
func TestPickLogWithoutErrors(t *testing.T) {
	got := picked(diagnose.PickLog([]string{"a", "b", "c", "d", "e"}, 8))
	if got != "3 c\n4 d\n5 e" {
		t.Fatalf("picked:\n%s", got)
	}
	if diagnose.PickLog(nil, 8) != nil || diagnose.PickLog([]string{"error"}, 0) != nil {
		t.Fatal("an empty tail or a zero limit picked lines")
	}
}

// A very long line is cut, so a card never holds a megabyte of one line.
func TestPickLogCutsLongLines(t *testing.T) {
	long := "error " + strings.Repeat("x", 5000)
	got := diagnose.PickLog([]string{long}, 8)
	if len(got) != 1 || len([]rune(got[0].Text)) != 400 || !strings.HasSuffix(got[0].Text, "…") {
		t.Fatalf("long line kept as %d runes", len([]rune(got[0].Text)))
	}
}
