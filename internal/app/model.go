package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/actions"
	"argo-tui/internal/ui/detail"
	"argo-tui/internal/ui/logs"
	"argo-tui/internal/ui/shared"
	"argo-tui/internal/ui/workflowlist"
)

// Root is the top-level Tea model: it owns routing, generations,
// cancellation and the update loop that alone mutates UI state (plan §4).
// F1 implements empty routes without worker feature logic; I1 composes the
// real views into it.
type Root struct {
	deps deps

	// route is the active top-level route.
	route Route

	// connGen bumps on namespace/profile switch; selGen bumps on selected
	// workflow change (plan §4 response stamping).
	connGen, selGen int

	// selection is the currently selected workflow (detail/logs scope).
	selection core.Ref

	// inflight cancels currently running commands, keyed by purpose.
	mu       sync.Mutex
	inflight map[string]context.CancelFunc

	// listState is the collected snapshot for the list route.
	listState listState

	// detailState is the loaded detail for the detail route.
	detailState detailState

	// logState is the retained log state for the logs route.
	logState logState

	// size is the last known terminal size.
	width, height int

	// quitting marks the exit path.
	quitting bool

	ids requestIDProvider

	listView   workflowlist.Model
	detailView *detail.Model
	logsView   *logs.Model
	actionView *actions.Model
	actionOpts actions.Options
	watchRV    string
	watchMode  string
}

// listState is the list route's data + status.
type listState struct {
	// items is the last complete (or capped/incomplete) snapshot.
	items []core.Summary
	// loading is true while a snapshot collection runs for this generation.
	loading bool
	// incomplete marks a capped collection (plan §5: visible incomplete state).
	incomplete bool
	// staleSince is when the last successful snapshot finished; errors keep
	// last good data and show stale age (plan §5).
	staleSince time.Time
	// lastErr is the last error for the list (kept with last good data).
	lastErr *core.APIError
}

// detailState is the detail route's data + status.
type detailState struct {
	ref      core.Ref
	workflow core.Workflow
	loading  bool
	lastErr  *core.APIError
	notFound bool
}

// logState is the logs route's data + status.
type logState struct {
	ref       core.Ref
	container string
	records   []core.LogRecord
	running   bool
	lastErr   error
	canceled  bool
}

// NewRoot constructs the root model.
func NewRoot(r core.Reader, clock Clock, namespace string, interval time.Duration) *Root {
	return NewRootWithOptions(r, clock, namespace, interval, actions.Options{ReadOnly: true})
}

// NewRootWithOptions wires optional beta capabilities while retaining the
// alpha-safe NewRoot constructor.
func NewRootWithOptions(r core.Reader, clock Clock, namespace string, interval time.Duration, opts actions.Options) *Root {
	var watcher core.Watcher
	if w, ok := r.(core.Watcher); ok {
		watcher = w
	}
	var actioner core.Actioner
	if a, ok := r.(core.Actioner); ok {
		actioner = a
	}
	return &Root{
		deps: deps{
			reader:      r,
			watcher:     watcher,
			actioner:    actioner,
			clock:       clock,
			interval:    interval,
			namespace:   namespace,
			snapshotCap: 5000,
			pageSize:    100,
		},
		inflight:   map[string]context.CancelFunc{},
		listView:   workflowlist.New(shared.NewTheme(false), false),
		detailView: detail.New(),
		actionView: actions.NewWithOptions(core.Ref{}, opts),
		actionOpts: opts,
	}
}

// compile-time interface checks.
var _ tea.Model = (*Root)(nil)

// Init implements tea.Model. It starts the first list collection.
func (m *Root) Init() tea.Cmd {
	return m.startListGeneration()
}

