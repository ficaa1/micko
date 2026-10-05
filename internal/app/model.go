package app

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/notify"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/archivedlist"
	"github.com/ficaa1/micko/internal/ui/detail"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/logs"
	"github.com/ficaa1/micko/internal/ui/namespaces"
	"github.com/ficaa1/micko/internal/ui/palette"
	"github.com/ficaa1/micko/internal/ui/profiles"
	"github.com/ficaa1/micko/internal/ui/shared"
	"github.com/ficaa1/micko/internal/ui/shell"
	"github.com/ficaa1/micko/internal/ui/workflowlist"
)

// Root is the top-level Tea model. Only its update loop changes UI state.
type Root struct {
	deps deps

	// route is the active top-level route.
	route Route

	// connGen bumps on a namespace or profile switch, selGen on a new selection.
	// Replies carry the generations they were requested under.
	connGen, selGen int

	// selection is the selected workflow, the scope of detail and logs.
	selection core.Ref

	// inflight holds the cancel function of each purpose's running command,
	// tagged with its request. A handler clears only its own entry, so a late
	// reply cannot retire the request that replaced it.
	mu       sync.Mutex
	inflight map[string]inflightOp
	spans    map[string]span

	// listState is the collected snapshot for the list route.
	listState listState

	// detailState is the loaded detail for the detail route.
	detailState detailState

	// logState is the retained log state for the logs route.
	logState logState

	// events streams Kubernetes events for the Events section.
	events eventsSession

	// logsFrom is the route the logs pane was opened from, which esc returns to.
	logsFrom Route

	// width and height are the last known terminal size.
	width, height int

	// quitting marks the exit path.
	quitting bool

	ids requestIDProvider

	// help is the global ? overlay, owned by the root because every footer
	// offers it.
	help shared.HelpOverlay

	// theme styles the shell chrome. The children keep their own copies for
	// row-level styling; SetTheme replaces all of them together.
	theme shared.Theme

	// skin is the skin name last applied. bgKnown and bgDark are the
	// terminal's reported background, which the auto skin chooses by.
	skin            string
	bgKnown, bgDark bool

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
	// bulk is the bulk action in progress, nil when none runs.
	bulk *bulkRun
	// actionFromMarks records that the open action pane acts on the marked
	// rows, so its finish can drop them.
	actionFromMarks bool
	// notify chooses which workflow changes notify the reader and how.
	// notifySend delivers one notice (nil means notify.Deliver); tests replace
	// it. notifyWarned records that a failed delivery was reported.
	notify       config.Notify
	notifySend   func(notify.Notice) error
	notifyWarned bool
	// watched holds the workflows watched with W, by UID, with the state
	// last seen. It outlives namespace switches; watchedGen names the
	// connection it belongs to, and watchedArmed that its check chain runs.
	watched      map[string]watch
	watchedGen   int
	watchedArmed bool
	// journalWarned records that a journal failure has been reported this
	// session.
	journalWarned bool
	// connectionReady gates mutations; data stays on screen while it is false.
	connectionReady  bool
	connectionFresh  bool
	connectionTarget string

	// rawMode renders the active pane's whole content with no border and no
	// bands, so a terminal mouse selection copies clean text. rawTop and
	// rawLeft are its scroll anchors; rawWrap cuts long lines at the screen
	// edge instead, and lasts the session.
	rawMode bool
	rawTop  int
	rawLeft int
	rawWrap bool

	// tickArmed guards the single poll chain, which every route re-arms. Two
	// live chains would double the poll rate.
	tickArmed bool
	// gPending is the armed half of vim's gg while the raw view is up.
	gPending bool

	// flash is the footer report of the last explicit command, cleared by the
	// next key.
	flash string

	webURL string

	// openURL launches a browser; tests replace it.
	openURL func(string) error

	// pipeCommand prefills the log pane's pipe editor.
	pipeCommand string

	// mascot is where Mićko sits on the pane when the terminal has room.
	mascot config.Mascot
	// mascotBeat is his place in mascotBeats. mascotGen names the running beat
	// chain; beats of an older chain are dropped.
	mascotBeat int
	mascotGen  int
	// mascotSleep holds a pose for its beat; nil means time.Sleep.
	mascotSleep func(time.Duration)

	// redact is the profile's redactValues setting, handed to each new detail
	// view.
	redact bool

	// nsView is the namespace picker dialog; nsSeed is the namespace list the
	// profile configured, offered alongside whatever the server reports.
	nsView *namespaces.Model
	nsSeed []string
	// nsDiscovered is the server's last answer to the namespace list, kept
	// for the palette's `ns` completion. Nil means it has not been asked.
	nsDiscovered []string

	// palView is the `:` command palette; registry is the command table it
	// is loaded from and that its RunMsg is resolved against.
	palView  *palette.Model
	registry []command

	// profView is the P profile picker. conn is the live connection, closed on a
	// switch; connector builds the next one.
	profView  *profiles.Model
	conn      *Connection
	connector Connector
	// profileCurrent is the connected profile, empty before the first
	// connection. profileCursor is the file's currentProfile, which places the
	// cursor on the first open and selects nothing by itself.
	profileCurrent string
	profileCursor  string

	// kindDefs are the list routes of the resource kinds beside workflows,
	// and kindStates their collection state. cronView is the cron kind's
	// pane, typed, for the code that fills it.
	kindDefs   map[Route]*kindDef
	kindStates map[Route]*kindState
	cronView   *kindlist.Model[core.CronWorkflow]
	tmplView   *kindlist.Model[core.WorkflowTemplate]
	ctmplView  *kindlist.Model[core.WorkflowTemplate]
	archView   *kindlist.Model[core.Workflow]

	// drill is the active drill-down from a kind's row to the workflows it
	// owns; nil outside one.
	drill *drillState

	// detailFrom is the route esc returns to from detail: the workflow list or
	// the archive list.
	detailFrom Route
}

// SetWebURL records the Argo UI address links are built from.
func (m *Root) SetWebURL(u string) { m.webURL = u }

// SetPipeCommand records the command the log pipe editor prefills.
func (m *Root) SetPipeCommand(cmd string) { m.pipeCommand = cmd }

