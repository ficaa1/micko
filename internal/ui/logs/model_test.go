package logs

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// ---------------------------------------------------------------------------
// model tests — follow/pause/autoscroll (plan §8 D1 slice; acceptance
// LOG-07/LOG-08), the "no autoscroll jump while paused" plan gate,
// retained-buffer search UX (LOG-09), and key isolation (UI-04).
// ---------------------------------------------------------------------------

// TestSpacePausesFollow pins Space = pause (LOG-07): a pure state change
// with no transport effect.
func TestSpacePausesFollow(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(5))
	if cmd := press(m, ' '); cmd != nil {
		t.Fatal("pause must be a pure key-side state change (no command)")
	}
	if !m.Paused() || m.Following() {
		t.Fatalf("after Space: paused=%v following=%v, want true/false", m.Paused(), m.Following())
	}
	if m.Phase() != PhasePaused {
		t.Fatalf("phase after Space = %v, want paused", m.Phase())
	}
}

// TestFollowResumesWithT pins `t` (tail) = resume follow (LOG-08). It used
// to be `f`, which now means the full-screen raw view on every route.
func TestFollowResumesWithT(t *testing.T) {
	m := testModel(t)
	press(m, ' ')
	press(m, 't')
	if !m.Following() || m.Paused() {
		t.Fatalf("after t: paused=%v following=%v, want false/true", m.Paused(), m.Following())
	}
}

// TestNoAutoscrollJumpWhilePaused is the plan gate: while paused, arriving
// lines must never move the viewport — the same rows stay visible. The
// count line may change (retention counters are live), the window rows may
// not.
func TestNoAutoscrollJumpWhilePaused(t *testing.T) {
	m := testModel(t)
	vh := viewportRows(m)
	m.ApplyRecords(recs(vh + 10)) // more than one window
	press(m, ' ')                 // pause

	rowsBefore := windowTexts(m)
	// A batch arrives while paused: viewport must not move.
	m.ApplyRecords(recs(7))
	rowsAfter := windowTexts(m)
	if len(rowsBefore) != len(rowsAfter) {
		t.Fatalf("window height changed while paused: %d → %d", len(rowsBefore), len(rowsAfter))
	}
	for i := range rowsBefore {
		if rowsBefore[i] != rowsAfter[i] {
			t.Fatalf("row %d moved while paused: %q → %q", i, rowsBefore[i], rowsAfter[i])
		}
	}
}

// TestPauseDoesNotGrowMemory pins the LOG-07 memory bound at the component
// level: pausing stops viewport motion but collection still lands in the
// bounded buffer — a sustained feed while paused stays capped.
func TestPauseDoesNotGrowMemory(t *testing.T) {
	m := testModel(t)
	press(m, ' ') // pause
	for i := 0; i < MaxLines+2000; i++ {
		m.ApplyRecords([]core.LogRecord{rec("flood-" + strconv.Itoa(i%1000))})
	}
	lines, _, _, _, _ := m.buf.Counts()
	if lines > MaxLines {
		t.Fatalf("retained %d lines while paused; cap %d exceeded (unbounded growth)", lines, MaxLines)
	}
}

// TestFollowPinsTail pins that following re-pins the tail on each render:
// the last line is visible while autoscrolling (LOG-07 follow semantics).
func TestFollowPinsTail(t *testing.T) {
	m := testModel(t)
	vh := viewportRows(m)
	m.ApplyRecords(recs(vh + 5))
	w := windowTexts(m)
	last := w[len(w)-1]
	if !strings.HasSuffix(last, "line-"+strconv.Itoa(vh+4)) {
		t.Fatalf("tail not pinned while following: last row = %q", last)
	}
}

// TestManualScrollDetachesFollow pins that explicit upward scroll pauses
// follow (the user asked to read history); the tail stops being pinned.
func TestManualScrollDetachesFollow(t *testing.T) {
	m := testModel(t)
	vh := viewportRows(m)
	m.ApplyRecords(recs(vh + 5))
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.Following() || !m.Paused() {
		t.Fatalf("scroll-up must detach follow: paused=%v following=%v", m.Paused(), m.Following())
	}
	// 26 rows total; maxBottom = 25; one up = bottom 24 → window [5..24],
	// so the first visible row is line-4 (after the context marker row 0).
	w := windowTexts(m)
	if !strings.HasSuffix(w[0], "line-4") {
		t.Fatalf("scroll-up did not move the window up: first row = %q", w[0])
	}
}

// TestSearchScopeIsRetainedBuffer pins LOG-09 UX: after applying a search,
// the count line states the retained-buffer scope explicitly and reports the
// position within the matches.
func TestSearchScopeIsRetainedBuffer(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(6))
	press(m, '/')         // focus search
	typeInto(m, "line-2") // matches line-2 (and line-2x prefixes)
	pressKey(m, keyEnter)
	if m.searchOn {
		t.Fatal("enter must close the search editor")
	}
	v := m.View()
	if !strings.Contains(v, "search scope: retained buffer only") {
		t.Fatalf("search scope not visible:\n%s", v)
	}
	if !strings.Contains(v, `search "line-2": 1/1`) {
		t.Fatalf("match position missing:\n%s", v)
	}
	// The line itself is never edited. An annotation pushed the content right
	// and travelled into every copy of the buffer.
	if strings.Contains(v, "[match") {
		t.Fatalf("search still rewrites the log line:\n%s", v)
	}
}

