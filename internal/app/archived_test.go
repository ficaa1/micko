package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/actions"
	"github.com/ficaa1/argo-tui/internal/ui/logs"
)

// archiveRecorder is the demo reader with its archive and live reads
// counted, so a test can see which route a detail came from.
type archiveRecorder struct {
	*testkit.FakeReader
	mu                 sync.Mutex
	liveGets, archGets int
	archQueries        []core.ArchiveQuery
}

func (r *archiveRecorder) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	r.mu.Lock()
	r.liveGets++
	r.mu.Unlock()
	return r.FakeReader.Get(ctx, ref)
}

func (r *archiveRecorder) GetArchivedWorkflow(ctx context.Context, uid string) (core.Workflow, error) {
	r.mu.Lock()
	r.archGets++
	r.mu.Unlock()
	return r.FakeReader.GetArchivedWorkflow(ctx, uid)
}

func (r *archiveRecorder) ListArchivedWorkflows(ctx context.Context, q core.ArchiveQuery) (core.ArchivePage, error) {
	r.mu.Lock()
	r.archQueries = append(r.archQueries, q)
	r.mu.Unlock()
	return r.FakeReader.ListArchivedWorkflows(ctx, q)
}

func archiveRoot(t *testing.T, tune func(*testkit.FakeReader)) (*Root, *archiveRecorder) {
	t.Helper()
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	if tune != nil {
		tune(f)
	}
	rec := &archiveRecorder{FakeReader: f}
	m := NewRoot(rec, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	deliver(m, m.startListGeneration())
	runLine(m, "aw")
	if m.route != RouteArchived {
		t.Fatalf("route = %v after :aw", m.route)
	}
	return m, rec
}

func archCursor(m *Root) string {
	w, _ := m.archView.Selected()
	return w.Summary.Ref.Name
}

// :archived and :aw list the namespace's archive with the workflow list's
// columns, newest first, asked for with the namespace field selector.
func TestArchivedList(t *testing.T) {
	m, rec := archiveRoot(t, nil)
	v := screen(m)
	for _, want := range []string{"Archived workflows", "6 archived workflows", "PHASE", "DURATION", "MESSAGE",
		"demo-deploy-multi-layer-q8w1c", "child 'apply' failed"} {
		if !strings.Contains(v, want) {
			t.Fatalf("screen lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "demo-ml-hparam-sweep-7tq4n") {
		t.Fatal("another namespace listed")
	}
	if q := rec.archQueries[len(rec.archQueries)-1]; q.Namespace != "demo" || q.Limit != archivePageSize {
		t.Fatalf("query = %+v", q)
	}
	if got := archCursor(m); !strings.HasPrefix(got, "demo-etl-hourly-") {
		t.Fatalf("newest first put %q on top", got)
	}
	runLine(m, "archived")
	if m.route != RouteArchived {
		t.Fatal(":archived did not show the list")
	}
}

// enter reads the run from the archive by UID and shows it as archived; esc
// returns to the archive list with the cursor in place; the detail is not
// polled, and actions are refused.
func TestArchivedDetail(t *testing.T) {
	m, rec := archiveRoot(t, nil)
	typeKeys(m, "j")
	want := archCursor(m)
	deliver(m, pressKey(m, tea.KeyEnter))
	if m.route != RouteDetail || !m.detailState.archived || m.selection.Name != want {
		t.Fatalf("enter: route %v archived %v selection %v", m.route, m.detailState.archived, m.selection)
	}
	if rec.archGets != 1 || rec.liveGets != 0 {
		t.Fatalf("reads: archive %d live %d", rec.archGets, rec.liveGets)
	}
	v := screen(m)
	for _, s := range []string{"Detail " + want + " (archived)", "source:    the workflow archive", "phase=Failed (archived)"} {
		if !strings.Contains(v, s) {
			t.Fatalf("detail lacks %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, "a actions") {
		t.Fatalf("an archived detail offers actions:\n%s", v)
	}

	_, cmd := m.Update(tickMsg{})
	deliver(m, cmd)
	if rec.archGets != 1 {
		t.Fatalf("the tick refetched an archived run (%d reads)", rec.archGets)
	}

	typeKeys(m, "a")
	if m.actionView.State() != actions.StateUnavailable {
		t.Fatalf("a on an archived run: state %v", m.actionView.State())
	}
	if !strings.Contains(screen(m), "this workflow is archived") {
		t.Fatalf("no reason given:\n%s", screen(m))
	}
	pressKey(m, tea.KeyEscape)

	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteArchived || archCursor(m) != want {
		t.Fatalf("esc: route %v cursor %q", m.route, archCursor(m))
	}
	// A live workflow opened afterwards is read from the live route and is
	// not marked archived.
	runLine(m, "wf")
	deliver(m, pressKey(m, tea.KeyEnter))
	if m.detailState.archived || m.detailView.Archived() || rec.liveGets != 1 {
		t.Fatalf("live detail: archived %v view %v live reads %d", m.detailState.archived, m.detailView.Archived(), rec.liveGets)
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteList {
		t.Fatalf("esc from a live detail went to %v", m.route)
	}
}

// Logs of an archived run that arrive empty say why; a failed stream says
// the likely reason beside its own; logs that were archived show as usual.
func TestArchivedLogs(t *testing.T) {
	m, _ := archiveRoot(t, nil)
	deliver(m, pressKey(m, tea.KeyEnter))
	ref := m.selection
	deliver(m, m.openLogs(OpenLogsMsg{Ref: ref, PodName: "gone-pod", Container: "main"}))
	if !m.logState.archived {
		t.Fatal("logs of an archived run not marked")
	}
	body := strings.Join(m.logsView.BodyLines(), "\n")
	if !strings.Contains(body, "no log lines: this workflow is archived: its pods are usually gone") {
		t.Fatalf("empty archived stream:\n%s", body)
	}

	m.handleLogRecord(logRecordMsg{genStamp: genStamp{Conn: m.connGen, Sel: m.selGen}, RequestID: m.logState.streamID,
		Done: true, Err: core.ErrNotFoundf(`pods "gone-pod" not found`)})
	if m.logsView.Phase() != logs.PhaseError || !strings.Contains(strings.Join(m.logsView.BodyLines(), "\n"), `pods "gone-pod" not found — this workflow is archived`) {
		t.Fatalf("failed archived stream:\n%s", strings.Join(m.logsView.BodyLines(), "\n"))
	}

	// The nightly report archived its logs.
	deliver(m, pressKey(m, tea.KeyEscape))
	deliver(m, pressKey(m, tea.KeyEscape))
	for !strings.HasPrefix(archCursor(m), "demo-nightly-report-") {
		typeKeys(m, "j")
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	deliver(m, m.openLogs(OpenLogsMsg{Ref: m.selection}))
	body = strings.Join(m.logsView.BodyLines(), "\n")
	if strings.Contains(body, "no log lines") || strings.Contains(body, "retained: 0/") {
		t.Fatalf("archived logs:\n%s", body)
	}

	// Logs of a live workflow never carry the note.
	runLine(m, "wf")
	deliver(m, m.openLogs(OpenLogsMsg{Ref: core.Ref{Namespace: "demo", Name: "demo-cleanup", UID: "synthetic-uid-demo-cleanup"}}))
	if m.logState.archived || strings.Contains(strings.Join(m.logsView.BodyLines(), "\n"), "archived") {
		t.Fatal("a live stream carried the archive note")
	}
}

// A server without an archive: the list says the archive is not enabled
// rather than dumping the error, and so does the detail.
func TestArchiveNotEnabled(t *testing.T) {
	disabled := &core.APIError{Kind: core.ErrUnsupported, Status: 500,
		Message: core.ArchiveDisabledMessage + " (the server said: getting archived workflows not supported)"}
	m, _ := archiveRoot(t, func(f *testkit.FakeReader) { f.ArchiveErr = disabled })
	v := screen(m)
	if !strings.Contains(v, "no archived workflows on this server") || !strings.Contains(v, core.ArchiveDisabledMessage) {
		t.Fatalf("disabled archive list:\n%s", v)
	}

	m, rec := archiveRoot(t, nil)
	rec.ArchiveErr = disabled
	deliver(m, pressKey(m, tea.KeyEnter))
	if v := screen(m); !strings.Contains(v, core.ArchiveDisabledMessage) {
		t.Fatalf("disabled archive detail:\n%s", v)
	}
}

// A large archive is read to the cap, newest first, and the pane says it
// holds only the newest runs.
func TestArchiveCap(t *testing.T) {
	m, rec := archiveRoot(t, func(f *testkit.FakeReader) {
		var runs []core.Workflow
		for i := 0; i < archiveCap+50; i++ {
			wf := testkit.SyntheticWorkflow("demo", fmt.Sprintf("old-%04d", i), "Succeeded", testkit.FixtureEpoch.Add(-time.Duration(i+1)*time.Hour))
			runs = append(runs, wf)
		}
		f.ArchivedWorkflows = runs
	})
	if n := m.archView.Len(); n != archiveCap {
		t.Fatalf("collected %d, want %d", n, archiveCap)
	}
	if len(rec.archQueries) != archiveCap/archivePageSize {
		t.Fatalf("pages asked = %d", len(rec.archQueries))
	}
	if !strings.Contains(screen(m), "newest 300 only; the archive holds more") {
		t.Fatalf("no cap note:\n%s", screen(m))
	}
}

// 0 lists the whole archive; o links to the run's archived page by UID,
// from the list and from the detail.
func TestArchiveAllNamespacesAndLink(t *testing.T) {
	m, rec := archiveRoot(t, nil)
	m.SetWebURL("https://argo.example")
	uid := func() string { w, _ := m.archView.Selected(); return w.Summary.Ref.UID }()
	if got := m.workflowURL(); got != "https://argo.example/archived-workflows/demo/"+uid {
		t.Fatalf("list url = %q", got)
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	if got := m.workflowURL(); got != "https://argo.example/archived-workflows/demo/"+uid {
		t.Fatalf("detail url = %q", got)
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	deliver(m, typeKeys(m, "0"))
	if q := rec.archQueries[len(rec.archQueries)-1]; q.Namespace != "" {
		t.Fatalf("all namespaces asked %q", q.Namespace)
	}
	if m.route != RouteArchived || !strings.Contains(screen(m), "demo-ml-hparam-sweep-7tq4n") {
		t.Fatalf("all namespaces:\n%s", screen(m))
	}
}
