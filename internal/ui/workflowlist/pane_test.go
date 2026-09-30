package workflowlist

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shell"
)

func crossNamespaceItems() []core.Summary {
	a, b, c := summary("etl", "Running"), summary("etl", "Running"), summary("sweep", "Succeeded")
	a.Ref.Namespace = "team-b"
	a.Ref.UID = "uid-b"
	b.Ref.Namespace = "team-a"
	b.Ref.UID = "uid-a"
	c.Ref.Namespace = "ml"
	c.Ref.UID = "uid-m"
	return []core.Summary{a, b, c}
}

func shellFrame(m *Model, w, h int) shell.Frame {
	f := shell.Frame{Width: w, Height: h, App: "micko", Server: "fixture", Namespace: "ns", Mode: "READ ONLY", Title: m.PaneTitle(), Route: "list", Help: "? help"}
	if m.allNS {
		f.Namespace = "all"
	}
	m.SetSize(f.BodyWidth(), f.BodyHeight())
	f.Body = m.BodyLines(testkit.FixtureEpoch)
	f.Hints = m.Hints()
	f.Status = m.WindowStatus()
	return f
}

// The pane keeps selection visible and reports its actual row window across sizes and navigation keys.
func TestPaneGeometry(t *testing.T) {
	items := make([]core.Summary, 40)
	for i := range items {
		items[i] = summary(fmt.Sprintf("row-%02d", i), "Running")
	}
	for _, h := range []int{0, 1, 2, 3, 5, 8, 12, 20, 45} {
		t.Run(strconv.Itoa(h), func(t *testing.T) {
			m := newList(t)
			m.SetItems(items, testkit.FixtureEpoch)
			for _, at := range []int{0, 30, 39, 0} {
				m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
				for i := 0; i < at; i++ {
					m.Update(runeKey('j'))
				}
				m.SetSize(100, h)
				lines := m.BodyLines(testkit.FixtureEpoch)
				if h > 0 && len(lines) > h {
					t.Fatalf("body %d > height %d", len(lines), h)
				}
				if !strings.Contains(strings.Join(lines, "\n"), fmt.Sprintf("row-%02d", at)) {
					t.Fatalf("selected row %d hidden: %v", at, lines)
				}
				count := 40
				if h > 0 {
					count = max(1, min(40, h-2))
				}
				end := min(40, max(count, at+1))
				start := end - count
				want := fmt.Sprintf("%d-%d/40", start+1, end)
				if count == 40 {
					want = "40 shown"
				}
				if m.WindowStatus() != want {
					t.Fatalf("status %q want %q", m.WindowStatus(), want)
				}
				if rows := strings.Count(strings.Join(lines, "\n"), "row-"); rows != count {
					t.Fatalf("rendered %d data rows want %d", rows, count)
				}
			}
			m.SetSize(100, 3)
			m.BodyLines(testkit.FixtureEpoch)
			m.SetSize(100, 12)
			if !strings.Contains(body(&m), "row-00") || m.WindowStatus() != "1-10/40" {
				t.Fatalf("resized body=%q status=%q, want row-00 visible and 1-10/40", body(&m), m.WindowStatus())
			}
		})
	}
	m := newList(t)
	m.SetItems(items, testkit.FixtureEpoch)
	m.SetSize(100, 8)
	m.BodyLines(testkit.FixtureEpoch)
	for _, c := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, "row-05"},
		{tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, "row-10"},
		{tea.KeyPressMsg{Code: tea.KeyPgUp}, "row-05"},
		{tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, "row-00"},
		{runeKey('G'), "row-39"},
		{tea.KeyPressMsg{Code: tea.KeyHome}, "row-00"},
		{tea.KeyPressMsg{Code: tea.KeyEnd}, "row-39"},
		{runeKey('g'), "row-39"},
		{runeKey('g'), "row-00"},
		{runeKey('g'), "row-00"},
		{runeKey('j'), "row-01"},
		{runeKey('g'), "row-01"},
		{runeKey('g'), "row-00"},
	} {
		m.Update(c.key)
		if m.SelectedRef().UID != c.want {
			t.Fatalf("key %s selected %q want %q", c.key.String(), m.SelectedRef().UID, c.want)
		}
		m.BodyLines(testkit.FixtureEpoch)
	}
}