// SetMascot places the mascot on his perch, on the pane's floor, or nowhere.
// Init, or the command that moved him, starts his routine.
func (m *Root) SetMascot(spot config.Mascot) {
	m.mascot = spot
	m.help.SetMascot(spot != config.MascotOff)
	m.mascotBeat = 0
	m.mascotGen++
}

// cycleMascot moves the mascot from off to perch to floor and off again, and
// says so, including when the terminal is too small to show him.
func (m *Root) cycleMascot() tea.Cmd {
	next := map[config.Mascot]config.Mascot{
		config.MascotOff:   config.MascotPerch,
		config.MascotPerch: config.MascotFloor,
		config.MascotFloor: config.MascotOff,
	}[m.mascot]
	m.SetMascot(next)
	switch {
	case next == config.MascotOff:
		m.flash = "Mićko flew off"
	case !shell.PerchFits(m.width, m.height):
		m.flash = "Mićko is on; he shows on terminals of 80x40 and larger"
	case next == config.MascotFloor:
		m.flash = "Mićko is on the floor"
	default:
		m.flash = "Mićko is perched"
	}
	return m.nextMascotBeat()
}

// mascotBeats is his routine where he sits.
func (m *Root) mascotBeats() []shared.Beat {
	if m.mascot == config.MascotFloor {
		return shared.MickoFloorBeats
	}
	return shared.MickoPerchBeats
}

// mascotBeatMsg moves Mićko on to his next pose. gen is the chain it
// belongs to.
type mascotBeatMsg struct{ gen int }

// nextMascotBeat holds his current pose for its beat, then moves him on. It
// schedules nothing while he is off.
func (m *Root) nextMascotBeat() tea.Cmd {
	if m.mascot == config.MascotOff {
		return nil
	}
	gen, hold, sleep := m.mascotGen, m.mascotBeats()[m.mascotBeat].Hold, m.mascotSleep
	if sleep == nil {
		sleep = time.Sleep
	}
	return func() tea.Msg {
		sleep(hold)
		return mascotBeatMsg{gen: gen}
	}
}

// SetRedactValues records whether workflows open with their parameter and
// output values hidden, and applies it to the detail view now on screen.
func (m *Root) SetRedactValues(redact bool) {
	m.redact = redact
	m.detailView.SetRedactByDefault(redact)
	for _, def := range m.kindDefs {
		def.pane.SetRedact(redact)
	}
}

// listState is the list route's data and status.
type listState struct {
	// items is the last snapshot, possibly capped.
	items []core.Summary
	// loading is true while a snapshot collection runs for this generation.
	loading bool
	// incomplete marks a capped collection.
	incomplete bool
	// staleSince is when the last successful snapshot finished.
	staleSince time.Time
	// lastErr is the list's last error; the last good data stays.
	lastErr *core.APIError
	// polledAt is when the last collection started.
	polledAt time.Time
}

// detailState is the detail route's data and status.
type detailState struct {
	ref      core.Ref
	workflow core.Workflow
	loading  bool
	lastErr  *core.APIError
	notFound bool
	// archived marks a workflow read from the workflow archive rather than
	// the live route.
	archived bool
	// polledAt is when the last fetch started.
	polledAt time.Time
}

// logState is the logs route's data and status.
type logState struct {
	ref       core.Ref
	container string
	// received counts the records this stream delivered. The lines live in the
	// logs view, which bounds them.
	received int
	running  bool
	lastErr  error
	canceled bool
	// archived marks a stream of an archived run's logs, whose pods are
	// often gone; an empty or failed stream then says so.
	archived bool
	// streamID is the request ID of the stream the pane reads. Reopening for
	// timestamps keeps the generations, so only this tells the old stream's
	// replies from the new one's.
	streamID uint64
}

// NewRoot builds the root model. The reader's optional interfaces enable
// their features.
func NewRoot(r core.Reader, clock Clock, namespace string, interval time.Duration, opts actions.Options) *Root {
	var watcher core.Watcher
	if w, ok := r.(core.Watcher); ok {
		watcher = w
	}
	var actioner core.Actioner
	if a, ok := r.(core.Actioner); ok {
		actioner = a
	}
	var nsLister core.NamespaceLister
	if n, ok := r.(core.NamespaceLister); ok {
		nsLister = n
	}
	var eventWatcher core.EventWatcher
	if e, ok := r.(core.EventWatcher); ok {
		eventWatcher = e
	}
	cronLister, _ := r.(core.CronLister)
	templateLister, _ := r.(core.TemplateLister)
	clusterTemplateLister, _ := r.(core.ClusterTemplateLister)
	archive, _ := r.(core.ArchiveReader)
	theme := shared.NewTheme(false)
	m := &Root{
		deps: deps{
			reader:                r,
			watcher:               watcher,
			actioner:              actioner,
			nsLister:              nsLister,
			eventWatcher:          eventWatcher,
			cronLister:            cronLister,
			templateLister:        templateLister,
			clusterTemplateLister: clusterTemplateLister,
			archive:               archive,
			clock:                 clock,
			interval:              interval,
			namespace:             namespace,
			snapshotCap:           5000,
			pageSize:              100,
		},
		inflight:        map[string]inflightOp{},
		theme:           theme,
		skin:            shared.SkinDefault,
		nsView:          namespaces.New(theme),
		profView:        profiles.New(theme),
		listView:        workflowlist.New(theme),
		actionOpts:      opts,
		profileCurrent:  opts.Profile,
		connectionReady: true,
		connectionFresh: true,
		palView:         palette.New(theme),
		registry:        newRegistry(),
	}
	m.detailView = m.newDetailView()
	m.actionView = m.newActionView(core.Ref{})
	m.help.SetTheme(theme)
	m.palView.SetCommands(paletteSpecs(m.registry))
	m.palView.SetArgSource(m.paletteArgs)
	m.kindDefs = map[Route]*kindDef{
		RouteCron:             m.newCronKind(),
		RouteTemplates:        m.newTemplateKind(),
		RouteClusterTemplates: m.newClusterTemplateKind(),
		RouteArchived:         m.newArchivedKind(),
	}
	m.kindStates = map[Route]*kindState{}
	for r := range m.kindDefs {
		m.kindStates[r] = &kindState{}
	}
	return m
}

