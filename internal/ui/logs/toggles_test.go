package logs

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
)

// wrapModel is a 40-column pane without labels holding n lines, every
// third of which is long enough to wrap to three screen lines.
func wrapModel(t *testing.T, n int) *Model {
	t.Helper()
	m := testModel(t)
	m.labels = false
	m.SetSize(40, 16)
	var rs []core.LogRecord
	for i := 0; i < n; i++ {
		text := "line-" + itoa(i)
		if i%3 == 0 {
			text += " " + strings.Repeat("w", 80)
		}
		rs = append(rs, podRec("p", text))
	}
	m.ApplyRecords(rs)
	return m
}

// bottomText is the logical line at the bottom of the window.
func bottomText(m *Model) string {
	w := m.window()
	return w[len(w)-1].text
}

// Wrapping fills the pane with screen lines, never more, and every screen
// line fits the pane width.
func TestWrapFillsThePaneExactly(t *testing.T) {
	m := wrapModel(t, 60)
	press(m, 'w')
	lines := m.windowLines()
	if len(lines) != m.viewRows() {
		t.Fatalf("wrapped window is %d lines, the pane holds %d", len(lines), m.viewRows())
	}
	for _, l := range lines {
		if len([]rune(l)) > 40 {
			t.Fatalf("a wrapped line overflows: %q", l)
		}
	}
	// Following, the newest line's last screen line is the pane's last.
	if !strings.HasPrefix(bottomText(m), "line-59") {
		t.Fatalf("following did not keep the tail at the bottom: %q", bottomText(m))
	}
}

// The scroll anchor is the logical line. Turning wrapping on and off while
// paused keeps the same line at the bottom of the pane, and j/k move one
// logical line in either mode.
func TestWrapKeepsTheLogicalLineAtTheBottom(t *testing.T) {
	m := wrapModel(t, 60)
	for i := 0; i < 20; i++ {
		press(m, 'k')
	}
	anchor := bottomText(m)
	press(m, 'w')
	if got := bottomText(m); got != anchor {
		t.Fatalf("wrap on moved the bottom line: %q, was %q", got, anchor)
	}
	press(m, 'k')
	moved := bottomText(m)
	if moved == anchor {
		t.Fatal("k did not move while wrapped")
	}
	press(m, 'j')
	if got := bottomText(m); got != anchor {
		t.Fatalf("j did not come back one logical line: %q", got)
	}
	press(m, 'w')
	if got := bottomText(m); got != anchor {
		t.Fatalf("wrap off moved the bottom line: %q, was %q", got, anchor)
	}
}

// At the top of a wrapped log the first line starts the pane, and scrolling
// up past it does not shrink the window.
func TestWrapTopFloor(t *testing.T) {
	m := wrapModel(t, 60)
	press(m, 'w')
	for i := 0; i < 100; i++ {
		press(m, 'k')
	}
	lines := m.windowLines()
	if !strings.HasPrefix(lines[0], "── (all pods)") || len(lines) != m.viewRows() {
		t.Fatalf("top of the wrapped log:\n%s", strings.Join(lines, "\n"))
	}
}

// A page in wrap mode moves by the rows on screen, not by screen lines, so
// pgup and pgdn neither skip a row nor stall: the row at the edge the page
// moves away from is still on screen after the move. Rows here are one and
// three screen lines tall, so every pane height lands a page differently.
func TestWrapPagesByVisibleRows(t *testing.T) {
	for h := 12; h <= 20; h++ {
		m := wrapModel(t, 60)
		m.SetSize(40, h)
		press(m, 'w')
		for i := 0; i < 30; i++ {
			press(m, 'k')
		}
		for _, key := range []rune{tea.KeyPgUp, tea.KeyPgDown} {
			before := m.window()
			pressKey(m, key)
			after := m.window()
			edge, moved := before[0].logIdx, after[0].logIdx < before[0].logIdx
			if key == tea.KeyPgDown {
				edge, moved = before[len(before)-1].logIdx, after[len(after)-1].logIdx > edge
			}
			shown := false
			for _, r := range after {
				shown = shown || r.logIdx == edge
			}
			if !shown || !moved {
				t.Fatalf("height %d, key %d: line %d shown=%v, moved=%v", h, key, edge, shown, moved)
			}
		}
	}
}

