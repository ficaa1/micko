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
	"argo-tui/internal/ui/shell"
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

	// logsFrom records the route the logs pane was opened from (list via
	// the 'l' key, or detail), so a single Esc returns to that route
	// instead of dumping the user onto a never-loaded/stale detail pane
	// (Q finding t_fa36e201). Zero value beats RouteList, which is the
	// common origin.
	logsFrom Route

	// size is the last known terminal size.
	width, height int

	// quitting marks the exit path.
	quitting bool

	ids requestIDProvider

	// help is the global `?` overlay. Every route footer advertises it, so
	// it is owned by the root rather than by any one child.
	help shared.HelpOverlay

	// theme styles the shell chrome. The children keep their own copies for
	// row-level styling; this one is only for the frame.
	theme shared.Theme

	// version is the build version shown in the context band.
	version string

	listView      workflowlist.Model
	detailView    *detail.Model
	logsView      *logs.Model
	actionView    *actions.Model
	actionOpts    actions.Options
	watchRV       string
	watchMode     string
	watchRetries  int
	watchAttempt  uint64
	actionAttempt uint64
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
		theme:      shared.NewTheme(false),
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
		m.resizeChildren(msg)
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()
		// Ctrl-C quits globally, including while a child modal owns input.
		if key == "ctrl+c" {
			return m.handleKey(msg)
		}
		// A non-idle action modal owns all remaining input (UI-04).
		if m.actionView != nil && m.actionView.State() != actions.StateIdle {
			_, cmd := m.actionView.Update(msg)
			return m, cmd
		}
		// The help overlay is a dialog (shared.KeyCtxDialog): while it is
		// open it owns every remaining key, so the view behind it cannot
		// move. q closes it instead of quitting; only Ctrl-C, handled
		// above, still quits globally.
		if m.help.IsOpen() {
			switch key {
			case "?", "esc", "q":
				m.help.Close()
			}
			return m, nil
		}
		// ? opens help everywhere except inside text entry, where it is an
		// ordinary printable character (UI-04 text-entry isolation).
		if key == "?" && !m.listView.SearchOn {
			m.help.Toggle()
			return m, nil
		}
		// q quits globally on every route (live-smoke: global q preserved),
		// gated on text-entry isolation: while the list search input is
		// focused, q types a letter and is consumed by the search handler
		// below instead.
		if key == "q" && !m.listView.SearchOn {
			return m, m.quit()
		}
		if m.route == RouteDetail && key == "a" {
			m.actionView = actions.NewWithOptions(m.selection, m.actionOpts)
			m.actionView.SetContext(m.actionOpts.Server, m.actionOpts.Profile, m.detailState.workflow.Summary.Phase)
			m.actionView.OpenMenu()
			return m, nil
		}
		if key == "esc" && (m.route == RouteDetail || (m.route == RouteLogs && (m.logsView == nil || !m.logsView.EscapeConsumed()))) {
			return m, m.back()
		}
		// List route: route the keyboard into the list child (j/k/arrows/Enter/
		// l//s/r and printable search text). The child emits intents (open,
		// logs, refresh); the root converts them to effects below. Global
		// keys (q, ctrl+c) stay at the root, and text-entry isolation means
		// printable letters — including q and r — reach the search buffer
		// while the search input is focused (UI-04).
		if m.route == RouteList {
			return m, m.updateChild(msg)
		}
		// Detail/logs routes: the active child consumes non-global keys.
		return m, m.updateChild(msg)

	case tea.QuitMsg:
		m.quitting = true
		return m, nil

	case tickMsg:
		// Only restart a poll when one is not already in flight for this
		// generation (plan §5: exactly one list op per generation).
		if m.route == RouteList && !m.listState.loading && !terminalWatchMode(m.watchMode) {
			return m, m.startListGeneration()
		}
		return m, nil

	case listLoadedMsg:
		return m, m.handleListLoaded(msg)

	case watchEventMsg:
		return m, m.handleWatchEvent(msg)

	case watchDoneMsg:
		return m, m.handleWatchDone(msg)

	case watchRetryMsg:
		if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.watchAttempt || m.watchMode != "rate limited" {
			return m, nil
		}
		return m, m.startWatch()

	case actionResultMsg:
		return m, m.handleActionResult(msg)

	case detailLoadedMsg:
		return m, m.handleDetailLoaded(msg)

	case logRecordMsg:
		return m, m.handleLogRecord(msg)

	case workflowlist.RefreshListMsg:
		// Manual refresh intent from the list child (r): an explicit
		// operator refresh clears terminal watch state (the escape hatch
		// from rate-limited/unauthorized states) and starts a fresh
		// collection of this generation.
		m.watchMode = ""
		m.watchRetries = 0
		m.cancelInflight("watch")
		return m, m.startListGeneration()

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

	case actions.ActionIntentMsg:
		// Only the active, confirmed child intent may reach the executor.
		if msg.Request.Ref != m.selection || m.actionView == nil ||
			m.actionView.State() != actions.StateSubmitting {
			return m, nil
		}
		return m, m.startAction(msg.Request)

	case ActionIntentMsg:
		// Retain the obsolete app message as a no-op compatibility boundary.
		return m, nil

	default:
		return m, nil
	}
}