// Shell frames show the list states, filtering, wide metadata and marks without duplicate pane chrome.
func TestPaneGoldenSnapshots(t *testing.T) {
	setTimeZone(t, time.UTC)
	for _, c := range []struct {
		name       string
		status     Status
		msg        string
		empty, all bool
		query      string
		phase      PhaseFilter
		width      int
	}{
		{name: "idle", width: 100},
		{name: "loading", status: StatusLoading, empty: true, width: 100},
		{name: "stale", status: StatusStale, msg: "connection refused", width: 100},
		{name: "forbidden", status: StatusForbidden, msg: "workflows list forbidden", empty: true, width: 100},
		{name: "unauth", status: StatusUnauthenticated, msg: "token expired", empty: true, width: 100},
		{name: "incomplete", status: StatusIncomplete, width: 160},
		{name: "filtered", query: "fixture-wf-0", width: 100},
		{name: "phase", phase: PhaseFailed, width: 100},
		{name: "empty", empty: true, width: 100},
		{name: "allns-empty", empty: true, all: true, width: 100},
		{name: "first-error", status: StatusStale, msg: "connection refused", empty: true, width: 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newList(t)
			if !c.empty {
				m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 6)), testkit.FixtureEpoch)
			}
			m.SetAllNamespaces(c.all)
			m.SetStatus(c.status, c.msg, 2*time.Minute)
			if c.query != "" {
				editQuery(&m, c.query)
				m.Update(enterKey())
			}
			phase := c.phase
			if phase == "" {
				phase = PhaseAll
			}
			for range phaseCycle {
				if m.phase == phase {
					break
				}
				m.Update(runeKey('p'))
			}
			if m.phase != phase {
				t.Fatalf("phase bucket = %q, want %q", m.phase, phase)
			}
			f := shellFrame(&m, c.width, 14)
			golden(t, c.name, f.Render(m.theme))
		})
	}
	for _, w := range []int{160, 100} {
		m := newList(t)
		items, now := demoSummaries(t)
		m.SetItems(items, now)
		m.Update(runeKey('w'))
		f := shellFrame(&m, w, 20)
		golden(t, "wide-"+strconv.Itoa(w), f.Render(m.theme))
	}
	m := newList(t)
	items, now := demoSummaries(t)
	m.SetItems(items, now)
	for i := 0; i < 3; i++ {
		m.Update(spaceKey())
		m.Update(runeKey('j'))
	}
	editQuery(&m, "phase=failed tmpl=nightly-report|cron=demo-etl-hourly")
	m.Update(enterKey())
	f := shellFrame(&m, 120, 12)
	golden(t, "marks-query", f.Render(m.theme))
}

// Panes below the usable width show resize guidance and reachable quit and help keys.
func TestResizeNotice(t *testing.T) {
	for _, w := range []int{1, 40, 59} {
		m := newList(t)
		m.SetSize(w, 12)
		got := body(&m)
		for _, want := range []string{"too small", "Resize to at least 60 columns", "q quit", "? help"} {
			if !strings.Contains(got, want) {
				t.Fatalf("width %d missing %q: %s", w, want, got)
			}
		}
	}
}

// The list advertises its full keyboard contract without repeating the shell help hint.
func TestHints(t *testing.T) {
	m := newList(t)
	want := "enter open  l logs  T timeline  X explain  E events  / search  s sort  p phase  space mark  a actions  n namespace  0 all ns  r refresh  w wide"
	if m.Hints() != want {
		t.Fatalf("hints = %q, want %q", m.Hints(), want)
	}
}