// newDetailView builds the detail pane with the root's theme.
func (m *Root) newDetailView() *detail.Model {
	d := detail.New()
	d.SetTheme(m.theme)
	d.SetRedactByDefault(m.redact)
	return d
}

// newActionView builds the action pane for ref with the root's theme.
func (m *Root) newActionView(ref core.Ref) *actions.Model {
	a := actions.NewWithOptions(ref, m.actionOpts)
	a.SetTheme(m.theme)
	return a
}

// ConnectionStateMsg carries transport lifecycle changes into the update
// loop. The forwarder runs on its own goroutine, so it sends this message
// instead of calling SetConnectionState.
type ConnectionStateMsg struct {
	// Conn is the connection generation the event belongs to, so an event from
	// a closed forward cannot mark the new connection lost.
	Conn   int
	Ready  bool
	Target string
}

// SetConnectionState applies a transport lifecycle change. A lost forward
// keeps the last snapshot on screen but blocks actions until a fresh one
// arrives.
func (m *Root) SetConnectionState(ready bool, target string) {
	m.connectionReady = ready
	if !ready {
		m.connectionFresh = false
	}
	if target != "" {
		m.connectionTarget = target
	}
	if !ready {
		m.cancelInflight("action")
	}
}

var _ tea.Model = (*Root)(nil)

// Init starts the first list collection, or opens the profile picker when
// the command line chose no profile.
func (m *Root) Init() tea.Cmd {
	if !m.connected() {
		return tea.Batch(m.openProfilePicker(), m.backgroundQuery(), m.nextMascotBeat())
	}
	list := m.startListGeneration()
	m.beginSpan("first_list", "list")
	return tea.Batch(list, m.waitConnStates(), m.backgroundQuery(), m.nextMascotBeat())
}

// armTick schedules the next poll unless one is pending, so the chain never
// forks.
func (m *Root) armTick() tea.Cmd {
	if m.tickArmed {
		return nil
	}
	m.tickArmed = true
	return m.deps.tickCmd()
}

// escLeavesRoute reports whether esc goes back from the active route rather
// than closing the child's own input or search.
func (m *Root) escLeavesRoute() bool {
	switch m.route {
	case RouteDetail:
		return m.detailView == nil || !m.detailView.EscapeConsumed()
	case RouteLogs:
		return m.logsView == nil || (!m.logsView.EscapeConsumed() && !m.logsView.EscapeClears())
	default:
		return false
	}
}

// textEntryActive reports whether the active route owns printable keys.
func (m *Root) textEntryActive() bool {
	switch m.route {
	case RouteList:
		return m.listView.SearchOn
	case RouteLogs:
		return m.logsView != nil && m.logsView.EscapeConsumed()
	case RouteDetail:
		return m.detailView != nil && m.detailView.TextEntry()
	default:
		if def := m.kind(m.route); def != nil {
			return def.pane.Searching()
		}
		return false
	}
}

