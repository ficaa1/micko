package workflowlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Age and duration cells prefer start time, show missing data honestly and clamp negative elapsed time.
func TestCellTimes(t *testing.T) {
	now := testkit.FixtureEpoch
	start := now.Add(-4 * time.Minute)
	end := start.Add(time.Minute)
	future := now.Add(time.Minute)
	zero := time.Time{}
	for _, c := range []struct {
		name          string
		created       time.Time
		start, end    *time.Time
		clock         time.Time
		age, duration string
	}{
		{"started preferred", now.Add(-time.Hour), &start, &end, now, "4m", "1m"},
		{"created fallback", now.Add(-time.Hour), nil, nil, now, "1h", "-"},
		{"ongoing", zero, &start, nil, now, "4m", "ongoing"},
		{"missing", zero, nil, nil, now, "-", "-"},
		{"zero pointers", zero, &zero, &zero, now, "-", "-"},
		{"future clamps", zero, &future, &start, now, "0s", "0s"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newList(t)
			r := summary("row", "Running")
			r.CreatedAt = c.created
			r.StartedAt = c.start
			r.FinishedAt = c.end
			m.SetItems([]core.Summary{r}, now)
			lines := m.BodyLines(c.clock)
			fields := strings.Fields(lines[2])
			if fields[3] != c.age || fields[4] != c.duration {
				t.Fatalf("cells %v want age %s duration %s", fields, c.age, c.duration)
			}
		})
	}
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{2*time.Hour + 13*time.Minute, "2h13m"},
		{2 * time.Hour, "2h"},
		{3*24*time.Hour + 4*time.Hour, "3d4h"},
		{3 * 24 * time.Hour, "3d"},
	} {
		t.Run(c.want, func(t *testing.T) {
			m := newList(t)
			r := summary("row", "Succeeded")
			start := now.Add(-c.d)
			r.StartedAt = &start
			r.FinishedAt = &now
			m.SetItems([]core.Summary{r}, now)
			fields := strings.Fields(m.BodyLines(now)[2])
			if fields[3] != c.want || fields[4] != c.want {
				t.Fatalf("duration %v cells %v", c.d, fields)
			}
		})
	}
}

// Progress cells show exact counts and incomplete bars, leaving malformed counts readable.
func TestProgressRendering(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "-"},
		{"0/4", "0/4 ░░░░░░"},
		{"3/7", "3/7 ██░░░░"},
		{"6/7", "6/7 █████░"},
		{"7/7", "7/7 ██████"},
		{"0/0", "0/0 ░░░░░░"},
		{"120/1500", "120/1500 ░░░"},
		{"9/3", "9/3"},
		{"lots", "lots"},
		{"123456/1234567", "123456/1234…"},
	} {
		t.Run(c.in, func(t *testing.T) {
			m := newList(t)
			m.SetSize(80, 20)
			m.Update(runeKey('w'))
			r := summary("row", "Running")
			r.Progress = c.in
			m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
			lines := m.BodyLines(testkit.FixtureEpoch)
			line := lines[2]
			got := renderedColumn(t, lines[1], line, "PROGRESS")
			if got != c.want {
				t.Fatalf("progress %q want %q row %q", got, c.want, line)
			}
		})
	}
}

// Wide cells show local timestamps and preferred origins while omitting labels already shown elsewhere.
func TestWideMetadata(t *testing.T) {
	m := newList(t)
	m.SetSize(220, 20)
	m.Update(runeKey('w'))
	setTimeZone(t, time.FixedZone("offset", 2*60*60))
	start := testkit.FixtureEpoch.Add(-26 * time.Minute)
	finish := testkit.FixtureEpoch.Add(-22 * time.Minute)
	r := summary("row", "Succeeded")
	r.StartedAt = &start
	r.FinishedAt = &finish
	r.Labels = map[string]string{clusterWorkflowTemplateLabel: "cluster", cronLabel: "hourly", phaseLabel: "Succeeded", completedLabel: "true", "team": "platform", "env": "prod"}
	m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
	got := body(&m)
	for _, want := range []string{"09-08 13:34  09-08 13:38", "cluster", "hourly", "env=prod,team=platform"} {
		if !strings.Contains(got, want) {
			t.Fatalf("metadata %q missing:\n%s", want, got)
		}
	}
	for _, key := range []string{workflowTemplateLabel, clusterWorkflowTemplateLabel, cronLabel, phaseLabel, completedLabel} {
		r.Labels = map[string]string{key: "value"}
		m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
		if strings.Contains(body(&m), "workflows.") {
			t.Fatalf("LABELS repeated %s", key)
		}
	}
	r.Labels = map[string]string{workflowTemplateLabel: "local", clusterWorkflowTemplateLabel: "cluster"}
	m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
	if !strings.Contains(body(&m), "local") || strings.Contains(body(&m), "cluster") {
		t.Fatalf("template body = %q, want local and no cluster origin", body(&m))
	}
}

