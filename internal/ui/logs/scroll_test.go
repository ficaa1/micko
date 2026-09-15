package logs

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
)

func scrollModel(t *testing.T, lines int) *Model {
	t.Helper()
	m := NewModel(core.Ref{Namespace: "ns", Name: "wf", UID: "u"}, "", "main")
	m.SetPaneMode(true)
	m.SetSize(100, 12)
	recs := make([]core.LogRecord, 0, lines)
	for i := 0; i < lines; i++ {
		recs = append(recs, core.LogRecord{Content: "line-" + itoaView(i)})
	}
	m.ApplyRecords(recs)
	return m
}

// Scrolling up past the oldest line must keep the pane full. Clamping the
// window to rows[0:bottom+1] instead would eat rows off the BOTTOM, showing
// one line less on each further press.
func TestScrollingAboveTheFirstLineKeepsThePaneFull(t *testing.T) {
	m := scrollModel(t, 60)
	full := len(m.window())
	if full != m.viewRows() {
		t.Fatalf("a full buffer must fill the pane: %d rows for %d view rows", full, m.viewRows())
	}
	// Far more presses than there are lines above the window.
	for i := 0; i < 200; i++ {
		m.scrollBy(-1)
	}
	if got := len(m.window()); got != full {
		t.Fatalf("after scrolling to the top the pane shows %d rows, want %d", got, full)
	}
	rows := m.window()
	if rows[0].text != m.rows[0].text {
		t.Fatalf("the top of the pane is not the oldest row: %q", rows[0].text)
	}
}

// A buffer shorter than the pane has nothing to scroll; it must not be
// clipped or padded into something else.
func TestShortBufferIsShownWhole(t *testing.T) {
	m := scrollModel(t, 3)
	before := len(m.window())
	for i := 0; i < 10; i++ {
		m.scrollBy(-1)
	}
	if got := len(m.window()); got != before {
		t.Fatalf("short buffer changed size on scroll: %d then %d", before, got)
	}
}

// G is how a reader gets back to live output after reading history.
func TestGReturnsToFollowingTheNewestLine(t *testing.T) {
	m := scrollModel(t, 60)
	m.scrollBy(-20)
	if m.Following() {
		t.Fatal("scrolling up must detach follow")
	}
	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !m.Following() {
		t.Fatal("G must re-attach follow")
	}
	rows := m.window()
	if rows[len(rows)-1].text != m.rows[len(m.rows)-1].text {
		t.Fatal("G must show the newest line at the bottom")
	}
}

// gg is two presses, and a lone g must not be treated as a command.
func TestGGJumpsToTheOldestRetainedLine(t *testing.T) {
	m := scrollModel(t, 60)
	press(m, 'g')
	if !m.Following() {
		t.Fatal("a single g must not scroll")
	}
	press(m, 'g')
	if m.Following() {
		t.Fatal("gg must detach follow: the reader asked for history")
	}
	if m.window()[0].text != m.rows[0].text {
		t.Fatal("gg must show the oldest retained row first")
	}
}

// pgup must not drive the stored bottom edge below what the renderer draws.
// A full pane on screen against a bottom edge of 0 disagree by one page, and
// arrow-down then moves the state and nothing else: the pane looks frozen
// until the reader presses down a whole page of times.
func TestArrowDownWorksImmediatelyAfterPagingToTheTop(t *testing.T) {
	m := scrollModel(t, 200)
	for i := 0; i < 20; i++ {
		m.scrollBy(-m.pageRows())
	}
	top := m.window()
	if top[0].text != m.rows[0].text {
		t.Fatalf("pgup did not reach the oldest line: %q", top[0].text)
	}

	m.scrollBy(1)
	after := m.window()
	if after[0].text == top[0].text {
		t.Fatalf("one arrow-down after reaching the top moved nothing: still %q", after[0].text)
	}
	if after[0].text != m.rows[1].text {
		t.Fatalf("arrow-down moved by more than one row: %q, want %q", after[0].text, m.rows[1].text)
	}
}
