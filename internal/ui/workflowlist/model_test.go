package workflowlist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
	"argo-tui/internal/ui/shared"
)

// tl builds a list model with plain (deterministic) theme.
func tl(t *testing.T) Model {
	t.Helper()
	th := shared.NewTheme(true)
	os.Unsetenv("NO_COLOR") // theme already forced plain; keep env clean for golden tests
	return New(th, true)
}

// summariesFrom converts fixture workflows to summaries.
func summariesFrom(wfs []core.Workflow) []core.Summary {
	out := make([]core.Summary, 0, len(wfs))
	for _, wf := range wfs {
		out = append(out, wf.Summary)
	}
	return out
}

// key builds a KeyPressMsg for a printable character.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestSelectionAndOpenIntent(t *testing.T) {
	m := tl(t)
	wfs := summariesFrom(testkit.FixtureWorkflowList("ns", 6))
	m.SetItems(wfs, testkit.FixtureEpoch)

	if !m.HasSelection() {
		t.Fatal("no default selection after SetItems")
	}
	first := m.SelectedRef()
	if first.Name == "" {
		t.Fatal("empty selection ref")
	}

	// Move down twice, open intent must carry the selected Ref.
	m.Update(runeKey('j'))
	m.Update(runeKey('j'))
	second := m.SelectedRef()
	if second.Name == first.Name {
		t.Fatalf("selection did not move: %v", second)
	}
	msg := m.OpenIntent()
	ow, ok := msg.(app.OpenWorkflowMsg)
	if !ok {
		t.Fatalf("intent type = %T", msg)
	}
	if ow.Ref.UID != second.UID {
		t.Fatalf("intent UID = %q, want %q", ow.Ref.UID, second.UID)
	}

	// Logs intent for the same row with visible default container (plan §2).
	lm, ok := m.LogsIntent().(app.OpenLogsMsg)
	if !ok {
		t.Fatalf("logs intent type = %T", m.LogsIntent())
	}
	if lm.Ref.UID != second.UID || lm.Container != "main" {
		t.Fatalf("logs intent = %+v", lm)
	}
}

func TestSearchTextEntryKeyIsolation(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 3)), testkit.FixtureEpoch)
	m.Update(runeKey('/'))
	if !m.SearchOn {
		t.Fatal("/ did not focus search")
	}

	// While in text entry: 'q' must TYPE a letter, never quit; 'j' must
	// not move the selection (UI-04).
	sel := m.SelectedRef()
	m.Update(runeKey('j'))
	if got := m.SelectedRef(); got.Name != sel.Name {
		t.Fatal("j moved selection during text entry")
	}
	m.Update(runeKey('q'))
	if m.SearchValue() != "jq" {
		t.Fatalf("search value = %q, want \"jq\" ('j' and 'q' typed)", m.SearchValue())
	}

	// Enter applies the query and exits input mode; esc cancels.
	m.SearchSetValue("fixture-wf-00")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.SearchOn {
		t.Fatal("enter did not leave search mode")
	}
	if m.Query() != "fixture-wf-00" {
		t.Fatalf("query = %q", m.Query())
	}
	if m.VisibleCount() != 3 {
		t.Fatalf("visible = %d, want 3 (all names contain the substring)", m.VisibleCount())
	}

	// Cancel via esc restores the unfiltered view.
	m.Update(runeKey('/'))
	m.SearchSetValue("zzz-no-match")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Query() != "" {
		t.Fatalf("esc must cancel search, query = %q", m.Query())
	}
	if m.VisibleCount() != 3 {
		t.Fatalf("visible after cancel = %d, want 3", m.VisibleCount())
	}
}