// Update is the only place state changes; async commands only produce
// messages.
func (m *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeChildren(msg)
		return m, nil

	case mascotBeatMsg:
		if msg.gen != m.mascotGen {
			return m, nil
		}
		m.mascotBeat = (m.mascotBeat + 1) % len(m.mascotBeats())
		return m, m.nextMascotBeat()

	case tea.KeyPressMsg:
		key := msg.String()
		// Ctrl-C quits globally, including while a child modal owns input.
		if key == "ctrl+c" {
			return m.handleKey(msg)
		}
		// A non-idle action modal owns all remaining input.
		if m.actionView != nil && m.actionView.State() != actions.StateIdle {
			had := m.actionView.State()
			_, cmd := m.actionView.Update(msg)
			// Closing a finished action returns to its route and refetches it.
			if had == actions.StateOutcome && m.actionView.State() == actions.StateIdle {
				return m, tea.Batch(cmd, m.afterActionPane())
			}
			return m, cmd
		}
		// The palette owns printable keys, so q and ? are typed into it.
		if m.paletteOpen() {
			return m, m.palView.Update(msg)
		}
		// The namespace picker owns printable keys, so it is checked before the
		// global q and ?.
		if m.namespaceDialogOpen() {
			return m, m.nsView.Update(msg)
		}
		// The profile picker owns printable keys too. Before the first connection
		// there is no session to return to, so esc quits.
		if m.profileDialogOpen() {
			if key == "esc" && !m.connected() && !m.profView.Connecting() {
				return m, m.quit()
			}
			return m, m.profView.Update(msg)
		}
		// The help overlay owns every remaining key; q closes it.
		if m.help.IsOpen() {
			switch key {
			case "?", "esc", "q":
				m.help.Close()
			}
			return m, nil
		}
		// ? opens help except inside text entry.
		if key == "?" && !m.textEntryActive() {
			m.help.Toggle()
			return m, nil
		}
		// q quits except inside text entry.
		if key == "q" && !m.textEntryActive() {
			return m, m.quit()
		}
		// Clear the last command's report before running the next.
		m.flash = ""
		if m.rawMode {
			return m, m.handleRawKey(key)
		}
		if !m.textEntryActive() {
			switch key {
			case ":":
				// The palette opens on every route except the raw view.
				return m, m.openPalette()
			case "f", "ctrl+f":
				// f or ctrl+f opens the raw view, whose mouse selection skips the border.
				// In the logs pane t (tail) follows instead of f.
				m.enterRaw()
				return m, nil
			case "y":
				m.flash = m.copyLabel()
				return m, tea.SetClipboard(m.copyText())
			case "o":
				return m, m.openInBrowser()
			}
		}
		// A cluster-scoped kind has no namespace, so the namespace keys say so.
		if m.clusterScopedRoute() && (key == "n" || key == "0") && !m.textEntryActive() {
			m.flash = "cluster workflow templates belong to no namespace: n and 0 do not apply here"
			return m, nil
		}
		// n switches namespace on the list routes; in the logs pane it is the next
		// match.
		if isListRoute(m.route) && key == "n" && !m.textEntryActive() {
			return m, m.openNamespacePicker()
		}
		// 0 toggles all namespaces on the list routes; detail and logs show one
		// workflow.
		if isListRoute(m.route) && key == "0" && !m.textEntryActive() {
			return m, m.toggleAllNamespaces()
		}
		// P switches profile on every route; p is the phase filter.
		if key == "P" && !m.textEntryActive() {
			return m, m.openProfilePicker()
		}
		if m.route == RouteDetail && key == "r" && !m.textEntryActive() {
			// On the Events section r also restarts the streams.
			if m.detailView.Section() == shared.SectionEvents {
				return m, tea.Batch(m.startDetailFetch(), m.restartEvents())
			}
			return m, m.startDetailFetch()
		}
		if m.route == RouteDetail && key == "a" && !m.textEntryActive() {
			if m.detailState.archived {
				// Actions address the live object, which may be gone or be another run of
				// the same name.
				m.actionView = m.newActionView(m.selection)
				m.actionView.SetContext(m.actionOpts.Server, m.actionOpts.Profile, m.detailState.workflow.Summary.Phase)
				m.actionView.OpenUnavailable("this workflow is archived: actions apply to live workflows only")
				return m, nil
			}
			m.openDetailActions()
			return m, nil
		}
		if m.route == RouteList && key == "a" && !m.textEntryActive() {
			m.openListActions()
			return m, nil
		}
		if m.route == RouteList && key == "W" && !m.textEntryActive() {
			return m, m.toggleWatch()
		}
		if key == "esc" && m.escLeavesRoute() {
			return m, m.back()
		}
		// In a drill-down, esc returns to the kind's list once the filter is clear.
		if key == "esc" && m.route == RouteList && m.drill != nil && !m.listView.SearchOn && m.listView.Query() == "" {
			return m, m.leaveDrill()
		}
		// The list child takes the remaining keys and emits intents, which the root
		// turns into effects below.
		if m.route == RouteList {
			return m, m.updateChild(msg)
		}
		// The detail and logs children take the remaining keys. A detail key can
		// start or cancel the Explain read.
		cmd := m.updateChild(msg)
		if m.route == RouteDetail {
			return m, tea.Batch(cmd, m.syncSections())
		}
		return m, cmd

	case tea.QuitMsg:
		m.quitting = true
		return m, nil

	case tea.BackgroundColorMsg:
		m.handleBackground(msg)
		return m, nil

	case tickMsg:
		m.tickArmed = false
		// A kind's route polls its own list on the tick, whatever the workflow
		// watch's state: a token refused workflows may still read the kind.
		if m.kind(m.route) != nil {
			var cmds []tea.Cmd
			if !m.kindStates[m.route].loading {
				cmds = append(cmds, m.startKindFetch(m.route))
			}
			if m.connectionReady && !m.connectionFresh && !m.listState.loading && !terminalWatchMode(m.watchMode) {
				cmds = append(cmds, m.startListGeneration())
			}
			return m, tea.Batch(append(cmds, m.armTick())...)
		}
		if terminalWatchMode(m.watchMode) {
			return m, nil
		}
		if m.route == RouteList {
			if m.listState.loading || !m.pollDue(m.listState.polledAt, m.watchLive()) {
				return m, m.armTick()
			}
			// The poll re-arms the tick when it completes.
			return m, m.startListGeneration()
		}
		// Off the list route the tick is re-armed too, and the detail route
		// refetches its workflow.
		var cmds []tea.Cmd
		// An archived run is fixed, so its detail is not polled.
		if m.route == RouteDetail && !m.detailState.loading && m.selection.UID != "" && !m.detailState.archived &&
			m.pollDue(m.detailState.polledAt, m.watchLive() && m.inSnapshot(m.selection.UID)) {
			cmds = append(cmds, m.startDetailFetch())
		}
		// Only an accepted list clears a stale snapshot, so poll the list off its
		// route while the block is up.
		if m.connectionReady && !m.connectionFresh && !m.listState.loading {
			cmds = append(cmds, m.startListGeneration())
		}
		return m, tea.Batch(append(cmds, m.armTick())...)

	case listLoadedMsg:
		return m, m.handleListLoaded(msg)

	case watchEventMsg:
		return m, m.handleWatchEvent(msg)

	case watchDoneMsg:
		return m, m.handleWatchDone(msg)

	case notifyFailedMsg:
		m.handleNotifyFailed(msg)
		return m, nil

	case watchedTickMsg:
		return m, m.handleWatchedTick(msg)

	case watchedLoadedMsg:
		return m, m.handleWatchedLoaded(msg)

	case watchRetryMsg:
		if !m.watchReplyCurrent(msg.genStamp) || m.watchMode != "rate limited" {
			return m, nil
		}
		return m, m.startWatch()

	case ConnectionStateMsg:
		// An older generation's event comes from a forward a switch already closed.
		if msg.Conn != m.connGen {
			return m, nil
		}
		m.SetConnectionState(msg.Ready, msg.Target)
		return m, m.waitConnStates()

	case actionResultMsg:
		return m, m.handleActionResult(msg)

	case detailLoadedMsg:
		return m, m.handleDetailLoaded(msg)

	case logRecordMsg:
		return m, m.handleLogRecord(msg)

	case workflowlist.RefreshListMsg:
		// r clears terminal watch state and starts a fresh collection.
		m.watchMode = ""
		m.watchRetries = 0
		m.cancelInflight("watch")
		return m, m.startListGeneration()

	case OpenWorkflowMsg:
		cmd := m.openWorkflow(msg.Ref)
		if msg.Section != "" {
			m.detailView.SetSection(msg.Section)
		}
		return m, tea.Batch(cmd, m.syncSections())

	case explainLogMsg:
		return m, m.handleExplainLog(msg)

	case eventsMsg:
		return m, m.handleEvents(msg)

	case eventsDoneMsg:
		return m, m.handleEventsDone(msg)

	case eventsRetryMsg:
		return m, m.handleEventsRetry(msg)

	case archivedlist.OpenMsg:
		return m, m.openArchived(msg.Ref)

	case OpenLogsMsg:
		return m, m.openLogs(msg)

	case namespacesLoadedMsg:
		return m, m.handleNamespacesLoaded(msg)

	case kindLoadedMsg:
		return m, m.handleKindLoaded(msg)

	case kindlist.DrillMsg:
		return m, m.handleDrill(msg)

	case kindlist.RefreshMsg:
		if m.kind(m.route) != nil {
			return m, m.startKindFetch(m.route)
		}
		return m, nil

	case namespaces.SwitchMsg:
		return m, m.switchNamespace(msg.Namespace)

	case profiles.SwitchMsg:
		return m, m.switchProfile(msg.Profile)

	case palette.RunMsg:
		return m, m.runCommand(msg)

	case palette.UnknownMsg:
		m.unknownCommand(msg)
		return m, nil

	case profileConnectedMsg:
		return m, m.handleProfileConnected(msg)

	case logs.PipeIntent:
		return m, m.startPipe(msg)

	case logs.TimestampsIntent:
		return m, m.reopenLogs(msg)

	case logSourcesMsg:
		m.clearInflight("logsources", msg.RequestID)
		if msg.Conn != m.connGen || msg.Sel != m.selGen || m.logsView == nil || m.logsView.Ref() != msg.Ref {
			return m, nil
		}
		m.logsView.SetSources(msg.Sources)
		return m, nil

	case pipeDoneMsg:
		return m, m.handlePipeDone(msg)

	case shared.SwitchContextIntent:
		return m, m.openLogs(OpenLogsMsg{Ref: msg.Ref, PodName: msg.PodName, Container: msg.Container})

	case detail.BackMsg:
		return m, m.back()

	case detail.NodeLogsIntent:
		// A node with no resolved pod opens the workflow-wide logs, and the pane
		// names the node asked about.
		return m, m.openLogs(OpenLogsMsg{Ref: m.selection, PodName: msg.PodName, Container: "main"})

	case BackMsg:
		return m, m.back()

	case actions.ActionIntentMsg:
		// Only the active, confirmed child intent may reach the executor.
		if m.actionView == nil || m.actionView.Bulk() || msg.Request.Ref != m.actionView.Ref() ||
			m.actionView.State() != actions.StateSubmitting {
			return m, nil
		}
		return m, m.startAction(msg.Request)

	case actions.BulkIntentMsg:
		if !m.bulkIntentMatches(msg.Requests) {
			return m, nil
		}
		return m, m.startBulk(msg.Requests)

	case bulkStepMsg:
		return m, m.handleBulkStep(msg)

	default:
		return m, nil
	}
}