// A search must show the reader the word they searched for. With the plain
// theme there is nothing to see, so this pins the coloured one.
func TestSearchHighlightsTheTermInPlace(t *testing.T) {
	m := testModel(t)
	th := shared.NewTheme(false)
	m.SetTheme(th)
	m.ApplyRecords(recs(6))
	m.applySearch("line-2")
	v := m.View()
	if !strings.Contains(v, th.Selected.Render("line-2")) {
		t.Fatalf("no highlight reached the screen:\n%q", v)
	}
	// The retained buffer itself stays clean: raw text is what a copy yanks.
	for _, l := range m.RawLines() {
		if strings.Contains(l, "\x1b[") {
			t.Fatalf("styling leaked into the retained line %q", l)
		}
	}
}

// n and N walk the hits and wrap. Before this a search reported a count and
// left the reader to scroll for the lines themselves.
func TestNextAndPreviousMatchWalkTheHits(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(6))
	m.applySearch("line-") // every line matches
	if len(m.hits) != 6 {
		t.Fatalf("hits = %d, want 6", len(m.hits))
	}
	if m.curHit != 0 {
		t.Fatalf("a fresh search must land on the first hit, got %d", m.curHit)
	}
	press(m, 'n')
	if m.curHit != 1 {
		t.Fatalf("n did not advance: %d", m.curHit)
	}
	press(m, 'N')
	if m.curHit != 0 {
		t.Fatalf("N did not go back: %d", m.curHit)
	}
	press(m, 'N')
	if m.curHit != 5 {
		t.Fatalf("N at the first hit must wrap to the last, got %d", m.curHit)
	}
	if m.follow {
		t.Fatal("jumping to a match must detach follow, or the tail drags the reader off it")
	}
}

// TestSearchEscCancelRestores pins that esc cancels a search edit without
// touching the committed search state.
func TestSearchEscCancelRestores(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(4))
	press(m, '/')
	typeInto(m, "zzz")
	pressKey(m, keyEscape)
	if m.searchOn {
		t.Fatal("esc must close the search editor")
	}
	if m.search.term != "" || m.hits != nil {
		t.Fatalf("esc applied the edit: term=%q hits=%v", m.search.term, m.hits)
	}
	if v := m.View(); strings.Contains(v, "zzz") {
		t.Fatalf("cancelled term leaked into the view:\n%s", v)
	}
}

// TestSearchApplyThenNavigate pins the committed-search state survives
// scrolling and new records.
func TestSearchApplyThenNavigate(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(8))
	press(m, '/')
	typeInto(m, "line-3")
	pressKey(m, keyEnter)
	m.ApplyRecords(recs(4)) // more lines arrive after the search
	if len(m.hits) != 1 || m.hits[0] != 3 {
		t.Fatalf("hits = %v, want [3]", m.hits)
	}
}

// TestContextEditorOpensWithPrefill pins LOG-10: the editor prefills the
// current pod (empty = workflow-wide) and the visible container default.
func TestContextEditorOpensWithPrefill(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	if !m.contextOn {
		t.Fatal("c did not open the context editor")
	}
	if m.containerBuf != "main" {
		t.Fatalf("container editor prefilled = %q, want main (default visible/editable)", m.containerBuf)
	}
	if v := m.View(); !strings.Contains(v, "[main]") {
		t.Fatalf("default container not visible in the editor:\n%s", v)
	}
}

// TestContextSwitchEmitsIntent pins that confirming the editor emits the
// SwitchContextIntent for the root and switches the scope.
func TestContextSwitchEmitsIntent(t *testing.T) {
	m := testModel(t)
	cmd := press(m, 'c')
	if cmd != nil {
		t.Fatal("c must only open the editor, not emit an intent")
	}
	typeInto(m, "x") // container "mainx"
	cmd = pressKey(m, keyEnter)
	if cmd == nil {
		t.Fatal("confirm produced no command (intent expected)")
	}
	if m.contextOn {
		t.Fatal("confirm must close the editor")
	}
	if m.container != "mainx" {
		t.Fatalf("container = %q, want mainx", m.container)
	}
	// The returned command must yield the intent message.
	msg := cmd()
	intent, ok := msg.(SwitchContextIntent)
	if !ok {
		t.Fatalf("intent type = %T, want SwitchContextIntent", msg)
	}
	if intent.Container != "mainx" || intent.PodName != "" {
		t.Fatalf("intent = %+v", intent)
	}
	if intent.Ref.UID != "u1" {
		t.Fatalf("intent ref = %+v", intent.Ref)
	}
}