func TestSearchScopeVisible(t *testing.T) {
	m := tl(t)
	// 12 fixtures but the (root) snapshot holds only 4: search must say
	// it searched the collected snapshot, not the namespace (LIST-05/09).
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 4)), testkit.FixtureEpoch)
	m.SetQuery("fixture-wf")
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "within 4 collected") {
		t.Fatalf("search scope not visible:\n%s", v)
	}

	// Incomplete snapshot must also show the incomplete marker (LIST-05).
	m.SetStatus(StatusIncomplete, "snapshot cap reached", 0)
	v = m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "INCOMPLETE") {
		t.Fatalf("incomplete marker missing:\n%s", v)
	}
}

func TestDeterministicSorts(t *testing.T) {
	base := testkit.FixtureEpoch
	mk := func(name, phase string, created time.Time, started *time.Time) core.Summary {
		s := core.Summary{
			Ref:       core.Ref{Namespace: "ns", Name: name, UID: "uid-" + name},
			Phase:     phase,
			CreatedAt: created,
			StartedAt: started,
		}
		return s
	}
	started1 := base.Add(-time.Hour)
	started2 := base.Add(-2 * time.Hour)
	items := []core.Summary{
		mk("zebra", "Running", base.Add(-time.Minute), nil),
		mk("apple", "WeirdPhase", base.Add(-3*time.Hour), nil), // unknown phase
		mk("mango", "Failed", base.Add(-2*time.Hour), &started2),
		mk("banana", "Running", base.Add(-5*time.Minute), &started1),
		mk("noplace", "Running", time.Time{}, nil), // missing CreatedAt
	}

	// Phase sort: Failed first, Running next, unknown last; names asc.
	Sort(items, SortPhaseName)
	got := []string{}
	for _, it := range items {
		got = append(got, it.Ref.Name)
	}
	want := []string{"mango", "banana", "noplace", "zebra", "apple"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("phase sort = %v, want %v", got, want)
	}

	// Name sort.
	Sort(items, SortName)
	got = nil
	for _, it := range items {
		got = append(got, it.Ref.Name)
	}
	want = []string{"apple", "banana", "mango", "noplace", "zebra"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("name sort = %v, want %v", got, want)
	}

	// Time sort: newest first; missing CreatedAt strictly last (by name).
	Sort(items, SortTime)
	got = nil
	for _, it := range items {
		got = append(got, it.Ref.Name)
	}
	want = []string{"zebra", "banana", "mango", "apple", "noplace"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("time sort = %v, want %v", got, want)
	}

	// Stability: equal keys keep relative input order.
	a := mk("same", "Running", base.Add(-time.Minute), nil)
	b := mk("same2", "Running", base.Add(-time.Minute), nil)
	items = []core.Summary{a, b}
	Sort(items, SortPhaseName)
	if items[0].Ref.Name != "same" || items[1].Ref.Name != "same2" {
		t.Fatal("stable order not preserved for equal keys")
	}
}

func TestSelectionPreservedAcrossReorderAndDeletion(t *testing.T) {
	m := tl(t)
	wfs := summariesFrom(testkit.FixtureWorkflowList("ns", 5))
	m.SetItems(wfs, testkit.FixtureEpoch)

	// Select a mid-row workflow by moving; note its UID.
	m.Update(runeKey('j'))
	m.Update(runeKey('j'))
	selected := m.SelectedRef()
	if selected.Name == "" {
		t.Fatal("no selection")
	}

	// Reorder the snapshot (reverse) — selection must stay on the same UID
	// even though the row index changed (plan gate: no row-index identity).
	rev := make([]core.Summary, len(wfs))
	for i := range wfs {
		rev[i] = wfs[len(wfs)-1-i]
	}
	m.SetItems(rev, testkit.FixtureEpoch)
	if got := m.SelectedRef(); got.UID != selected.UID {
		t.Fatalf("reorder lost selection: %v != %v", got.UID, selected.UID)
	}

	// Deletion: remove the selected workflow; selection must drop (LIST-13
	// path: it must not silently jump to a row that was elsewhere).
	next := make([]core.Summary, 0, len(rev))
	for _, it := range rev {
		if it.Ref.UID != selected.UID {
			next = append(next, it)
		}
	}
	m.SetItems(next, testkit.FixtureEpoch)
	if m.SelectedRef().UID == selected.UID {
		t.Fatal("deleted workflow still selected")
	}
	if !m.HasSelection() {
		t.Fatal("selection should re-anchor to first row, not vanish")
	}

	// Same name, new UID = a different workflow (LIST-12): selecting the
	// old UID must not reattach to the new one.
	replacement := core.Summary{
		Ref:       core.Ref{Namespace: "ns", Name: selected.Name, UID: "brand-new-uid"},
		Phase:     "Running",
		CreatedAt: testkit.FixtureEpoch,
	}
	m.SetItems([]core.Summary{replacement}, testkit.FixtureEpoch)
	if m.SelectedRef().UID == selected.UID {
		t.Fatal("selection crossed UID boundary")
	}
}