func terminalWatchMode(mode string) bool {
	return mode == "authentication/permission required" || mode == "rate limited; refresh required"
}

// View renders every route through the same shell: context band, bordered
// pane and key-hint band.
func (m *Root) View() tea.View {
	// The raw view skips the shell: it shows nothing but content.
	if m.rawMode {
		return m.rawView()
	}
	f := shell.Frame{
		// Global command and help hints stay visible when route hints are clipped.
		Help:        ": command  ? help",
		Width:       m.width,
		Height:      m.height,
		App:         "micko " + m.version,
		Server:      m.serverLabel(),
		Namespace:   m.namespaceLabel(),
		Mode:        m.modeLabel(),
		Mascot:      m.mascot != config.MascotOff,
		MascotFloor: m.mascot == config.MascotFloor,
		MascotPose:  m.mascotBeats()[m.mascotBeat].Pose,
	}

	// An action modal replaces the pane, so the route behind it cannot be
	// mistaken for the target.
	if m.actionView != nil && m.actionView.State() != actions.StateIdle {
		f.Title = "Confirm action"
		if m.actionView.Bulk() {
			f.Title = "Bulk action"
		}
		f.Route = "action"
		f.Hints = m.actionView.Hints()
		f.Help = ""
		f.Body = splitLines(m.actionView.View().Content)
		return m.finishView(f)
	}
	if m.profileDialogOpen() {
		f.Title = "Profile"
		f.Route = "profile"
		f.Hints = m.profView.Hints()
		if m.profView.Connecting() {
			// While connecting the hint line is a sentence, not keys.
			f.Hints, f.Notice = "", m.profView.Hints()
		}
		f.Help = ""
		m.profView.SetSize(f.BodyWidth(), f.BodyHeight())
		f.Body = m.profView.BodyLines()
		return m.finishView(f)
	}
	if m.namespaceDialogOpen() {
		f.Title = "Namespace"
		f.Route = "namespace"
		f.Hints = m.nsView.Hints()
		f.Help = ""
		m.nsView.SetSize(f.BodyWidth(), f.BodyHeight())
		f.Body = m.nsView.BodyLines()
		return m.finishView(f)
	}
	if m.help.IsOpen() {
		f.Title = "Help"
		f.Route = "help"
		f.Help = "esc close"
		f.Body = splitLines(m.help.View(f.BodyWidth(), f.BodyHeight()))
		return m.finishView(f)
	}

	// The palette sits on top of the active pane, which gets the rows left.
	bodyH := f.BodyHeight()
	var pal []string
	if m.paletteOpen() {
		pal = m.palView.BodyLines(f.BodyWidth(), paletteRows(bodyH))
		if bodyH > 0 {
			bodyH -= len(pal)
			if bodyH < 1 {
				bodyH = 1
			}
		}
	}

	switch m.route {
	case RouteDetail:
		f.Title = m.detailPaneTitle()
		f.TitleRight = m.detailSummary()
		f.Route = "detail"
		if m.detailView != nil {
			m.detailView.SetSize(f.BodyWidth(), bodyH)
			m.detailView.SetNow(m.deps.clock.Now())
			f.Hints = m.detailView.Hints()
			f.Status = m.detailView.PaneStatus()
			f.Body = m.detailView.BodyLines()
		}
	case RouteLogs:
		f.Route = "logs"
		if m.logsView != nil {
			m.logsView.SetSize(f.BodyWidth(), bodyH)
			f.Title = m.logsView.PaneTitle()
			f.TitleRight = m.logSummary()
			f.Hints = m.logsView.Hints()
			f.Status = m.logsView.PaneStatus()
			f.Body = m.logsView.BodyLines()
		}
	case RouteCron, RouteTemplates, RouteClusterTemplates, RouteArchived:
		def := m.kind(m.route)
		def.pane.SetSize(f.BodyWidth(), bodyH)
		f.Title = def.pane.PaneTitle()
		f.TitleRight = m.kindSummary(m.route, def.noun)
		f.Route = m.route.String()
		f.Hints = def.pane.Hints()
		f.Body = def.pane.BodyLines(m.deps.clock.Now())
		f.Status = def.pane.WindowStatus()
	default:
		m.listView.SetSize(f.BodyWidth(), bodyH)
		f.Title = m.listPaneTitle()
		f.TitleRight = m.listSummary()
		f.Route = "list"
		f.Hints = m.listView.Hints()
		// The list body must render before WindowStatus, which reports the
		// window that render chose.
		f.Body = m.listView.BodyLines(m.deps.clock.Now())
		f.Status = m.listStatusCell()
	}
	if pal != nil {
		f.Body = append(pal, f.Body...)
		f.Route = "command"
		f.Hints = m.palView.Hints()
		f.Help = ""
	}
	return m.finishView(f)
}