func terminalWatchMode(mode string) bool {
	return mode == "authentication/permission required" || mode == "rate limited; refresh required"
}

// View implements tea.Model. Every route renders through the same shell:
// a context band, a bordered pane, and a key-hint band. The shell fixes the
// geometry, so the bands stay put while routes and connection states change.
func (m *Root) View() tea.View {
	f := shell.Frame{
		// `?` is root-owned on every route, so the shell advertises it once
		// rather than each pane repeating it in its own hints.
		Help:      "? help",
		Width:     m.width,
		Height:    m.height,
		App:       "argo-tui " + m.version,
		Server:    m.serverLabel(),
		Namespace: m.deps.namespace,
		Mode:      m.modeLabel(),
	}

	// An action modal is a dialog: it takes the pane so the route behind it
	// cannot be mistaken for the thing being confirmed.
	if m.actionView != nil && m.actionView.State() != actions.StateIdle {
		f.Title = "Confirm action"
		f.Route = "action"
		f.Hints = "y confirm  n cancel  esc close"
		f.Help = ""
		f.Body = splitLines(m.actionView.View().Content)
		return m.finishView(f)
	}
	if m.help.IsOpen() {
		f.Title = "Help"
		f.Route = "help"
		f.Help = "esc close"
		f.Body = splitLines(m.help.View(f.BodyWidth(), f.BodyHeight()))
		return m.finishView(f)
	}

	switch m.route {
	case RouteDetail:
		f.Title = m.detailPaneTitle()
		f.TitleRight = m.detailSummary()
		f.Route = "detail"
		if m.detailView != nil {
			m.detailView.SetSize(f.BodyWidth(), f.BodyHeight())
			f.Hints = m.detailView.Hints()
			f.Status = m.detailView.PaneStatus()
			f.Body = m.detailView.BodyLines()
		}
	case RouteLogs:
		f.Route = "logs"
		if m.logsView != nil {
			m.logsView.SetPaneMode(true)
			m.logsView.SetSize(f.BodyWidth(), f.BodyHeight())
			f.Title = m.logsView.PaneTitle()
			f.TitleRight = m.logSummary()
			f.Hints = m.logsView.Hints()
			f.Status = m.logsView.PaneStatus()
			f.Body = m.logsView.BodyLines()
		}
	default:
		m.listView.SetSize(f.BodyWidth(), f.BodyHeight())
		f.Title = m.listView.PaneTitle()
		f.TitleRight = m.listSummary()
		f.Route = "list"
		f.Hints = m.listView.Hints()
		// The list body must render before WindowStatus, which reports the
		// window that render chose.
		f.Body = m.listView.BodyLines(m.deps.clock.Now())
		f.Status = m.listStatusCell()
	}
	return m.finishView(f)
}

// splitLines turns a child's rendered block into frame body lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// modeLabel is the safety state: the single word that says whether this
// session can mutate anything. It is always shown.
func (m *Root) modeLabel() string {
	if m.actionOpts.AllowActions && !m.actionOpts.ReadOnly && !m.actionOpts.Demo {
		return "ACTIONS ENABLED"
	}
	return "READ ONLY"
}

// serverLabel names where the data comes from. The demo must never claim a
// server it does not have.
func (m *Root) serverLabel() string {
	if m.actionOpts.Demo {
		return "synthetic demo"
	}
	return m.actionOpts.Server
}