// Server text remains readable without passing terminal controls through plain or styled rows.
func TestBodySanitizesServerText(t *testing.T) {
	for _, colored := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "color"}[colored], func(t *testing.T) {
			m := newList(t)
			if colored {
				t.Setenv("NO_COLOR", "")
				m.SetTheme(shared.NewTheme(false))
			}
			m.SetSize(240, 20)
			m.SetAllNamespaces(true)
			m.Update(runeKey('w'))
			r := summary("ok\x1b]0;pwned\x07name", "Mystery\x07")
			r.Ref.Namespace = "safe\x1b[31mns"
			r.Message = "hello\x1b[31mred\x07"
			r.Progress = "lots\x07"
			r.Labels = map[string]string{workflowTemplateLabel: "t\x07mpl", cronLabel: "c\x07ron", "env": "p\x07rod"}
			m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
			got := body(&m)
			plain := ansi.Strip(got)
			for _, want := range []string{"okname", "safens", "hellored", "tmpl", "cron", "env=prod", "lots", "Mystery"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("sanitized %q missing:\n%s", want, plain)
				}
			}
			if strings.ContainsRune(got, '\x07') || strings.ContainsAny(plain, "\x1b\x07") || strings.Contains(got, "\x1b]0;pwned") || strings.Contains(got, "hello\x1b[31mred") || strings.Contains(got, "safe\x1b[31mns") {
				t.Fatalf("server control bytes leaked: %q", got)
			}
			m.SetItems(nil, testkit.FixtureEpoch)
			m.SetStatus(StatusForbidden, "denied \x1b]8;;http://evil\x1b\\link\x07back", 0)
			plain = ansi.Strip(body(&m))
			if !strings.Contains(plain, "denied linkback") || strings.ContainsAny(plain, "\x1b\x07") {
				t.Fatalf("status sanitize %q", plain)
			}
		})
	}
}

// Wide names stay intact or clip by terminal cells without shifting the phase column.
func TestUnicodeAlignment(t *testing.T) {
	m := newList(t)
	r := summary("日本語ワークフロー-表达式", "Running")
	a := summary("ascii", "Running")
	m.SetItems([]core.Summary{r, a}, testkit.FixtureEpoch)
	for _, c := range []struct {
		w           int
		name        string
		phaseOffset int
	}{
		{100, r.Ref.Name, 50},
		{80, r.Ref.Name, 34},
		{60, "日本語ワークフロー-…", 26},
	} {
		m.SetSize(c.w, 20)
		lines := m.BodyLines(testkit.FixtureEpoch)
		if len(lines) != 4 || !strings.Contains(strings.Join(lines, "\n"), c.name) {
			t.Fatalf("name missing width %d: %v", c.w, lines)
		}
		for _, line := range lines[2:] {
			i := strings.Index(line, "Running")
			if i < 0 || ansi.StringWidth(line[:i]) != c.phaseOffset || ansi.StringWidth(line) > c.w {
				t.Fatalf("alignment width %d: %q", c.w, line)
			}
		}
	}
}

// A malformed query uses the error style in the focused search line.
func TestQueryErrorStyle(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m := New(shared.NewTheme(false))
	m.SetSize(160, 10)
	m.SetItems([]core.Summary{summary("a", "Running")}, testkit.FixtureEpoch)
	editQuery(&m, "age<")
	want := m.theme.ErrorText.Render("✗ age< needs a value")
	if !strings.Contains(want, "\x1b[") || !strings.Contains(body(&m), want) {
		t.Fatalf("query error style missing: %q", body(&m))
	}
}

// Every phase carries a symbol and word, including suspended, unknown and missing phases.
func TestPhaseDisplay(t *testing.T) {
	for _, c := range []struct {
		phase     string
		suspended bool
		want      string
	}{
		{"Running", false, "● Running"},
		{"Running", true, "◐ Suspended"},
		{"Pending", false, "○ Pending"},
		{"Succeeded", false, "✓ Succeeded"},
		{"Failed", false, "✗ Failed"},
		{"Error", false, "✗ Error"},
		{"Mystery", false, "• Mystery"},
		{"", false, "• (no phase)"},
	} {
		m := newList(t)
		r := summary("row", c.phase)
		r.Suspended = c.suspended
		m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
		if !strings.Contains(m.BodyLines(testkit.FixtureEpoch)[2], c.want) {
			t.Fatalf("phase %q suspended=%v: %s", c.phase, c.suspended, body(&m))
		}
	}
}

// renderedColumn reads a row's cell using the rendered column headings.
func renderedColumn(t *testing.T, header, row, column string) string {
	t.Helper()
	start := strings.Index(header, column)
	if start < 0 {
		t.Fatalf("column %q absent from header %q", column, header)
	}
	left := ansi.StringWidth(header[:start])
	right := ansi.StringWidth(row)
	rest := header[start+len(column):]
	if next := strings.Fields(rest); len(next) > 0 {
		offset := start + len(column) + strings.Index(rest, next[0])
		right = ansi.StringWidth(header[:offset])
	}
	return strings.TrimSpace(ansi.Cut(row, left, right))
}