// Update implements tea.Model. All state mutation happens here; async
// commands only produce messages (plan §4).
func (m *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if m.actionView != nil && m.actionView.State() != actions.StateIdle {
			_, cmd := m.actionView.Update(msg)
			return m, cmd
		}
		if m.route == RouteDetail && msg.String() == "a" {
			m.actionView = actions.NewWithOptions(m.selection, m.actionOpts)
			m.actionView.OpenMenu()
			return m, nil
		}
		if msg.String() == "esc" && (m.route == RouteDetail || (m.route == RouteLogs && (m.logsView == nil || !m.logsView.EscapeConsumed()))) {
			return m, m.back()
		}
		if m.route != RouteList && msg.String() != "ctrl+c" {
			return m, m.updateChild(msg)
		}
		return m.handleKey(msg)

	case tea.QuitMsg:
		m.quitting = true
		return m, nil

	case tickMsg:
		// Only restart a poll when one is not already in flight for this
		// generation (plan §5: exactly one list op per generation).
		if m.route == RouteList && !m.listState.loading {
			return m, m.startListGeneration()
		}
		return m, nil

	case listLoadedMsg:
		return m, m.handleListLoaded(msg)

	case watchEventMsg:
		return m, m.handleWatchEvent(msg)

	case watchDoneMsg:
		return m, m.handleWatchDone(msg)

	case actionResultMsg:
		return m, m.handleActionResult(msg)

	case detailLoadedMsg:
		return m, m.handleDetailLoaded(msg)

	case logRecordMsg:
		return m, m.handleLogRecord(msg)

	case OpenWorkflowMsg:
		return m, m.openWorkflow(msg.Ref)

	case OpenLogsMsg:
		return m, m.openLogs(msg)

	case shared.SwitchContextIntent:
		return m, m.openLogs(OpenLogsMsg{Ref: msg.Ref, PodName: msg.PodName, Container: msg.Container})

	case detail.BackMsg:
		return m, m.back()

	case BackMsg:
		return m, m.back()

	case ActionIntentMsg:
		// F1: alpha/demo never converts action intents to writes (plan §6).
		// No effect; I1 will gate this behind --allow-actions.
		return m, nil

	default:
		return m, nil
	}
}

// View implements tea.Model. F1 renders a minimal placeholder per route
// (the plan gate: "children have a compiling baseline and one working
// fake-driven root route"; do not call this stub a finished alpha).
func (m *Root) View() tea.View {
	var b strings.Builder
	b.WriteString("argo-tui | ns: " + m.deps.namespace + " | READ ONLY\n")
	switch m.route {
	case RouteDetail:
		b.WriteString("route: detail | " + m.detailSummary() + "\n")
		if m.detailView != nil {
			b.WriteString(m.detailView.View().Content)
		}
	case RouteLogs:
		b.WriteString("route: logs | " + m.logSummary() + "\n")
		if m.logsView != nil {
			b.WriteString(m.logsView.View())
		}
	default:
		b.WriteString("route: list | " + m.listSummary() + "\n")
		b.WriteString(m.listView.View())
	}
	return tea.NewView(b.String())
}

func (m *Root) listSummary() string {
	st := m.listState
	switch {
	case st.loading && len(st.items) == 0:
		return "list: loading..."
	case st.lastErr != nil && len(st.items) == 0:
		return "list: error: " + st.lastErr.Message
	case st.lastErr != nil:
		return "list: stale (last good " + lenItems(st.items) + "): " + st.lastErr.Message
	case len(st.items) == 0:
		return "list: empty"
	default:
		s := "list: " + lenItems(st.items)
		if m.watchMode != "" {
			s += " | mode: " + m.watchMode
		}
		if st.incomplete {
			s += " (incomplete: snapshot cap reached)"
		}
		return s
	}
}

func lenItems(items []core.Summary) string {
	return plural(len(items), "workflow")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (m *Root) detailSummary() string {
	st := m.detailState
	switch {
	case st.loading:
		return "loading detail for " + st.ref.Name + "..."
	case st.notFound:
		return "workflow no longer available: " + st.ref.Name
	case st.lastErr != nil:
		return "detail error: " + st.lastErr.Message
	default:
		wf := st.workflow
		return "workflow " + wf.Summary.Ref.Name + " phase=" + wf.Summary.Phase
	}
}

func (m *Root) logSummary() string {
	st := m.logState
	switch {
	case st.running:
		return "streaming " + st.ref.Name + " (" + plural(len(st.records), "record") + ")..."
	case st.canceled:
		return "stream canceled"
	case st.lastErr != nil:
		return "stream error: " + st.lastErr.Error()
	default:
		return "stream ended (" + plural(len(st.records), "record") + ")"
	}
}

// key handling ---------------------------------------------------------------

func (m *Root) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if shared.QuitKeySet(key == "ctrl+c", key, shared.KeyCtxBrowsing) {
		m.cancelAll()
		m.quitting = true
		return m, tea.Quit
	}
	switch key {
	case "q":
		// browsing context only (QuitKeySet already gated ctrl+c); F1 root
		// has no text entry, so q quits.
		m.cancelAll()
		m.quitting = true
		return m, tea.Quit
	case "esc":
		return m, m.back()
	case "n":
		// F1: namespace switching is I1 scope; stub does nothing yet.
		return m, nil
	default:
		return m, nil
	}
}