// & shows only the lines matching the search, says how many of the
// retained lines that is, and & again or esc shows everything. The buffer
// underneath is untouched.
func TestFilterShowsOnlyMatchingLines(t *testing.T) {
	m := testModel(t)
	var rs []core.LogRecord
	for i := 0; i < 40; i++ {
		text := "ok " + itoa(i)
		if i%10 == 3 {
			text = "ERROR " + itoa(i)
		}
		rs = append(rs, podRec("p", text))
	}
	m.ApplyRecords(rs)
	press(m, '&')
	if m.filterOn || !strings.Contains(body(m), "search first") {
		t.Fatalf("& without a search filtered, or did not say why:\n%s", body(m))
	}
	press(m, '/')
	typeInto(m, "error")
	pressKey(m, keyEnter)
	press(m, '&')
	if !m.filterOn {
		t.Fatal("& did not filter")
	}
	v := body(m)
	if !strings.Contains(v, `& "error": showing 4 of 40`) {
		t.Fatalf("status does not count the filter:\n%s", v)
	}
	for _, r := range m.window() {
		if !strings.Contains(r.text, "ERROR") {
			t.Fatalf("a non-matching row is shown: %q", r.text)
		}
	}
	if !m.EscapeClears() {
		t.Fatal("esc does not clear the filter first")
	}
	pressKey(m, keyEscape)
	if m.filterOn {
		t.Fatal("esc did not clear the filter")
	}
	press(m, '&')
	press(m, '&')
	if m.filterOn {
		t.Fatal("a second & did not clear the filter")
	}
	if lines := len(m.buf.Lines()); lines != 40 {
		t.Fatalf("the filter changed the buffer: %d lines", lines)
	}
	// Clearing the search leaves nothing to filter by.
	press(m, '&')
	press(m, '/')
	for range "error" {
		pressKey(m, keyBackspace)
	}
	pressKey(m, keyEnter)
	if m.filterOn {
		t.Fatal("the filter outlived its search")
	}
}

// The filter counts against what the buffer retains. Past the line cap the
// oldest lines are evicted as always, and "of" is the retained count.
func TestFilterWithTheRetentionCap(t *testing.T) {
	m := testModel(t)
	total := MaxLines + 500
	var rs []core.LogRecord
	for i := 0; i < total; i++ {
		text := "ok " + itoa(i)
		if i%100 == 0 {
			text = "match " + itoa(i)
		}
		rs = append(rs, podRec("p", text))
	}
	m.ApplyRecords(rs)
	press(m, '/')
	typeInto(m, "match")
	pressKey(m, keyEnter)
	press(m, '&')
	retained := m.buf.Lines()
	want := 0
	for _, l := range retained {
		if strings.HasPrefix(l.Content, "match") {
			want++
		}
	}
	if len(retained) >= total || want == 0 {
		t.Fatalf("the cap did not evict: %d retained of %d, %d matching", len(retained), total, want)
	}
	status := m.statusView()
	if !strings.Contains(status, "showing "+itoa(want)+" of "+itoa(len(retained))) {
		t.Fatalf("status %q, want showing %d of %d", status, want, len(retained))
	}
	// New lines keep arriving and keep being filtered; the cap evicts two
	// old lines to make room, so the count is taken again.
	m.ApplyRecords([]core.LogRecord{podRec("p", "match late"), podRec("p", "ok late")})
	want = 0
	for _, l := range m.buf.Lines() {
		if strings.HasPrefix(l.Content, "match") {
			want++
		}
	}
	if !strings.Contains(m.statusView(), "showing "+itoa(want)+" of "+itoa(len(m.buf.Lines()))) {
		t.Fatalf("status %q after late lines, want showing %d", m.statusView(), want)
	}
	if last := m.window()[len(m.window())-1]; last.text != "match late" && m.follow {
		t.Fatalf("a following filtered pane does not end on the late match: %q", last.text)
	}
}