func TestStatusesDistinguishable(t *testing.T) {
	m := tl(t)
	wfs := summariesFrom(testkit.FixtureWorkflowList("ns", 4))
	m.SetItems(wfs, testkit.FixtureEpoch)
	base := m.ViewAt(testkit.FixtureEpoch)

	cases := []struct {
		status Status
		msg    string
		marker string
	}{
		{StatusLoading, "", "loading…"},
		{StatusStale, "connection refused", "stale"},
		{StatusForbidden, "workflows.argoproj.io is forbidden", "forbidden"},
		{StatusUnauthenticated, "token expired", "unauthenticated"},
		{StatusIncomplete, "", "INCOMPLETE"},
	}
	for _, tc := range cases {
		mm := m
		mm.SetStatus(tc.status, tc.msg, 2*time.Minute)
		v := mm.ViewAt(testkit.FixtureEpoch)
		if !strings.Contains(v, tc.marker) {
			t.Errorf("status %d: marker %q missing:\n%s", tc.status, tc.marker, v)
		}
		// Text must differ from the idle view for each state (UI-07).
		if tc.status != StatusIncomplete && v == base {
			t.Errorf("status %d view identical to idle", tc.status)
		}
	}
}

func TestForbiddenErrorSanitized(t *testing.T) {
	m := tl(t)
	// Malicious message must be neutralized before display (SEC-01/02).
	m.SetStatus(StatusForbidden, "denied \x1b]8;;http://evil\x1b\\link\x07back", 0)
	m.SetItems(nil, testkit.FixtureEpoch)
	v := m.ViewAt(testkit.FixtureEpoch)
	if strings.ContainsRune(v, '\x1b') {
		t.Fatalf("escape sequence leaked into view:\n%q", v)
	}
}

func TestUnicodeNameRendering(t *testing.T) {
	m := tl(t)
	wide := core.Summary{
		Ref:       core.Ref{Namespace: "ns", Name: "日本語ワークフロー-表达式", UID: "uid-wide"},
		Phase:     "Running",
		CreatedAt: testkit.FixtureEpoch.Add(-time.Minute),
	}
	ascii := core.Summary{
		Ref:       core.Ref{Namespace: "ns", Name: "ascii-name", UID: "uid-ascii"},
		Phase:     "Failed",
		CreatedAt: testkit.FixtureEpoch.Add(-2 * time.Minute),
	}
	m.SetItems([]core.Summary{wide, ascii}, testkit.FixtureEpoch)
	m.SetSize(80, 24)
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "日本語ワークフロー-表达式") {
		t.Fatalf("wide name dropped:\n%s", v)
	}
	// Alignment: every row line should be a sane cell width (no panic,
	// no negative padding crash) — exercise with narrow width too.
	m.SetSize(60, 24)
	_ = m.ViewAt(testkit.FixtureEpoch)
}