// paletteRows is how many suggestions fit in a pane of bodyH lines: at most
// palette.MaxRows and about half the pane.
func paletteRows(bodyH int) int {
	rows := palette.MaxRows
	if bodyH <= 0 {
		return rows
	}
	if half := bodyH/2 - 2; half < rows {
		rows = half
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// splitLines turns a child's rendered block into frame body lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// modeLabel returns a badge label only for read-only sessions.
func (m *Root) modeLabel() string {
	if m.actionsEnabled() {
		return ""
	}
	return "READ ONLY"
}

// actionsEnabled reports whether this session may send a mutation at all.
func (m *Root) actionsEnabled() bool {
	return m.actionOpts.AllowActions && !m.actionOpts.ReadOnly && !m.actionOpts.Demo
}

// serverLabel names where the data comes from; the demo claims no server.
func (m *Root) serverLabel() string {
	if m.actionOpts.Demo {
		return "synthetic demo"
	}
	if !m.connected() {
		return "no profile"
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

// SetVersion records the build version for the context band.
func (m *Root) SetVersion(v string) { m.version = v }

// finishView renders the frame as a full-window alternate-screen view. The
// alternate screen drops rows past the terminal height, where the footer
// is, so the frame is clamped.
func (m *Root) finishView(f shell.Frame) tea.View {
	if m.flash != "" {
		// The last command's report replaces the hints until the next key.
		f.Notice = m.flash
	}
	content := f.Render(m.theme)
	if m.height > 0 {
		lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
		content = strings.Join(shared.ClampLines(lines, m.height), "\n")
	}
	v := tea.NewView(content)
	// Bubble Tea restores the terminal on every exit path.
	v.AltScreen = true
	return v
}

// shortReason caps an error message to the room a border title can spare.
func shortReason(msg string) string {
	const max = 60
	msg = strings.TrimSpace(msg)
	if len(msg) <= max {
		return msg
	}
	return msg[:max-1] + "…"
}

// staleActionReason explains why a mutation is blocked right now.
func staleActionReason(connected bool) string {
	if !connected {
		return "connection lost: actions stay blocked until fresh data arrives"
	}
	return "data is stale: actions stay blocked until a fresh snapshot is accepted"
}

func (m *Root) listSummary() string {
	st := m.listState
	switch {
	case !m.connected():
		// An empty list behind the picker would read as a cluster with no workflows.
		return "list: no profile connected"
	case st.loading && len(st.items) == 0:
		return "list: loading..."
	case st.lastErr != nil && len(st.items) == 0:
		return "list: error: " + shortReason(st.lastErr.Message)
	case st.lastErr != nil:
		// The reason is capped so "STALE" survives the border's width.
		return "list: STALE (last good " + lenItems(st.items) + "): " + shortReason(st.lastErr.Message)
	case len(st.items) == 0:
		return "list: empty"
	default:
		s := "list: " + lenItems(st.items)
		if m.listAcrossNamespaces() {
			s += " in " + plural(countNamespaces(st.items), "namespace")
		}
		if m.watchMode != "" {
			s += " | mode: " + m.watchMode
		}
		if st.incomplete {
			s += " (incomplete: snapshot cap reached)"
		}
		return s
	}
}

// countNamespaces counts the distinct namespaces in a snapshot.
func countNamespaces(items []core.Summary) int {
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Ref.Namespace] = true
	}
	return len(seen)
}

func lenItems(items []core.Summary) string {
	return plural(len(items), "workflow")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
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
		s := "workflow " + wf.Summary.Ref.Name + " phase=" + wf.Summary.Phase
		if st.archived {
			s += " (archived)"
		}
		return s
	}
}

func (m *Root) logSummary() string {
	st := m.logState
	switch {
	case st.running:
		return "streaming " + st.ref.Name + " (" + plural(st.received, "record") + ")..."
	case st.canceled:
		return "stream canceled"
	case st.lastErr != nil:
		return "stream error: " + st.lastErr.Error()
	default:
		return "stream ended (" + plural(st.received, "record") + ")"
	}
}

func (m *Root) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, m.quit()
	}
	switch key {
	case "esc":
		return m, m.back()
	default:
		return m, nil
	}
}

// back is esc: it returns from logs to the route they were opened from, and
// from detail to the list it was opened from.
func (m *Root) back() tea.Cmd {
	switch m.route {
	case RouteLogs:
		// Leaving logs cancels the stream promptly.
		m.cancelInflight("logs")
		m.cancelInflight("logsources")
		m.logState.running = false
		// The zero value, RouteList, covers logs opened with no recorded origin.
		target := m.logsFrom
		if target != RouteDetail && target != RouteList {
			target = RouteList
		}
		m.route = target
		// Opening logs canceled the Explain read and the event streams; restart them.
		return m.syncSections()
	case RouteDetail:
		m.cancelInflight("detail")
		m.stopExplainLog()
		m.stopEvents()
		m.detailState.loading = false
		if m.kind(m.detailFrom) != nil {
			// The archive list keeps its rows and cursor; nothing it shows changed.
			m.route = m.detailFrom
			return nil
		}
		m.route = RouteList
		// Refetch now so the row the reader returns to shows the action's effect.
		if !m.listState.loading && !terminalWatchMode(m.watchMode) {
			return m.startListGeneration()
		}
		return nil
	default:
		return nil
	}
}