// Normal, wide and namespace layouts fit the pane and drop optional columns as space shrinks.
func TestColumnsAtEachWidth(t *testing.T) {
	t.Run("wide toggle", func(t *testing.T) {
		m := newList(t)
		m.SetSize(140, 20)
		m.SetItems([]core.Summary{summary("row", "Running")}, testkit.FixtureEpoch)
		for _, wide := range []bool{true, false} {
			m.Update(runeKey('w'))
			head := m.BodyLines(testkit.FixtureEpoch)[1]
			if strings.Contains(head, "STARTED") != wide {
				t.Fatalf("wide=%v header %q", wide, head)
			}
		}
	})
	for _, c := range []struct {
		width int
		wide  bool
		head  string
	}{
		{200, true, "NAME PHASE AGE DURATION PROGRESS STARTED FINISHED TEMPLATE CRON LABELS MESSAGE"},
		{160, true, "NAME PHASE AGE DURATION PROGRESS STARTED FINISHED TEMPLATE CRON MESSAGE"},
		{140, true, "NAME PHASE AGE DURATION PROGRESS STARTED FINISHED TEMPLATE MESSAGE"},
		{120, true, "NAME PHASE AGE DURATION PROGRESS STARTED TEMPLATE"},
		{100, true, "NAME PHASE AGE DURATION PROGRESS STARTED"},
		{80, true, "NAME PHASE AGE DURATION PROGRESS"},
		{60, true, "NAME PHASE AGE DURATION"},
		{160, false, "NAME PHASE AGE DURATION PROGRESS MESSAGE"},
		{120, false, "NAME PHASE AGE DURATION PROGRESS MESSAGE"},
		{119, false, "NAME PHASE AGE DURATION MESSAGE"},
		{100, false, "NAME PHASE AGE DURATION MESSAGE"},
		{80, false, "NAME PHASE AGE DURATION MESSAGE"},
		{60, false, "NAME PHASE AGE DURATION"},
		{70, false, "NAME PHASE AGE DURATION"},
	} {
		m := newList(t)
		m.SetSize(c.width, 20)
		if c.wide {
			m.Update(runeKey('w'))
		}
		row := summary(strings.Repeat("n", 120), "Failed")
		row.Message = strings.Repeat("m", 400)
		m.SetItems([]core.Summary{row}, testkit.FixtureEpoch)
		lines := m.BodyLines(testkit.FixtureEpoch)
		if len(lines) != 3 || !slices.Equal(strings.Fields(lines[1]), strings.Fields(c.head)) {
			t.Fatalf("width %d wide %v: %v", c.width, c.wide, lines)
		}
		for _, l := range lines[1:] {
			if ansi.StringWidth(l) > c.width {
				t.Fatalf("overflow width %d: %q", c.width, l)
			}
		}
		if !strings.Contains(c.head, "MESSAGE") && strings.Contains(lines[2], "mmmm") {
			t.Fatalf("header=%q row=%q, want no message cell", lines[1], lines[2])
		}
	}
	for _, w := range []int{60, 76, 100, 136} {
		m := newList(t)
		m.SetSize(w, 20)
		m.SetAllNamespaces(true)
		m.SetItems(crossNamespaceItems(), testkit.FixtureEpoch)
		lines := m.BodyLines(testkit.FixtureEpoch)
		if len(lines) != 5 || strings.Fields(lines[1])[0] != "NAMESPACE" {
			t.Fatalf("namespace body = %v, want a NAMESPACE header and three rows", lines)
		}
		for _, l := range lines[1:] {
			if ansi.StringWidth(l) > w {
				t.Fatalf("allns width %d overflow: %s", w, l)
			}
		}
		for _, want := range []string{"team-a     etl", "team-b     etl", "ml         sweep"} {
			if !strings.Contains(strings.Join(lines, "\n"), want) {
				t.Fatalf("namespace value %q missing", want)
			}
		}
	}
	m := newList(t)
	m.SetSize(80, 20)
	m.SetAllNamespaces(true)
	row := summary("wf", "Running")
	row.Ref.Namespace = strings.Repeat("n", 40)
	m.SetItems([]core.Summary{row}, testkit.FixtureEpoch)
	if !strings.HasPrefix(m.BodyLines(testkit.FixtureEpoch)[2], "  "+strings.Repeat("n", 15)+"…  wf") {
		t.Fatalf("namespace row = %q, want 15 n cells followed by an ellipsis and wf", m.BodyLines(testkit.FixtureEpoch)[2])
	}
}

// Wider panes display a longer message prefix while retaining an ellipsis for clipped text.
func TestMessageGrowth(t *testing.T) {
	for _, c := range []struct{ w, visible int }{
		{100, 17},
		{140, 43},
		{200, 103},
	} {
		m := newList(t)
		m.SetSize(c.w, 20)
		r := summary("row", "Failed")
		r.Message = strings.Repeat("m", 400)
		m.SetItems([]core.Summary{r}, testkit.FixtureEpoch)
		lines := m.BodyLines(testkit.FixtureEpoch)
		got := renderedColumn(t, lines[1], lines[2], "MESSAGE")
		want := strings.Repeat("m", c.visible-1) + "…"
		if got != want {
			t.Fatalf("width %d message = %q, want %q", c.w, got, want)
		}
	}
}