// listStatusCell is the right-aligned footer cell for the list: the watch
// mode and the visible row window.
func (m *Root) listStatusCell() string {
	parts := []string{}
	if m.watchMode != "" {
		parts = append(parts, m.watchMode)
	}
	if w := m.listView.WindowStatus(); w != "" {
		parts = append(parts, w)
	}
	return strings.Join(parts, " · ")
}

// detailPaneTitle names the pane, falling back to the route name before a
// workflow loads.
func (m *Root) detailPaneTitle() string {
	if m.detailView == nil {
		return "Detail"
	}
	return m.detailView.PaneTitle()
}

// SetVersion records the build version for the context band. main owns the
// version string; the root only displays it.
func (m *Root) SetVersion(v string) { m.version = v }

// finishView renders the frame and marks it as a full-window alternate-screen
// view.
//
// The alternate screen has no scrollback: a frame taller than the terminal
// does not scroll, it loses its bottom rows outright, and the bottom row is
// the footer with the key hints. shell.Render already produces exactly the
// terminal size, so the clamp here is a safety net for an unsized frame.
func (m *Root) finishView(f shell.Frame) tea.View {
	content := f.Render(m.theme)
	if m.height > 0 {
		lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
		content = strings.Join(shared.ClampLines(lines, m.height), "\n")
	}
	v := tea.NewView(content)
	// Full-window alternate screen (live-smoke defect 4b): the layout is
	// stable in the alternate buffer, and bubbletea restores the terminal on
	// every exit/error path (q, Ctrl-C, and any program teardown).
	v.AltScreen = true
	return v
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
		return m, m.quit()
	}
	switch key {
	case "esc":
		return m, m.back()
	case "n":
		// F1: namespace switching is I1 scope; stub does nothing yet.
		return m, nil
	default:
		return m, nil
	}
}

// back implements Esc semantics: logs -> (origin) -> list. The logs pane
// returns to the route it was opened from — the list when 'l' was pressed
// on the list, or the loaded detail when logs were entered from detail —
// so a single Esc never lands on a never-loaded or stale detail pane
// (Q finding t_fa36e201).
func (m *Root) back() tea.Cmd {
	switch m.route {
	case RouteLogs:
		// Leaving logs cancels the stream promptly (plan §5; STR-02).
		m.cancelInflight("logs")
		m.logState.running = false
		// logsFrom is set by openLogs; a zero value (RouteList) is the
		// safe fallback for a log view reached with no explicit origin.
		target := m.logsFrom
		if target != RouteDetail && target != RouteList {
			target = RouteList
		}
		m.route = target
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

// openLogs handles the logs intent. It records the route the logs pane is
// opened from (unless it is itself already the logs route — a pod/container
// context switch keeps the original origin), so a later single Esc returns
// to where the user came from rather than a never-loaded/stale detail pane.
func (m *Root) openLogs(msg OpenLogsMsg) tea.Cmd {
	if msg.Container == "" {
		msg.Container = "main"
	}
	// Re-opening logs for the same ref+container while running: ignore.
	if m.route == RouteLogs && m.logState.running && m.logState.ref == msg.Ref && m.logState.container == msg.Container {
		return nil
	}
	if m.route != RouteLogs {
		m.logsFrom = m.route
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
		switch msg.Err.Kind {
		case core.ErrUnauthenticated, core.ErrForbidden:
			m.watchMode = "authentication/permission required"
			return nil
		case core.ErrRateLimited:
			m.watchMode = "rate limited; refresh required"
			return nil
		}
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
	if terminalWatchMode(m.watchMode) {
		return nil
	}
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

// resizeChildren propagates a terminal resize to every child view model so
// the active route and the ones the user can switch to all re-lay out (live-
// smoke defect 4). The Tea event loop re-renders View after every Update, so
// mutating child sizes here is sufficient to trigger a responsive redraw.
func (m *Root) resizeChildren(msg tea.WindowSizeMsg) {
	m.listView.Update(msg)
	if m.detailView != nil {
		_, _ = m.detailView.Update(msg)
	}
	if m.logsView != nil {
		m.logsView.Update(msg)
	}
	if m.actionView != nil {
		_, _ = m.actionView.Update(msg)
	}
}

// quit performs the shared teardown for every exit path (q, ctrl+c): cancel
// all inflight work, mark quitting and return the Quit command so the
// renderer restores the terminal (alternate screen) on the way out.
func (m *Root) quit() tea.Cmd {
	m.cancelAll()
	m.quitting = true
	return tea.Quit
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