// back implements Esc semantics: logs -> detail -> list.
func (m *Root) back() tea.Cmd {
	switch m.route {
	case RouteLogs:
		// Leaving logs cancels the stream promptly (plan §5; STR-02).
		m.cancelInflight("logs")
		m.logState.running = false
		m.route = RouteDetail
		return nil
	case RouteDetail:
		m.cancelInflight("detail")
		m.detailState.loading = false
		m.route = RouteList
		return nil
	default:
		return nil
	}
}

// openWorkflow handles the list→detail intent.
func (m *Root) openWorkflow(ref core.Ref) tea.Cmd {
	// Same workflow re-selected: no generation bump.
	if m.route == RouteDetail && m.selection == ref {
		return nil
	}
	m.selection = ref
	m.selGen++
	m.route = RouteDetail
	m.detailState = detailState{ref: ref, loading: true}
	m.detailView.SetLoading()
	return m.startDetailFetch()
}

// openLogs handles the logs intent.
func (m *Root) openLogs(msg OpenLogsMsg) tea.Cmd {
	if msg.Container == "" {
		msg.Container = "main"
	}
	// Re-opening logs for the same ref+container while running: ignore.
	if m.route == RouteLogs && m.logState.running && m.logState.ref == msg.Ref && m.logState.container == msg.Container {
		return nil
	}
	m.selection = msg.Ref
	m.selGen++
	m.route = RouteLogs
	m.logState = logState{ref: msg.Ref, container: msg.Container, running: true}
	m.logsView = logs.NewModel(msg.Ref, msg.PodName, msg.Container)
	return m.startLogStream(msg)
}

// startListGeneration cancels any prior list, bumps nothing (list runs per
// connection generation), and starts a fresh single-flight collection.
func (m *Root) startListGeneration() tea.Cmd {
	m.cancelInflight("list")
	m.listState.loading = true
	ctx, cancel := context.WithCancel(context.Background())
	m.setInflight("list", cancel)
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	cmd := m.deps.listCmd(ctx, g, id)
	return cmd
}

// startDetailFetch starts a detail fetch for the current selection.
func (m *Root) startDetailFetch() tea.Cmd {
	m.cancelInflight("detail")
	ctx, cancel := context.WithCancel(context.Background())
	m.setInflight("detail", cancel)
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	return m.deps.detailCmd(ctx, g, id, m.selection)
}

// startLogStream starts a log stream for msg.
func (m *Root) startLogStream(msg OpenLogsMsg) tea.Cmd {
	m.cancelInflight("logs")
	ctx, cancel := context.WithCancel(context.Background())
	m.setInflight("logs", cancel)
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	req := core.LogRequest{
		Ref:       msg.Ref,
		PodName:   msg.PodName,
		Container: msg.Container,
	}
	return m.deps.streamLogsCmd(ctx, g, id, req)
}