// openWorkflow opens a workflow's detail.
func (m *Root) openWorkflow(ref core.Ref) tea.Cmd {
	// Reselecting the same workflow keeps the generation.
	if m.route == RouteDetail && m.selection == ref && !m.detailState.archived {
		return nil
	}
	if m.route != RouteDetail {
		m.detailFrom = m.route
	}
	if m.events.ref != ref {
		// The pane drops another workflow's events, so their cursors go too.
		m.stopEvents()
	}
	m.selection = ref
	m.selGen++
	m.route = RouteDetail
	m.detailState = detailState{ref: ref, loading: true}
	m.detailView.SetArchived(false)
	m.detailView.SetLoading()
	cmd := m.startDetailFetch()
	m.beginSpan("detail_open", "detail")
	return cmd
}

// openLogs opens the logs pane and records the route it was opened from. A
// pod or container switch inside logs keeps the original origin.
func (m *Root) openLogs(msg OpenLogsMsg) tea.Cmd {
	if msg.Container == "" {
		msg.Container = "main"
	}
	// Reopening the running stream's workflow and container does nothing.
	if m.route == RouteLogs && m.logState.running && m.logState.ref == msg.Ref && m.logState.container == msg.Container {
		return nil
	}
	if m.route != RouteLogs {
		m.logsFrom = m.route
	}
	m.stopExplainLog()
	m.stopEvents()
	// Logs opened from an archived run's detail, or a pod switch inside
	// them, belong to that archived run.
	archived := m.detailState.archived && m.detailState.ref.UID == msg.Ref.UID &&
		(m.route == RouteDetail || (m.route == RouteLogs && m.logState.archived))
	m.selection = msg.Ref
	m.selGen++
	m.route = RouteLogs
	m.logState = logState{ref: msg.Ref, container: msg.Container, running: true, archived: archived}
	prev := m.logsView
	m.logsView = logs.NewModel(msg.Ref, msg.PodName, msg.Container)
	m.logsView.SetTheme(m.theme)
	m.logsView.SetPipeCommand(m.pipeCommand)
	m.logsView.KeepViewPrefs(prev)
	stream := m.startLogStream(msg)
	m.beginSpan("first_log", "logs")
	return tea.Batch(stream, m.startLogSources(msg))
}

// startLogSources gives a workflow-wide log pane the step each pod ran, so
// lines are labelled by step. The loaded detail answers for the same
// workflow; otherwise the workflow is read once.
func (m *Root) startLogSources(msg OpenLogsMsg) tea.Cmd {
	if msg.PodName != "" {
		return nil
	}
	if wf := m.detailState.workflow; wf.Summary.Ref.UID == msg.Ref.UID && wf.NodesAvailable {
		m.logsView.SetSources(podSources(wf.Nodes))
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	m.setInflight("logsources", id, cancel)
	return m.deps.logSourcesCmd(ctx, g, id, msg.Ref)
}

// reopenLogs restarts the stream with timestamps switched as the pane asked,
// keeping the pane and its lines. The old stream's late replies carry the
// old request ID and are ignored.
func (m *Root) reopenLogs(msg logs.TimestampsIntent) tea.Cmd {
	if m.route != RouteLogs || m.logsView == nil || m.logsView.Ref() != msg.Ref {
		return nil
	}
	pod, container := m.logsView.Context()
	m.logState.running = true
	m.logState.lastErr = nil
	m.logState.canceled = false
	return m.startLogStream(OpenLogsMsg{Ref: msg.Ref, PodName: pod, Container: container})
}

// startListGeneration cancels any running list and starts a fresh collection.
func (m *Root) startListGeneration() tea.Cmd {
	// Before a profile is chosen there is no reader to collect from.
	if !m.connected() {
		return nil
	}
	m.cancelInflight("list")
	m.listState.loading = true
	m.listState.polledAt = m.deps.clock.Now()
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	m.setInflight("list", id, cancel)
	cmd := m.deps.listCmd(ctx, g, id)
	return cmd
}

// startDetailFetch starts a detail fetch for the current selection.
func (m *Root) startDetailFetch() tea.Cmd {
	m.cancelInflight("detail")
	m.detailState.polledAt = m.deps.clock.Now()
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	m.setInflight("detail", id, cancel)
	// The tick reads loading to decide whether a refetch is due, so every fetch
	// sets it, whatever started it.
	m.detailState.loading = true
	if m.detailState.archived {
		return m.deps.archivedDetailCmd(ctx, g, id, m.selection)
	}
	return m.deps.detailCmd(ctx, g, id, m.selection)
}

// startLogStream starts a log stream for msg.
func (m *Root) startLogStream(msg OpenLogsMsg) tea.Cmd {
	m.cancelInflight("logs")
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	m.setInflight("logs", id, cancel)
	m.logState.streamID = id
	req := core.LogRequest{
		Ref:       msg.Ref,
		PodName:   msg.PodName,
		Container: msg.Container,
		// Follow keeps the stream open while the pod runs; the server ends it when
		// the pod finishes, and back() cancels it.
		Follow: true,
		// The pane owns the timestamps choice and keeps it across a container switch.
		Timestamps: m.logsView != nil && m.logsView.Timestamps(),
	}
	return m.deps.streamLogsCmd(ctx, g, id, req)
}

// handleListLoaded applies a list result unless it is stale.
func (m *Root) handleListLoaded(msg listLoadedMsg) tea.Cmd {
	m.clearInflight("list", msg.RequestID)
	if msg.Canceled {
		// A canceled collection is always stale.
		return nil
	}
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale: discard
	}
	m.endSpan("first_list", msg.RequestID, msg.Err != nil)
	m.endSpan("allns", msg.RequestID, msg.Err != nil)
	st := &m.listState
	st.loading = false
	if msg.Err != nil {
		// Keep the last good data. The snapshot is stale now, so mutations stay
		// blocked until a fresh list is accepted, even if the transport is fine.
		m.connectionFresh = false
		m.cancelInflight("action")
		st.lastErr = msg.Err
		if st.staleSince.IsZero() {
			st.staleSince = m.deps.clock.Now()
		}
		m.listView.SetStatus(m.listErrorStatus(msg.Err), m.listErrorText(msg.Err), m.deps.clock.Now().Sub(st.staleSince))
		switch msg.Err.Kind {
		case core.ErrUnauthenticated, core.ErrForbidden:
			m.watchMode = "authentication/permission required"
			return nil
		case core.ErrRateLimited:
			m.watchMode = "rate limited; refresh required"
			return nil
		}
		return m.armTick()
	}
	notices := m.notifySnapshot(st.items, msg.Page.Items)
	st.items = msg.Page.Items
	if m.connectionReady {
		m.connectionFresh = true
	}
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
		return notices
	}
	if m.deps.watcher != nil {
		return tea.Batch(notices, m.startWatch(), m.armTick())
	}
	return tea.Batch(notices, m.armTick())
}