// TestContextBlankContainerRefused pins that an emptied container is
// refused with the editor kept open (never silently guessed, LOG-10).
func TestContextBlankContainerRefused(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	// Backspace "main" to empty, then confirm.
	for i := 0; i < 4; i++ {
		pressKey(m, keyBackspace)
	}
	if m.containerBuf != "" {
		t.Fatalf("backspace did not empty the container buffer: %q", m.containerBuf)
	}
	pressKey(m, keyEnter)
	if !m.contextOn {
		t.Fatal("blank container must keep the editor open")
	}
	if !strings.Contains(m.View(), "must not be empty") {
		t.Fatalf("refusal message missing:\n%s", m.View())
	}
}

// TestContextEscCancelsWithoutIntent pins esc = no intent, editor closes.
func TestContextEscCancelsWithoutIntent(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	typeInto(m, "x")
	pressKey(m, keyEscape)
	if m.contextOn {
		t.Fatal("esc must close the editor")
	}
	if m.container != "main" {
		t.Fatalf("esc must not apply the edit: container = %q", m.container)
	}
}

// TestSpaceDoesNotTypeInEditor pins UI-04 key isolation: while the
// context editor has focus, Space is input, not pause.
func TestSpaceDoesNotTypeInEditor(t *testing.T) {
	m := testModel(t)
	press(m, 'c')
	press(m, ' ')
	if m.Paused() {
		t.Fatal("space paused while the editor had focus (UI-04 violation)")
	}
}

// TestSearchModeConsumesCommandKeys pins UI-04 for the search editor:
// typed command letters do not trigger browse-mode actions.
func TestSearchModeConsumesCommandKeys(t *testing.T) {
	m := testModel(t)
	press(m, '/') // focus search
	press(m, 'c') // types 'c', must NOT open the context editor
	if m.contextOn {
		t.Fatal("command key leaked into the search editor (UI-04)")
	}
	if m.searchBuf != "c" {
		t.Fatalf("printable input not delivered to the editor: %q", m.searchBuf)
	}
	pressKey(m, keyBackspace)
	if m.searchBuf != "" {
		t.Fatalf("backspace did not edit: %q", m.searchBuf)
	}
}

// TestGlobalKeysNotShadowed pins that browse mode leaves root-owned keys
// (q/n/p/?) alone (UI-04: the child must not shadow them).
func TestGlobalKeysNotShadowed(t *testing.T) {
	m := testModel(t)
	for _, k := range []rune{'q', 'n', 'p', '?'} {
		if cmd := press(m, k); cmd != nil {
			t.Fatalf("key %q produced a command; root-owned keys must pass through", k)
		}
	}
	if m.searchOn || m.contextOn {
		t.Fatal("root-owned keys must not open child editors")
	}
}

// TestOpenIntentCarriesFrozenContract pins the fresh-open path against the
// frozen shared contract (docs/development.md): OpenLogsMsg shape with
// the explicit container default.
func TestOpenIntentCarriesFrozenContract(t *testing.T) {
	m := testModel(t)
	msg := m.OpenIntent()
	om, ok := msg.(shared.OpenLogsMsg)
	if !ok {
		t.Fatalf("intent type = %T, want shared.OpenLogsMsg", msg)
	}
	if om.Container != "main" || om.Ref.UID != "u1" || om.PodName != "" {
		t.Fatalf("OpenLogsMsg = %+v", om)
	}
}

// TestSetErrorSurfacesHonestState pins LOG-12: an error state renders the
// sanitized cause, distinguishably from ended.
func TestSetErrorSurfacesHonestState(t *testing.T) {
	m := testModel(t)
	m.SetError("pod logs unavailable: pod deleted (log source gone)")
	if m.Phase() != PhaseError {
		t.Fatalf("phase = %v, want error", m.Phase())
	}
	v := m.View()
	if !strings.Contains(v, "[ERROR]") || !strings.Contains(v, "pod deleted") {
		t.Fatalf("error state not visible:\n%s", v)
	}
	if strings.ContainsRune(v, 0x1b) {
		t.Fatal("unsanitized bytes in error text")
	}
}

// TestResizeInvalidatesGeometry pins that a resize keeps the view
// renderable (no stale geometry) and stays within bounds.
func TestResizeInvalidatesGeometry(t *testing.T) {
	m := testModel(t)
	m.ApplyRecords(recs(40))
	m.SetSize(80, 10)
	v := m.View()
	if strings.Count(v, "\n") != 10-1 { // View emits exactly height lines
		t.Fatalf("view lines = %d, want %d after resize", strings.Count(v, "\n"), 10-1)
	}
}

// windowTexts returns the currently visible row texts (the scroll window).
func windowTexts(m *Model) []string {
	w := m.window()
	out := make([]string, 0, len(w))
	for _, r := range w {
		out = append(out, r.text)
	}
	return out
}

var _ = core.LogRecord{}