// handleListLoaded applies a list result honoring generation/request
// staleness (plan §4: ignore stale completions even after cancellation).
func (m *Root) handleListLoaded(msg listLoadedMsg) tea.Cmd {
	m.clearInflight("list")
	if msg.Canceled {
		// Canceled collections are always stale by construction.
		return nil
	}
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale: discard
	}
	st := &m.listState
	st.loading = false
	if msg.Err != nil {
		// Keep last good data; record error + stale age (plan §5).
		st.lastErr = msg.Err
		if st.staleSince.IsZero() {
			st.staleSince = m.deps.clock.Now()
		}
		m.listView.SetStatus(workflowlist.StatusStale, msg.Err.Message, m.deps.clock.Now().Sub(st.staleSince))
		return m.deps.tickCmd()
	}
	st.items = msg.Page.Items
	m.watchRV = msg.Page.ResourceVersion
	st.incomplete = msg.Capped
	st.lastErr = nil
	st.staleSince = time.Time{}
	m.listView.SetItems(st.items, m.deps.clock.Now())
	status := workflowlist.StatusIdle
	if st.incomplete {
		status = workflowlist.StatusIncomplete
	}
	m.listView.SetStatus(status, "", 0)
	if m.deps.watcher != nil {
		return tea.Batch(m.startWatch(), m.deps.tickCmd())
	}
	return m.deps.tickCmd()
}

// handleDetailLoaded applies a detail result honoring staleness + UID
// validation (same-name replacement is a different workflow, LIST-12).
func (m *Root) handleDetailLoaded(msg detailLoadedMsg) tea.Cmd {
	m.clearInflight("detail")
	if msg.Canceled {
		return nil
	}
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale
	}
	st := &m.detailState
	if msg.Err != nil {
		if msg.Err.Kind == core.ErrNotFound {
			st.notFound = true
			st.lastErr = nil
			st.loading = false
			m.detailView.SetNotFound()
			return nil
		}
		st.lastErr = msg.Err
		st.loading = false
		m.detailView.SetError(msg.Err.Message)
		return nil
	}
	// UID check: response must match the selection that requested it.
	if msg.Workflow.Summary.Ref.UID != msg.Ref.UID {
		st.lastErr = core.NewAPIError(core.ErrNotFound, 404,
			"workflow changed: selected UID no longer matches (workflow replaced?)")
		st.loading = false
		return nil
	}
	st.workflow = msg.Workflow
	st.loading = false
	st.lastErr = nil
	st.notFound = false
	m.detailView.SetWorkflow(msg.Workflow, m.deps.clock.Now())
	return nil
}

// handleLogRecord applies one batched delivery honoring staleness.
func (m *Root) handleLogRecord(msg logRecordMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale
	}
	st := &m.logState
	st.records = append(st.records, msg.Records...)
	if m.logsView != nil && len(msg.Records) > 0 {
		m.logsView.ApplyRecords(msg.Records)
	}
	if msg.Done {
		st.running = false
		st.lastErr = msg.Err
		st.canceled = msg.Canceled
		if m.logsView != nil {
			switch {
			case msg.Canceled:
				m.logsView.SetPhase(logs.PhaseCanceled)
			case msg.Err != nil:
				m.logsView.SetError(msg.Err.Error())
			default:
				m.logsView.SetPhase(logs.PhaseEnded)
			}
		}
		m.clearInflight("logs")
		return nil
	}
	// chain the next drain; the streamState rides in the closure
	return nil
}

// inflight bookkeeping --------------------------------------------------------

func (m *Root) updateChild(msg tea.Msg) tea.Cmd {
	switch m.route {
	case RouteList:
		return m.listView.Update(msg)
	case RouteDetail:
		_, cmd := m.detailView.Update(msg)
		return cmd
	case RouteLogs:
		if m.logsView == nil {
			return nil
		}
		return m.logsView.Update(msg)
	default:
		return nil
	}
}

func (m *Root) setInflight(purpose string, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.inflight[purpose]; ok {
		old()
	}
	m.inflight[purpose] = cancel
}

func (m *Root) cancelInflight(purpose string) {
	m.mu.Lock()
	cancel, ok := m.inflight[purpose]
	delete(m.inflight, purpose)
	m.mu.Unlock()
	if ok {
		cancel()
	}
}

func (m *Root) clearInflight(purpose string) {
	m.mu.Lock()
	delete(m.inflight, purpose)
	m.mu.Unlock()
}

func (m *Root) cancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.inflight))
	for _, c := range m.inflight {
		cancels = append(cancels, c)
	}
	m.inflight = map[string]context.CancelFunc{}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}