func TestResizeNoticeSmallTerminal(t *testing.T) {
	m := tl(t)
	m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 3)), testkit.FixtureEpoch)
	m.SetSize(40, 10)
	v := m.ViewAt(testkit.FixtureEpoch)
	if !strings.Contains(v, "too small") {
		t.Fatalf("resize notice missing:\n%s", v)
	}
	if !strings.Contains(v, "q quit") {
		t.Fatal("quit hint must survive the tiny-terminal notice (plan §2)")
	}
}

func goldenTestcases() map[string]func(*Model) {
	return map[string]func(*Model){
		"idle":       func(m *Model) {},
		"loading":    func(m *Model) { m.SetStatus(StatusLoading, "", 0) },
		"stale":      func(m *Model) { m.SetStatus(StatusStale, "connection refused", 2*time.Minute) },
		"forbidden":  func(m *Model) { m.SetStatus(StatusForbidden, "workflows list forbidden", 0) },
		"unauth":     func(m *Model) { m.SetStatus(StatusUnauthenticated, "token expired", 0) },
		"incomplete": func(m *Model) { m.SetStatus(StatusIncomplete, "", 0) },
		"filtered":   func(m *Model) { m.SetQuery("fixture-wf-0") },
		"phase":      func(m *Model) { m.SetPhase(PhaseFailed) },
		"empty":      func(m *Model) { m.SetItems(nil, testkit.FixtureEpoch) },
	}
}

func TestGoldenSnapshots(t *testing.T) {
	dir := filepath.Join("testdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, setup := range goldenTestcases() {
		t.Run(name, func(t *testing.T) {
			m := tl(t)
			if name != "empty" {
				m.SetItems(summariesFrom(testkit.FixtureWorkflowList("ns", 6)), testkit.FixtureEpoch)
			}
			setup(&m)
			m.SetSize(80, 24)
			got := m.ViewAt(testkit.FixtureEpoch)
			golden(t, filepath.Join(dir, name+".golden"), got)
		})
	}
}

// golden compares (or writes, with UPDATE_GOLDEN=1) a golden file.
func golden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden missing (%s); run UPDATE_GOLDEN=1 once and review: %v", path, err)
		}
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestSearchBufferEditing(t *testing.T) {
	m := tl(t)
	m.Update(runeKey('/'))
	if !m.SearchOn {
		t.Fatal("/ did not focus search")
	}
	// Empty-state rendering shows the cursor and the usage hint.
	if v := m.ViewAt(testkit.FixtureEpoch); !strings.Contains(v, "[_]  (enter apply, esc cancel)") {
		t.Fatalf("empty search line rendering missing:\n%s", v)
	}
	for _, r := range "abc" {
		m.Update(runeKey(r))
	}
	if m.SearchValue() != "abc" {
		t.Fatalf("buffer = %q, want abc", m.SearchValue())
	}
	// Backspace removes the last typed rune; cursor edits are honored.
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.SearchValue() != "ab" {
		t.Fatalf("after backspace = %q, want ab", m.SearchValue())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(runeKey('X'))
	if m.SearchValue() != "aXb" {
		t.Fatalf("mid-insert = %q, want aXb", m.SearchValue())
	}
	// Control keys are dropped, never inserted and never commands.
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.SearchValue() != "aXb" {
		t.Fatalf("tab leaked into buffer: %q", m.SearchValue())
	}
	// Enter applies (trimmed) and clears the entry buffer.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Query() != "aXb" || m.SearchOn {
		t.Fatalf("enter apply: query=%q on=%v", m.Query(), m.SearchOn)
	}
	if m.SearchValue() != "" {
		t.Fatalf("buffer must clear after apply, got %q", m.SearchValue())
	}
	// Editing over a wide rune stays rune-correct (no byte slicing).
	m.Update(runeKey('/'))
	for _, r := range "日本" {
		m.Update(runeKey(r))
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.SearchValue() != "日" {
		t.Fatalf("wide-rune backspace = %q, want 日", m.SearchValue())
	}
}
