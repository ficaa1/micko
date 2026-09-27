package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
)

// queryRecorder is the demo reader with every workflow list query recorded,
// so a test can see what the drill-down asked the server for.
type queryRecorder struct {
	*testkit.FakeReader
	mu      sync.Mutex
	queries []core.Query
}

func (r *queryRecorder) List(ctx context.Context, q core.Query) (core.Page, error) {
	r.mu.Lock()
	r.queries = append(r.queries, q)
	r.mu.Unlock()
	return r.FakeReader.List(ctx, q)
}

func (r *queryRecorder) last() core.Query {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queries) == 0 {
		return core.Query{}
	}
	return r.queries[len(r.queries)-1]
}

// cronRoot is a demo root on the cron route with its list loaded.
func cronRoot(t *testing.T) (*Root, *queryRecorder) {
	t.Helper()
	rec := &queryRecorder{FakeReader: testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))}
	m := NewRoot(rec, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	deliver(m, m.startListGeneration())
	runLine(m, "cron")
	if m.route != RouteCron {
		t.Fatalf("route = %v after :cron", m.route)
	}
	return m, rec
}

func cronCursor(m *Root) string {
	_, name := m.cronView.SelectedName()
	return name
}

// :cron, :cwf and :cronworkflows show the cron list of the session's
// namespace, from any route.
func TestCronCommandShowsTheList(t *testing.T) {
	for _, word := range []string{"cron", "cwf", "cronworkflows"} {
		m, _ := demoRoot(t)
		deliver(m, m.openWorkflow(m.listView.SelectedRef()))
		runLine(m, word)
		if m.route != RouteCron {
			t.Fatalf(":%s: route %v", word, m.route)
		}
		v := screen(m)
		for _, want := range []string{"Cron workflows", "3 cron workflows", "demo-etl-hourly", "demo-weekly-compaction", "NEXT RUN"} {
			if !strings.Contains(v, want) {
				t.Fatalf(":%s: screen lacks %q:\n%s", word, want, v)
			}
		}
		if strings.Contains(v, "demo-ml-retrain") {
			t.Fatalf(":%s listed another namespace", word)
		}
	}
}

// The cron list refreshes on the list's tick while it is the active route.
func TestCronPolledOnTheTick(t *testing.T) {
	m, rec := cronRoot(t)
	before := rec.KindCalls
	_, cmd := m.Update(tickMsg{})
	deliver(m, cmd)
	if rec.KindCalls != before+1 {
		t.Fatalf("tick on the cron route listed cron workflows %d times, want 1", rec.KindCalls-before)
	}
	// Off the route the cron list is not polled.
	runLine(m, "wf")
	before = rec.KindCalls
	_, cmd = m.Update(tickMsg{})
	deliver(m, cmd)
	if rec.KindCalls != before {
		t.Fatal("the cron list was polled from the workflow list")
	}
}

