package diagnose

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Log evidence keeps error context and tracebacks, preferring bounded lines nearest the tail.
func TestExplainLogSelection(t *testing.T) {
	var chatty []string
	for i := 1; i <= 200; i++ {
		if i%10 == 0 {
			chatty = append(chatty, fmt.Sprintf("ERROR batch %d failed", i))
		} else {
			chatty = append(chatty, fmt.Sprintf("batch %d ok", i))
		}
	}
	cases := []struct {
		name  string
		lines []string
		want  []LogLine
		intro string
	}{
		{
			name: "timestamped traceback",
			lines: []string{
				"2026-09-08T10:00:00Z loading",
				"2026-09-08T10:00:01Z applying transforms",
				"2026-09-08T10:00:02Z Traceback (most recent call last):",
				"2026-09-08T10:00:02Z   File \"/app/t.py\", line 88, in <module>",
				"2026-09-08T10:00:02Z     run()",
				"2026-09-08T10:00:02Z ValueError: bad row",
				"2026-09-08T10:00:03Z shutting down",
			},
			want: []LogLine{
				{2, "2026-09-08T10:00:01Z applying transforms", false},
				{3, "2026-09-08T10:00:02Z Traceback (most recent call last):", true},
				{4, "2026-09-08T10:00:02Z   File \"/app/t.py\", line 88, in <module>", true},
				{5, "2026-09-08T10:00:02Z     run()", true},
				{6, "2026-09-08T10:00:02Z ValueError: bad row", true},
			},
			intro: "picked from the last 7 lines of job's log (container main)",
		},
		{
			name:  "whole words and predecessor context",
			lines: []string{"connecting", "FATAL: password authentication failed for user app", "terror", "errorless", "partition 8 written", "Killed", "panic: runtime error: index out of range", "goroutine 1 [running]:"},
			want:  []LogLine{{1, "connecting", false}, {2, "FATAL: password authentication failed for user app", true}, {5, "partition 8 written", false}, {6, "Killed", true}, {7, "panic: runtime error: index out of range", true}},
			intro: "picked from the last 8 lines of job's log (container main)",
		},
		{
			name: "last four errors within eight lines", lines: chatty,
			want: []LogLine{
				{169, "batch 169 ok", false}, {170, "ERROR batch 170 failed", true},
				{179, "batch 179 ok", false}, {180, "ERROR batch 180 failed", true},
				{189, "batch 189 ok", false}, {190, "ERROR batch 190 failed", true},
				{199, "batch 199 ok", false}, {200, "ERROR batch 200 failed", true},
			},
			intro: "picked from the last 200 lines of job's log (container main)",
		},
		{
			name: "no errors", lines: []string{"a", "b", "c", "d", "e"},
			want:  []LogLine{{3, "c", false}, {4, "d", false}, {5, "e", false}},
			intro: "no error in the last 5 lines of job's log (container main); it ends",
		},
		{
			name: "long unicode error", lines: []string{"error " + strings.Repeat("界", 5000)},
			want:  []LogLine{{1, "error " + strings.Repeat("界", 393) + "…", true}},
			intro: "picked from the last 1 line of job's log (container main)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newWF("Failed")
			b.task("job", "Failed", ran(0, 30), exit("1"), pod())
			in := b.input()
			in.Logs = []Log{{NodeID: "job", PodName: "wf-job", State: LogRead, Tail: 200, Lines: c.lines}}
			f := findRule(t, Explain(in), RuleRootFailure)
			var got *Evidence
			for i := range f.Evidence {
				if f.Evidence[i].Label == "log" {
					got = &f.Evidence[i]
				}
			}
			if got == nil {
				t.Fatal("root failure omitted log evidence")
			}
			if got.Text != c.intro || !reflect.DeepEqual(got.Log, c.want) {
				t.Fatalf("log evidence = %+v, want intro %q and lines %+v", got, c.intro, c.want)
			}
		})
	}
}
