package logs

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
)

// scrollModel is a 100×12 pane holding n lines.
func scrollModel(t *testing.T, n int) *Model {
	t.Helper()
	m := NewModel(core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, "", "main")
	m.SetSize(100, 12)
	m.ApplyRecords(recs(n))
	return m
}

// Space pauses, t resumes, and G returns to the newest line. The status
// cell says which state the pane is in; pausing is a pure key-side change
// that asks the root for nothing.
func TestPauseAndResumeKeys(t *testing.T) {
	m := scrollModel(t, 60)
	for _, s := range []struct {
		key  rune
		want string
	}{
		{' ', "PAUSED · paused"},
		{'t', "FOLLOWING · follow"},
		{' ', "PAUSED · paused"},
		{'G', "FOLLOWING · follow"},
	} {
		if cmd := press(m, s.key); cmd != nil {
			t.Fatalf("%q asked the root for something", s.key)
		}
		if got := m.PaneStatus(); got != s.want {
			t.Fatalf("after %q the status is %q, want %q", s.key, got, s.want)
		}
	}
	rows := m.window()
	if rows[len(rows)-1].text != m.rows[len(m.rows)-1].text {
		t.Fatal("G did not show the newest line at the bottom")
	}
}

// Following keeps the newest line at the bottom as lines arrive. While
// paused, arriving lines never move the window.
func TestFollowPinsTheTailAndPauseFreezesIt(t *testing.T) {
	m := testModel(t)
	vh := m.viewRows()
	m.ApplyRecords(recs(vh + 5))
	if w := windowTexts(m); !strings.HasSuffix(w[len(w)-1], "line-"+itoa(vh+4)) {
		t.Fatalf("following, the last row is %q", w[len(w)-1])
	}
	press(m, ' ')
	before := windowTexts(m)
	m.ApplyRecords(recs(7))
	after := windowTexts(m)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("the window moved while paused:\n%q\n%q", before, after)
	}
}

// Scrolling up detaches follow: the reader asked for history.
func TestScrollingUpDetachesFollow(t *testing.T) {
	m := testModel(t)
	vh := m.viewRows()
	m.ApplyRecords(recs(vh + 5))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.follow || !m.paused {
		t.Fatalf("scroll-up left follow=%v paused=%v", m.follow, m.paused)
	}
	// One row up from the tail: the stream header is row 0, so the window
	// now starts at line-4.
	if w := windowTexts(m); !strings.HasSuffix(w[0], "line-4") {
		t.Fatalf("the window did not move up: first row %q", w[0])
	}
}

// gg is two presses and jumps to the oldest retained line; a lone g does
// nothing.
func TestGGJumpsToTheOldestRetainedLine(t *testing.T) {
	m := scrollModel(t, 60)
	press(m, 'g')
	if !m.follow {
		t.Fatal("a single g scrolled")
	}
	press(m, 'g')
	if m.follow || m.window()[0].text != m.rows[0].text {
		t.Fatal("gg did not show the oldest retained row first")
	}
}

// Scrolling past the oldest line keeps the pane full. Clamping the window
// to rows[0:bottom+1] instead eats rows off the bottom, one more per press.
func TestScrollingAboveTheFirstLineKeepsThePaneFull(t *testing.T) {
	m := scrollModel(t, 60)
	full := len(m.window())
	if full != m.viewRows() {
		t.Fatalf("a full buffer shows %d rows in %d view rows", full, m.viewRows())
	}
	for i := 0; i < 200; i++ {
		m.scrollBy(-1)
	}
	if got := m.window(); len(got) != full || got[0].text != m.rows[0].text {
		t.Fatalf("at the top the pane shows %d rows starting %q", len(got), got[0].text)
	}
}

// A buffer shorter than the pane has nothing to scroll and stays whole.
func TestShortBufferIsShownWhole(t *testing.T) {
	m := scrollModel(t, 3)
	before := len(m.window())
	for i := 0; i < 10; i++ {
		m.scrollBy(-1)
	}
	if got := len(m.window()); got != before {
		t.Fatalf("a short buffer went from %d rows to %d on scroll", before, got)
	}
}

// Paging up must not drive the stored bottom edge below what is drawn:
// otherwise arrow-down moves the state and not the screen, and the pane
// looks frozen for a page of presses.
func TestArrowDownWorksImmediatelyAfterPagingToTheTop(t *testing.T) {
	m := scrollModel(t, 200)
	for i := 0; i < 20; i++ {
		m.scrollBy(-m.pageRows())
	}
	if top := m.window(); top[0].text != m.rows[0].text {
		t.Fatalf("pgup did not reach the oldest line: %q", top[0].text)
	}
	m.scrollBy(1)
	if got := m.window()[0].text; got != m.rows[1].text {
		t.Fatalf("one arrow-down after the top shows %q first, want %q", got, m.rows[1].text)
	}
}