// listErrorStatus picks how the list shows a failed collection: stale rows,
// or with none the refusal itself, since an empty table would read as a
// namespace with no workflows.
func (m *Root) listErrorStatus(ae *core.APIError) workflowlist.Status {
	if len(m.listState.items) > 0 {
		return workflowlist.StatusStale
	}
	switch ae.Kind {
	case core.ErrForbidden:
		return workflowlist.StatusForbidden
	case core.ErrUnauthenticated:
		return workflowlist.StatusUnauthenticated
	default:
		return workflowlist.StatusStale
	}
}

// listErrorText is the reason the list shows for a failed collection. In the
// all-namespaces view it also names the key back to one namespace, since a
// token is often refused the cluster-wide list.
func (m *Root) listErrorText(ae *core.APIError) string {
	if !m.listAcrossNamespaces() {
		return ae.Message
	}
	return "all namespaces: " + ae.Message + " — press 0 to return to " + m.deps.namespace
}

// handleDetailLoaded applies a detail result unless it is stale or its UID
// differs from the selection.
func (m *Root) handleDetailLoaded(msg detailLoadedMsg) tea.Cmd {
	m.clearInflight("detail", msg.RequestID)
	st := &m.detailState
	// A stale or canceled reply ends only its own fetch.
	st.loading = m.hasInflight("detail")
	if msg.Canceled {
		return nil
	}
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale
	}
	m.endSpan("detail_open", msg.RequestID, msg.Err != nil || msg.Workflow.Summary.Ref.UID != msg.Ref.UID)
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
	// A same-name replacement has a new UID and is a different workflow.
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
	return m.syncSections()
}

// handleLogRecord applies one batch of log records unless it is stale.
func (m *Root) handleLogRecord(msg logRecordMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return msg.Next // stale: drained and dropped
	}
	if msg.RequestID != m.logState.streamID {
		// A stream replaced by a reopen: its lines are duplicates, and its
		// cancellation must not mark the live stream canceled.
		return msg.Next
	}
	if !msg.Canceled && (len(msg.Records) > 0 || msg.Done) {
		m.endSpan("first_log", msg.RequestID, msg.Err != nil)
	}
	st := &m.logState
	st.received += len(msg.Records)
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
			case m.archivedLogOutcome(msg.Err):
			case msg.Err != nil:
				m.logsView.SetError(msg.Err.Error())
			default:
				m.logsView.SetPhase(logs.PhaseEnded)
			}
		}
		m.clearInflight("logs", msg.RequestID)
		return nil
	}
	return msg.Next
}

func (m *Root) updateChild(msg tea.Msg) tea.Cmd {
	switch m.route {
	case RouteList:
		return m.listView.Update(msg)
	case RouteDetail:
		return m.detailView.Update(msg)
	case RouteLogs:
		if m.logsView == nil {
			return nil
		}
		return m.logsView.Update(msg)
	default:
		if def := m.kind(m.route); def != nil {
			return def.pane.Update(msg)
		}
		return nil
	}
}

// resizeChildren passes a terminal resize to every child view.
func (m *Root) resizeChildren(msg tea.WindowSizeMsg) {
	m.listView.Update(msg)
	if m.detailView != nil {
		m.detailView.Update(msg)
	}
	if m.logsView != nil {
		m.logsView.Update(msg)
	}
	if m.actionView != nil {
		_, _ = m.actionView.Update(msg)
	}
}

// quit cancels all in-flight work and returns the Quit command, which
// restores the terminal.
func (m *Root) quit() tea.Cmd {
	m.cancelAll()
	// A kubectl port-forward is a child process, so it must be closed on quit.
	if m.conn != nil && m.conn.Close != nil {
		m.conn.Close()
	}
	m.quitting = true
	return tea.Quit
}

// inflightOp is one running command: the request that owns it and the
// function that cancels it.
type inflightOp struct {
	id     uint64
	cancel context.CancelFunc
}

func (m *Root) setInflight(purpose string, id uint64, cancel context.CancelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.inflight[purpose]; ok {
		old.cancel()
	}
	m.inflight[purpose] = inflightOp{id: id, cancel: cancel}
}

func (m *Root) cancelInflight(purpose string) {
	m.mu.Lock()
	op, ok := m.inflight[purpose]
	delete(m.inflight, purpose)
	m.mu.Unlock()
	if ok {
		op.cancel()
	}
}

// clearInflight retires purpose's entry only if it still belongs to request
// id, so a replaced request's reply leaves the newer one cancelable.
func (m *Root) clearInflight(purpose string, id uint64) {
	m.mu.Lock()
	if op, ok := m.inflight[purpose]; ok && op.id == id {
		delete(m.inflight, purpose)
	}
	m.mu.Unlock()
}

// hasInflight reports whether a command is running for purpose.
func (m *Root) hasInflight(purpose string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.inflight[purpose]
	return ok
}

func (m *Root) cancelAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.inflight))
	for _, op := range m.inflight {
		cancels = append(cancels, op.cancel)
	}
	m.inflight = map[string]inflightOp{}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	m.events = eventsSession{}
}