// Turning the filter on while paused keeps the reader on their line, and
// turning it off returns them to it.
func TestFilterKeepsThePausedLine(t *testing.T) {
	m := testModel(t)
	var rs []core.LogRecord
	for i := 0; i < 1000; i++ {
		text := "ok " + itoa(i)
		if i%5 == 0 {
			text = "tick " + itoa(i)
		}
		rs = append(rs, podRec("p", text))
	}
	m.ApplyRecords(rs)
	press(m, '/')
	typeInto(m, "tick")
	pressKey(m, keyEnter) // jumps to the first match and pauses
	// Far enough down that the filtered view has a pane's worth of lines
	// above the anchor, so the top floor does not move it.
	for i := 0; i < 300; i++ {
		press(m, 'j')
	}
	anchor := m.window()[len(m.window())-1]
	press(m, '&')
	got := m.window()[len(m.window())-1]
	if got.logIdx > anchor.logIdx || anchor.logIdx-got.logIdx >= 5 {
		t.Fatalf("filter on moved from line %d to %d", anchor.logIdx, got.logIdx)
	}
	press(m, '&')
	if back := m.window()[len(m.window())-1]; back.logIdx != got.logIdx {
		t.Fatalf("filter off moved from line %d to %d", got.logIdx, back.logIdx)
	}
}

// ctrl+t flips the timestamps request, marks the switch point in the
// buffer and asks the root to reopen; the retained lines stay.
func TestTimestampsToggleMarksAndAsks(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords([]core.LogRecord{podRec("p", "before")})
	cmd := m.Update(ctrlT())
	if cmd == nil {
		t.Fatal("ctrl+t asked for nothing")
	}
	intent, ok := cmd().(TimestampsIntent)
	if !ok || !intent.On || intent.Ref != testRef() || intent.Container != "main" {
		t.Fatalf("intent = %#v", intent)
	}
	if !m.Timestamps() {
		t.Fatal("the pane does not record timestamps on")
	}
	v := body(m)
	if !strings.Contains(v, "before") || !strings.Contains(v, "── server timestamps on: stream reopened from the start ──") {
		t.Fatalf("switch point missing:\n%s", v)
	}
	intent = m.Update(ctrlT())().(TimestampsIntent)
	if intent.On || m.Timestamps() {
		t.Fatal("a second ctrl+t did not switch timestamps off")
	}
	if !strings.Contains(body(m), "server timestamps off") {
		t.Fatal("the off switch point is not marked")
	}
}

// A new pane for another container keeps wrapping and timestamps, and
// keeps the labels choice between workflow-wide panes.
func TestViewPrefsCarryOver(t *testing.T) {
	prev := testModel(t)
	press(prev, 'w')
	press(prev, 'L')
	prev.Update(ctrlT())
	next := NewModel(testRef(), "", "sidecar")
	next.KeepViewPrefs(prev)
	if !next.wrap || !next.Timestamps() || next.labels {
		t.Fatalf("prefs lost: wrap=%v ts=%v labels=%v", next.wrap, next.Timestamps(), next.labels)
	}
	pod := NewModel(testRef(), "wf-1-a-1", "main")
	press(prev, 'L') // labels back on
	pod.KeepViewPrefs(prev)
	if pod.labels {
		t.Fatal("a pod-scoped pane took the workflow-wide labels choice")
	}
}

// Goldens: labels from the node map, wrapping, the filter and the
// timestamps switch point, plain.
func TestToggleGoldens(t *testing.T) {
	build := func() *Model {
		m := testModel(t)
		m.SetSources(map[string]string{"wf-1-transform-1": "transform(0)", "wf-1-load-2": "load"})
		m.ApplyRecords([]core.LogRecord{
			podRec("wf-1-transform-1", "2026-09-07T10:00:00Z reading raw-extract (184022 rows)"),
			podRec("wf-1-transform-1", "2026-09-07T10:00:03Z ERROR column 'revenue_eur' has 312 nulls; schema requires non-null values in every row"),
			podRec("wf-1-load-2", `{"level":"warn","msg":"slow insert","rows":184022}`),
			podRec("wf-1-extra-3", "errors: 0"),
		})
		return m
	}
	cases := map[string]func(*Model){
		"labels": func(m *Model) {},
		"wrap":   func(m *Model) { press(m, 'w') },
		"filter": func(m *Model) {
			press(m, '/')
			typeInto(m, "rows")
			pressKey(m, keyEnter)
			press(m, '&')
		},
		"timestamps": func(m *Model) {
			m.Update(ctrlT())
			m.ApplyRecords([]core.LogRecord{podRec("wf-1-transform-1", "2026-09-07T10:00:00.137000000Z 2026-09-07T10:00:00Z reading raw-extract (184022 rows)")})
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m := build()
			setup(m)
			golden(t, "toggle-"+name, pane(m))
		})
	}
}