// enter lists the row's runs through a server-side label selector in the
// cron workflow's namespace, and the title says what the list is narrowed to.
func TestCronDrillDown(t *testing.T) {
	m, rec := cronRoot(t)
	if got := cronCursor(m); got != "demo-etl-hourly" {
		t.Fatalf("cursor on %q, want the next run first", got)
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	if m.route != RouteList || m.drill == nil {
		t.Fatalf("enter: route %v drill %v", m.route, m.drill)
	}
	q := rec.last()
	if q.LabelSelector != "workflows.argoproj.io/cron-workflow=demo-etl-hourly" || q.Namespace != "demo" {
		t.Fatalf("list query = %+v", q)
	}
	v := screen(m)
	if !strings.Contains(v, "Workflows ← cron demo-etl-hourly") || !strings.Contains(v, "list: 3 workflows") {
		t.Fatalf("drill-down screen:\n%s", v)
	}
	for _, r := range m.listView.Rows() {
		if !strings.HasPrefix(r.Ref.Name, "demo-etl-hourly-") {
			t.Fatalf("drill-down listed %s", r.Ref.Name)
		}
	}
}

// esc returns to the cron list with the cursor where it was, and the next
// workflow list is the plain one again.
func TestCronDrillEscReturns(t *testing.T) {
	m, rec := cronRoot(t)
	typeKeys(m, "j")
	want := cronCursor(m)
	deliver(m, pressKey(m, tea.KeyEnter))
	if !strings.Contains(screen(m), "no workflows in this namespace yet") {
		t.Fatalf("a cron with no runs:\n%s", screen(m))
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteCron || m.drill != nil {
		t.Fatalf("esc: route %v drill %v", m.route, m.drill)
	}
	if got := cronCursor(m); got != want {
		t.Fatalf("cursor on %q after esc, want %q", got, want)
	}
	runLine(m, "wf")
	if q := rec.last(); q.LabelSelector != "" {
		t.Fatalf("the plain list still sends selector %q", q.LabelSelector)
	}
	if n := len(m.listView.Rows()); n != 12 {
		t.Fatalf("plain list shows %d workflows, want 12", n)
	}
}

// A filter on the drilled list clears before esc leaves it.
func TestCronDrillEscClearsFilterFirst(t *testing.T) {
	m, _ := cronRoot(t)
	deliver(m, pressKey(m, tea.KeyEnter))
	typeKeys(m, "/179")
	deliver(m, pressKey(m, tea.KeyEnter))
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteList || m.listView.Query() != "" {
		t.Fatalf("first esc: route %v query %q", m.route, m.listView.Query())
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteCron {
		t.Fatalf("second esc: route %v", m.route)
	}
}

// A workflow opened from the drill-down: esc twice lands on the cron list.
func TestCronDrillDetailEscTwice(t *testing.T) {
	m, _ := cronRoot(t)
	deliver(m, pressKey(m, tea.KeyEnter))
	deliver(m, pressKey(m, tea.KeyEnter))
	if m.route != RouteDetail || !strings.HasPrefix(m.selection.Name, "demo-etl-hourly-") {
		t.Fatalf("enter on the drill-down: route %v selection %v", m.route, m.selection)
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteList || m.drill == nil {
		t.Fatalf("first esc: route %v drill %v", m.route, m.drill)
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if m.route != RouteCron {
		t.Fatalf("second esc: route %v", m.route)
	}
}

// 0 on the cron route lists every namespace and stays on the route; a
// drill-down from a row in another namespace asks that namespace, and the
// header says so until esc.
func TestCronAllNamespaces(t *testing.T) {
	m, rec := cronRoot(t)
	deliver(m, typeKeys(m, "0"))
	if m.route != RouteCron || !m.deps.allNamespaces {
		t.Fatalf("0: route %v all %v", m.route, m.deps.allNamespaces)
	}
	v := screen(m)
	if !strings.Contains(v, "NAMESPACE") || !strings.Contains(v, "demo-ml-retrain") || !strings.Contains(v, "ns: all") {
		t.Fatalf("all-namespaces cron list:\n%s", v)
	}
	for cronCursor(m) != "demo-ml-retrain" {
		typeKeys(m, "j")
	}
	deliver(m, pressKey(m, tea.KeyEnter))
	if q := rec.last(); q.Namespace != "demo-ml" {
		t.Fatalf("drill from demo-ml asked namespace %q", q.Namespace)
	}
	if !strings.Contains(screen(m), "ns: demo-ml") {
		t.Fatalf("header does not name the owner's namespace:\n%s", screen(m))
	}
	deliver(m, pressKey(m, tea.KeyEscape))
	if !strings.Contains(screen(m), "ns: all") {
		t.Fatalf("header after esc:\n%s", screen(m))
	}
}

// A namespace switch from a drilled list ends the drill-down.
func TestNamespaceSwitchEndsDrill(t *testing.T) {
	m, rec := cronRoot(t)
	deliver(m, pressKey(m, tea.KeyEnter))
	deliver(m, m.switchNamespace("demo-ml"))
	if m.drill != nil || m.route != RouteList || m.deps.labelSelector != "" {
		t.Fatalf("switch kept the drill-down: route %v drill %v", m.route, m.drill)
	}
	if q := rec.last(); q.LabelSelector != "" || q.Namespace != "demo-ml" {
		t.Fatalf("list after the switch = %+v", q)
	}
}

// A reply for the namespace the reader has left is discarded.
func TestCronStaleReplyDiscarded(t *testing.T) {
	m, _ := cronRoot(t)
	old := m.startKindFetch(RouteCron)
	deliver(m, m.switchNamespace("demo-ml"))
	if m.route != RouteCron {
		t.Fatalf("switch left the cron route: %v", m.route)
	}
	deliver(m, old)
	rows := m.cronView.Rows()
	if len(rows) != 1 || rows[0].Namespace != "demo-ml" {
		t.Fatalf("rows after a stale reply: %+v", rows)
	}
}

// A refusal shows as one on the pane; a connection with no cron lister says
// it cannot list them.
func TestCronErrors(t *testing.T) {
	f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	f.CronErr = core.ErrForbiddenf(`cronworkflows.argoproj.io is forbidden: cannot list resource "cronworkflows"`)
	m := NewRoot(f, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runLine(m, "cron")
	v := screen(m)
	if !strings.Contains(v, "list forbidden") || !strings.Contains(v, "cannot list resource") {
		t.Fatalf("403 on the cron list:\n%s", v)
	}

	f.CronErr = core.NewAPIError(core.ErrNotFound, 404, "Not Found")
	m = NewRoot(f, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runLine(m, "cron")
	if v := screen(m); !strings.Contains(v, "no cron workflows on this server") || !strings.Contains(v, "HTTP 404") {
		t.Fatalf("404 on the cron list:\n%s", v)
	}

	plain := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{}}
	m = NewRoot(onlyReader{plain}, testkit.NewFakeClock(testkit.FixtureEpoch), "demo", time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runLine(m, "cron")
	if v := screen(m); !strings.Contains(v, "cannot list cron workflows") {
		t.Fatalf("no lister:\n%s", v)
	}
}

// onlyReader hides every optional interface of the reader it wraps.
type onlyReader struct{ r core.Reader }

func (o onlyReader) List(ctx context.Context, q core.Query) (core.Page, error) {
	return o.r.List(ctx, q)
}
func (o onlyReader) Get(ctx context.Context, ref core.Ref) (core.Workflow, error) {
	return o.r.Get(ctx, ref)
}
func (o onlyReader) StreamLogs(ctx context.Context, req core.LogRequest, cb func(core.LogRecord) error) error {
	return o.r.StreamLogs(ctx, req, cb)
}

// f shows the selected cron workflow's manifest with its values; under
// redactValues it is redacted and v on the row reveals it; y copies the name.
func TestCronRawManifest(t *testing.T) {
	m, _ := cronRoot(t)
	typeKeys(m, "f")
	if raw := strings.Join(m.rawLines(), "\n"); !strings.Contains(raw, "demo-token-not-a-real-secret") {
		t.Fatalf("values hidden by default:\n%s", raw)
	}
	typeKeys(m, "f")
	m.SetRedactValues(true)
	typeKeys(m, "f")
	raw := strings.Join(m.rawLines(), "\n")
	if !strings.Contains(raw, "kind: CronWorkflow") || !strings.Contains(raw, "name: demo-etl-hourly") {
		t.Fatalf("raw view:\n%s", raw)
	}
	if strings.Contains(raw, "demo-token-not-a-real-secret") || !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("raw view not redacted:\n%s", raw)
	}
	typeKeys(m, "f")
	typeKeys(m, "v")
	if raw := strings.Join(m.rawLines(), "\n"); !strings.Contains(raw, "demo-token-not-a-real-secret") {
		t.Fatalf("v did not reveal the manifest:\n%s", raw)
	}
	if got := m.copyText(); got != "demo-etl-hourly" {
		t.Fatalf("copy = %q", got)
	}
}

// o opens the cron workflow's page in the Argo UI.
func TestCronOpenInBrowser(t *testing.T) {
	m, _ := cronRoot(t)
	m.SetWebURL("https://argo.example/")
	if got := m.workflowURL(); got != "https://argo.example/cron-workflows/demo/demo-etl-hourly" {
		t.Fatalf("url = %q", got)
	}
}

// Keys typed into the cron filter are letters, not commands.
func TestCronFilterIsolation(t *testing.T) {
	m, _ := cronRoot(t)
	typeKeys(m, "/q0n")
	if m.quitting || m.route != RouteCron || m.deps.allNamespaces || m.namespaceDialogOpen() {
		t.Fatal("a key typed into the filter acted as a command")
	}
	if n := len(m.cronView.Rows()); n != 0 {
		t.Fatalf("filter q0n matched %d rows", n)
	}
}
