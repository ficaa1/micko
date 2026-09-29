package app

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
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

// Root is the top-level Tea model: it owns routing, generations,
// cancellation and the update loop that alone mutates UI state.
type Root struct {
	deps deps

	// route is the active top-level route.
	route Route

	// connGen bumps on namespace/profile switch; selGen bumps on selected
	// workflow change; responses carry the generation they were requested under.
	connGen, selGen int

	// selection is the currently selected workflow (detail/logs scope).
	selection core.Ref

	// inflight holds the cancel function of the running command for each
	// purpose, tagged with the request it belongs to. The tag is what makes
	// a late reply harmless: a handler clears the entry only when the entry
	// is still its own, so a reply that lost the race can never retire the
	// request that replaced it.
	mu       sync.Mutex
	inflight map[string]inflightOp

	// listState is the collected snapshot for the list route.
	listState listState

	// detailState is the loaded detail for the detail route.
	detailState detailState

	// logState is the retained log state for the logs route.
	logState logState

	// events is the Kubernetes event streaming for the detail pane's
	// Events section.
	events eventsSession

	// logsFrom records the route the logs pane was opened from (list via
	// the 'l' key, or detail), so a single Esc returns to that route
	// instead of dumping the user onto a never-loaded/stale detail pane.
	// Zero value beats RouteList, which is the common origin.
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
	// journalWarned records that a journal failure has been reported. It is
	// reported once per session, not after every action.
	journalWarned bool
	// connectionReady gates mutations. Data remains rendered while false.
	connectionReady  bool
	connectionFresh  bool
	connectionTarget string

	// rawMode renders the active pane's whole content with no border and no
	// bands, so a terminal mouse selection copies clean text. rawTop is its
	// scroll anchor.
	rawMode bool

	// tickArmed guards the single poll chain. The tick has to be re-armed
	// on every route, or opening a workflow stops every refresh until the
	// reader presses r. Exactly one chain may be alive: two of them double
	// the poll rate on every route change.
	tickArmed bool
	rawTop    int
	// gPending is the armed half of vim's gg while the raw view is up.
	gPending bool

	// flash is a one-line result of the last explicit command (a copy, a
	// browser open). It is shown in the footer and cleared by the next key,
	// so an action never looks like it did nothing.
	flash string

	// webURL is the Argo UI address for this profile, used to build a link
	// for the open and copy keys. Empty means the profile configured none.
	webURL string

	// openURL launches a browser. It is a field so tests can observe the
	// call instead of opening a real window.
	openURL func(string) error

	// pipeCommand prefills the log pane's pipe editor.
	pipeCommand string

	// mascot is where Mićko sits on the pane when the terminal has room.
	mascot config.Mascot
	// mascotBeat is his place in his routine there (mascotBeats). mascotGen names the
	// running beat chain; turning him off or on again starts a new one, and
	// beats of an older chain are dropped, so there is never more than one.
	mascotBeat int
	mascotGen  int
	// mascotSleep holds a pose for its beat. Nil means time.Sleep; tests
	// replace it so a beat arrives at once.
	mascotSleep func(time.Duration)

	// redact is the profile's redactValues setting. The detail view is
	// rebuilt on every namespace and profile switch, so the root holds it
	// and hands it to each new one.
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

	// profView is the profile picker dialog (the P key). conn is the live
	// connection behind it, held so a switch can close the old transport and
	// so the lifecycle channel can be re-armed; connector builds the next one.
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

	// detailFrom is the route the detail pane was opened from, which esc
	// returns to: the workflow list, or the archive list for an archived
	// run.
	detailFrom Route
}

// SetWebURL records the Argo UI address links are built from.
func (m *Root) SetWebURL(u string) { m.webURL = u }

// SetPipeCommand records the command the log pipe editor prefills with. The
// log pane is rebuilt on every open, so the root holds it.
func (m *Root) SetPipeCommand(cmd string) { m.pipeCommand = cmd }

// SetMascot puts the mascot on his perch, on the floor of the pane, or
// nowhere, and signs the help overlay with his wordmark while he is about.
// He starts in his resting pose; Init, or the command that moved him,
// starts his routine.
func (m *Root) SetMascot(spot config.Mascot) {
	m.mascot = spot
	m.help.SetMascot(spot != config.MascotOff)
	m.mascotBeat = 0
	m.mascotGen++
}

// cycleMascot moves the mascot on, from off to his perch to the floor and
// off again, and says what happened, including when the terminal is too
// small for him to show.
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

// listState is the list route's data + status.
type listState struct {
	// items is the last complete (or capped/incomplete) snapshot.
	items []core.Summary
	// loading is true while a snapshot collection runs for this generation.
	loading bool
	// incomplete marks a capped collection (visible incomplete state).
	incomplete bool
	// staleSince is when the last successful snapshot finished; errors keep
	// last good data and show stale age.
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
	// archived marks a workflow read from the workflow archive rather than
	// the live route.
	archived bool
}

// logState is the logs route's data + status.
type logState struct {
	ref       core.Ref
	container string
	// received counts the records this stream has delivered. The lines
	// themselves live in the logs view, which bounds them by line count and
	// by bytes; keeping a second unbounded copy here would grow the heap
	// for as long as a chatty pod is followed, and nothing reads it.
	received int
	running  bool
	lastErr  error
	canceled bool
	// archived marks a stream of an archived run's logs, whose pods are
	// often gone; an empty or failed stream then says so.
	archived bool
	// streamID is the request ID of the stream the pane is reading.
	// Reopening the stream for timestamps keeps the pane and the
	// generations, so only this tells the old stream's late batches and
	// its cancellation apart from the new stream's.
	streamID uint64
}

// NewRoot constructs the root model. The reader's optional interfaces
// (watch, actions, namespaces, events, the other kinds and the archive)
// enable the features that need them; opts sets what actions may do.
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

// newDetailView builds the detail pane with the root's theme, so node rows
// are styled from the same palette as every other pane.
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

// ConnectionStateMsg carries transport lifecycle changes into the update loop.
// The forwarding manager runs on its own goroutine, so it must never call
// SetConnectionState directly; it sends this message through the program
// instead and the single-threaded update loop applies it.
type ConnectionStateMsg struct {
	// Conn is the connection generation the event belongs to. A profile
	// switch bumps the generation, so an event from the forward that was just
	// closed cannot mark the new connection lost.
	Conn   int
	Ready  bool
	Target string
}

// SetConnectionState is used by the transport lifecycle bridge. A lost
// forward keeps the last-good snapshot visible but makes actions unavailable
// until a fresh snapshot is accepted.
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

// compile-time interface checks.
var _ tea.Model = (*Root)(nil)

// Init implements tea.Model. It starts the first list collection, or opens
// the profile picker when no profile was chosen on the command line: with no
// connection there is nothing to collect, and the first question a session
// with a config file of several clusters has to answer is which one.
func (m *Root) Init() tea.Cmd {
	if !m.connected() {
		return tea.Batch(m.openProfilePicker(), m.backgroundQuery(), m.nextMascotBeat())
	}
	return tea.Batch(m.startListGeneration(), m.waitConnStates(), m.backgroundQuery(), m.nextMascotBeat())
}

// armTick schedules the next poll unless one is already scheduled. Every
// caller goes through it so the chain can never fork.
func (m *Root) armTick() tea.Cmd {
	if m.tickArmed {
		return nil
	}
	m.tickArmed = true
	return m.deps.tickCmd()
}

// escLeavesRoute reports whether esc goes back from the active route. A
// child that is using esc itself, to close its own input or clear its own
// search, keeps it.
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

// Update implements tea.Model. All state mutation happens here; async
// commands only produce messages.
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
			// Closing a finished action returns to the route it was started
			// from, and refetches it: the workflows the reader is looking at
			// have just changed, and showing their pre-action state would be
			// the most misleading moment to be stale.
			if had == actions.StateOutcome && m.actionView.State() == actions.StateIdle {
				return m, tea.Batch(cmd, m.afterActionPane())
			}
			return m, cmd
		}
		// The command palette is a dialog that owns printable keys: q and ?
		// are letters of a command being typed.
		if m.paletteOpen() {
			return m, m.palView.Update(msg)
		}
		// The namespace picker is a dialog too, and it owns printable keys:
		// typing narrows the list. It therefore has to be tested before the
		// global q and ? below, or neither letter could ever be typed.
		if m.namespaceDialogOpen() {
			return m, m.nsView.Update(msg)
		}
		// The profile picker is the same kind of dialog, and it too owns
		// printable keys. Esc closes it — except before the first connection,
		// where there is no session behind it to return to, so esc quits.
		if m.profileDialogOpen() {
			if key == "esc" && !m.connected() && !m.profView.Connecting() {
				return m, m.quit()
			}
			return m, m.profView.Update(msg)
		}
		// The help overlay is a dialog: while it is
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
		// ordinary printable character (text entry isolates its keys).
		if key == "?" && !m.textEntryActive() {
			m.help.Toggle()
			return m, nil
		}
		// q quits globally on every route, except while the active child
		// owns printable text entry.
		if key == "q" && !m.textEntryActive() {
			return m, m.quit()
		}
		// Every explicit command below reports its result in the footer, so
		// the previous report is cleared first: a stale "copied" line beside
		// a new screen would be a lie.
		m.flash = ""
		if m.rawMode {
			return m, m.handleRawKey(key)
		}
		if !m.textEntryActive() {
			switch key {
			case ":":
				// The palette opens on every route. It is not offered in the
				// raw view above, whose only job is to be copied from.
				return m, m.openPalette()
			case "f", "ctrl+f":
				// The bordered pane wraps every line, so a mouse selection
				// picks up the border columns too. The raw view drops all
				// chrome for exactly that reason. `f` is full screen on
				// every route, logs included; there `t` (tail) follows.
				// ctrl+f stays as a second way in.
				m.enterRaw()
				return m, nil
			case "y":
				m.flash = m.copyLabel()
				return m, tea.SetClipboard(m.copyText())
			case "o":
				return m, m.openInBrowser()
			}
		}
		// A cluster-scoped kind belongs to no namespace, so the namespace
		// keys have nothing to change there. They say so rather than switch
		// a scope the pane does not show.
		if m.clusterScopedRoute() && (key == "n" || key == "0") && !m.textEntryActive() {
			m.flash = "cluster workflow templates belong to no namespace: n and 0 do not apply here"
			return m, nil
		}
		// `n` switches namespace. It is bound on the list because that is the
		// route the namespace describes; in the logs pane `n` is the next
		// search match, which is the vim meaning a reader expects there.
		if isListRoute(m.route) && key == "n" && !m.textEntryActive() {
			return m, m.openNamespacePicker()
		}
		// `0` toggles the all-namespaces view, bound where `n` is: on the list
		// routes, whose scope the namespace sets. The detail and logs panes
		// show one workflow, which lives in one namespace either way.
		if isListRoute(m.route) && key == "0" && !m.textEntryActive() {
			return m, m.toggleAllNamespaces()
		}
		// P switches profile. Capital, because p is the phase filter on the
		// list and on the nodes tab. It is bound on every route: a reader who
		// has drilled into a workflow can still change cluster, and the switch
		// returns them to the list anyway.
		if key == "P" && !m.textEntryActive() {
			return m, m.openProfilePicker()
		}
		if m.route == RouteDetail && key == "r" && !m.textEntryActive() {
			// On the Events section r also starts the streams over, which
			// is the way back from a stream that stopped.
			if m.detailView.Section() == shared.SectionEvents {
				return m, tea.Batch(m.startDetailFetch(), m.restartEvents())
			}
			return m, m.startDetailFetch()
		}
		if m.route == RouteDetail && key == "a" && !m.textEntryActive() {
			if m.detailState.archived {
				// The archive holds a record, not a workflow: every action
				// endpoint addresses the live object, which may be gone or
				// may be another run of the same name.
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
		if key == "esc" && m.escLeavesRoute() {
			return m, m.back()
		}
		// In a drill-down, esc returns to the kind's list once the workflow
		// list has nothing of its own left to back out of: a filter clears
		// first, as it does on the plain list.
		if key == "esc" && m.route == RouteList && m.drill != nil && !m.listView.SearchOn && m.listView.Query() == "" {
			return m, m.leaveDrill()
		}
		// List route: route the keyboard into the list child (j/k/arrows/Enter/
		// l//s/r and printable search text). The child emits intents (open,
		// logs, refresh); the root converts them to effects below. Global
		// keys (q, ctrl+c) stay at the root, and text-entry isolation means
		// printable letters — including q and r — reach the search buffer
		// while the search input is focused.
		if m.route == RouteList {
			return m, m.updateChild(msg)
		}
		// Detail/logs routes: the active child consumes non-global keys. A
		// key in the detail pane can open or leave the Explain section,
		// which starts or cancels its log read.
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
		// Only restart a poll when one is not already in flight for this
		// generation (exactly one list op per generation).
		m.tickArmed = false
		// A kind's route polls its own list on the same tick. The workflow
		// list's watch state does not stop it: a token refused workflows
		// may still read cron workflows, and the kind reports its own
		// refusal.
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
			if m.listState.loading {
				return m, m.armTick()
			}
			// The poll re-arms the tick when it completes.
			return m, m.startListGeneration()
		}
		// Off the list route the tick still has to be re-armed, or the
		// refresh stops for the rest of the session. The detail route also
		// refetches, so an open workflow tracks its own phase.
		var cmds []tea.Cmd
		// An archived run is fixed, so its detail is read once and not
		// polled.
		if m.route == RouteDetail && !m.detailState.loading && m.selection.UID != "" && !m.detailState.archived {
			cmds = append(cmds, m.startDetailFetch())
		}
		// A stale snapshot blocks every mutation, and only an accepted list
		// clears it. The list poll runs on the list route alone, so without
		// this a reader who lost the snapshot while reading a workflow stays
		// blocked for as long as they stay on it. The collection runs only
		// while the block is up, so a healthy session off the list route
		// keeps polling nothing but its own workflow.
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

	case watchRetryMsg:
		if msg.Conn != m.connGen || msg.Sel != m.selGen || msg.Attempt != m.watchAttempt || m.watchMode != "rate limited" {
			return m, nil
		}
		return m, m.startWatch()

	case ConnectionStateMsg:
		// An event stamped with an older generation belongs to a forward that
		// has already been closed by a profile switch.
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
		// Manual refresh intent from the list child (r): an explicit
		// operator refresh clears terminal watch state (the escape hatch
		// from rate-limited/unauthorized states) and starts a fresh
		// collection of this generation.
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
		// A node with no resolved pod would send the server a pod name that
		// may not exist, so the request stays workflow-wide and the pane
		// says which node the reader asked about.
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

// View implements tea.Model. Every route renders through the same shell:
// a context band, a bordered pane, and a key-hint band. The shell fixes the
// geometry, so the bands stay put while routes and connection states change.
func (m *Root) View() tea.View {
	// The raw view is deliberately outside the shell: its whole purpose is to
	// put nothing but content on the screen.
	if m.rawMode {
		return m.rawView()
	}
	f := shell.Frame{
		// `:` and `?` are root-owned on every route, so the shell advertises
		// them once rather than each pane repeating them in its own hints.
		// They share the protected right-hand cell: between them they lead to
		// every command and every key, so a narrow terminal drops route hints
		// before it drops them.
		Help:      ": command  ? help",
		Width:     m.width,
		Height:    m.height,
		App:       "micko " + m.version,
		Server:    m.serverLabel(),
		Namespace: m.namespaceLabel(),
		Mode:      m.modeLabel(),
		// The badge colour repeats what the mode words say.
		ActionsEnabled: m.actionsEnabled(),
		Mascot:         m.mascot != config.MascotOff,
		MascotFloor:    m.mascot == config.MascotFloor,
		MascotPose:     m.mascotBeats()[m.mascotBeat].Pose,
	}

	// An action modal is a dialog: it takes the pane so the route behind it
	// cannot be mistaken for the thing being confirmed.
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

	// The palette sits at the top of the active pane rather than replacing
	// it: the reader is choosing where to go from what they are looking at.
	// The pane below is sized to what the palette leaves.
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

// paletteRows is how many suggestions the palette may show in a pane of
// bodyH lines: at most palette.MaxRows, and never more than about half the
// pane, so the view underneath keeps its column heads and a few rows.
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

// modeLabel is the safety state: the single word that says whether this
// session can mutate anything. It is always shown.
func (m *Root) modeLabel() string {
	if m.actionsEnabled() {
		return "ACTIONS ENABLED"
	}
	return "READ ONLY"
}

// actionsEnabled reports whether this session may send a mutation at all.
func (m *Root) actionsEnabled() bool {
	return m.actionOpts.AllowActions && !m.actionOpts.ReadOnly && !m.actionOpts.Demo
}

// serverLabel names where the data comes from. The demo must never claim a
// server it does not have.
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
	if m.flash != "" {
		// The result of the last explicit command replaces the route hints
		// for one screen: it answers the key the reader just pressed, and it
		// disappears on their next key.
		f.Notice = m.flash
	}
	content := f.Render(m.theme)
	if m.height > 0 {
		lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
		content = strings.Join(shared.ClampLines(lines, m.height), "\n")
	}
	v := tea.NewView(content)
	// Full-window alternate screen: the layout is stable in the alternate
	// buffer, and bubbletea restores the terminal on every exit/error path
	// (q, Ctrl-C, and any program teardown).
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
		// The picker is on top of this pane. An "empty" list behind it would
		// read as a cluster with no workflows.
		return "list: no profile connected"
	case st.loading && len(st.items) == 0:
		return "list: loading..."
	case st.lastErr != nil && len(st.items) == 0:
		return "list: error: " + shortReason(st.lastErr.Message)
	case st.lastErr != nil:
		// The stale warning must survive the border's width budget, so the
		// reason is capped: an operator needs to see "stale" far more than to
		// read a full transport error on the border.
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

// key handling ---------------------------------------------------------------

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

// back implements Esc semantics: logs -> (origin) -> list. The logs pane
// returns to the route it was opened from — the list when 'l' was pressed
// on the list, or the loaded detail when logs were entered from detail —
// so a single Esc never lands on a never-loaded or stale detail pane.
func (m *Root) back() tea.Cmd {
	switch m.route {
	case RouteLogs:
		// Leaving logs cancels the stream promptly.
		m.cancelInflight("logs")
		m.cancelInflight("logsources")
		m.logState.running = false
		// logsFrom is set by openLogs; a zero value (RouteList) is the
		// safe fallback for a log view reached with no explicit origin.
		target := m.logsFrom
		if target != RouteDetail && target != RouteList {
			target = RouteList
		}
		m.route = target
		// Opening the log canceled an explain read and the event streams;
		// back on the section they start again.
		return m.syncSections()
	case RouteDetail:
		m.cancelInflight("detail")
		m.stopExplainLog()
		m.stopEvents()
		m.detailState.loading = false
		if m.kind(m.detailFrom) != nil {
			// An archived run returns to the archive list, which keeps its
			// rows and cursor; nothing it shows changed.
			m.route = m.detailFrom
			return nil
		}
		m.route = RouteList
		// A Resume or a Stop changes the row the reader is about to look
		// at. Waiting for the next poll shows the old phase first, which
		// reads as "the action did nothing".
		if !m.listState.loading && !terminalWatchMode(m.watchMode) {
			return m.startListGeneration()
		}
		return nil
	default:
		return nil
	}
}

// openWorkflow handles the list→detail intent.
func (m *Root) openWorkflow(ref core.Ref) tea.Cmd {
	// Same workflow re-selected: no generation bump.
	if m.route == RouteDetail && m.selection == ref && !m.detailState.archived {
		return nil
	}
	if m.route != RouteDetail {
		m.detailFrom = m.route
	}
	m.selection = ref
	m.selGen++
	m.route = RouteDetail
	m.detailState = detailState{ref: ref, loading: true}
	m.detailView.SetArchived(false)
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
	return tea.Batch(m.startLogStream(msg), m.startLogSources(msg))
}

// startLogSources gives a workflow-wide log pane the node each pod belongs
// to, so its lines are labelled by step name rather than by pod name. The
// loaded detail answers when it is the same workflow; otherwise the
// workflow is read once. A pane on one pod needs no map, and a failed read
// only leaves the labels on pod names.
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

// reopenLogs restarts the stream with server timestamps switched as the
// pane asked, keeping the pane and its retained lines. The pane has marked
// the switch point already. The old stream is canceled first; its late
// replies carry the old request ID and are ignored.
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

// startListGeneration cancels any prior list, bumps nothing (list runs per
// connection generation), and starts a fresh single-flight collection.
func (m *Root) startListGeneration() tea.Cmd {
	// Before a profile is chosen there is no reader to collect from. The
	// picker is on screen; the list waits for it.
	if !m.connected() {
		return nil
	}
	m.cancelInflight("list")
	m.listState.loading = true
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
	ctx, cancel := context.WithCancel(context.Background())
	g := genStamp{Conn: m.connGen, Sel: m.selGen}
	id := m.ids.newID()
	m.setInflight("detail", id, cancel)
	// loading is what the poll tick reads to decide whether a refetch is
	// due. Setting it here keeps it true for exactly as long as a fetch is
	// running, whichever key or timer started it.
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
		// Follow keeps the stream open so a running workflow keeps
		// delivering. Without it the server closes at the current end of
		// the log and the reader has to leave and re-enter the view. The
		// server ends the stream itself once the pod finishes, and back()
		// cancels it on exit.
		Follow: true,
		// The pane owns the timestamps choice (ctrl+t) and keeps it across
		// a container switch.
		Timestamps: m.logsView != nil && m.logsView.Timestamps(),
	}
	return m.deps.streamLogsCmd(ctx, g, id, req)
}

// handleListLoaded applies a list result honoring generation/request
// staleness (ignore stale completions even after cancellation).
func (m *Root) handleListLoaded(msg listLoadedMsg) tea.Cmd {
	m.clearInflight("list", msg.RequestID)
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
		// Keep last good data; record error + stale age. The
		// snapshot is now stale, so mutations stay blocked until a fresh
		// list is accepted, even if the transport itself never reported a
		// loss (a server-side failure is just as stale as a dead forward).
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
		return nil
	}
	if m.deps.watcher != nil {
		return tea.Batch(m.startWatch(), m.armTick())
	}
	return m.armTick()
}

// listErrorStatus picks how the list shows a failed collection. With rows
// still on screen the failure is a stale snapshot. With none, a refusal is
// shown as the refusal it is: an empty table under a small "stale" note
// reads as a namespace with no workflows, which is the one wrong conclusion
// the reader must not draw.
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
// all-namespaces view it says which request failed and how to leave it: a
// token that may read one namespace is routinely refused the cluster-wide
// list, and the fix is the key that returns to that namespace.
func (m *Root) listErrorText(ae *core.APIError) string {
	if !m.listAcrossNamespaces() {
		return ae.Message
	}
	return "all namespaces: " + ae.Message + " — press 0 to return to " + m.deps.namespace
}

// handleDetailLoaded applies a detail result honoring staleness + UID
// validation (same-name replacement is a different workflow).
func (m *Root) handleDetailLoaded(msg detailLoadedMsg) tea.Cmd {
	m.clearInflight("detail", msg.RequestID)
	st := &m.detailState
	// loading tracks the fetch, not this reply. A canceled or stale reply
	// still ends a fetch, so leaving the flag set here would tell the poll
	// tick that a fetch is running for the rest of the session and stop the
	// detail view refreshing on its own.
	st.loading = m.hasInflight("detail")
	if msg.Canceled {
		return nil
	}
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale
	}
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
	return m.syncSections()
}

// handleLogRecord applies one batched delivery honoring staleness.
func (m *Root) handleLogRecord(msg logRecordMsg) tea.Cmd {
	if msg.Conn != m.connGen || msg.Sel != m.selGen {
		return nil // stale
	}
	if msg.RequestID != m.logState.streamID {
		// A stream replaced by a reopen on the same pane: its batches would
		// duplicate lines, and its cancellation would mark the live stream
		// canceled.
		return nil
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
		if def := m.kind(m.route); def != nil {
			return def.pane.Update(msg)
		}
		return nil
	}
}

// resizeChildren propagates a terminal resize to every child view model so
// the active route and the ones the user can switch to all re-lay out. The
// Tea event loop re-renders View after every Update, so mutating child sizes
// here is sufficient to trigger a responsive redraw.
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
	// The transport outlives the update loop unless it is released here: a
	// kubectl port-forward is a child process, and quitting without closing it
	// leaves it running against a terminal that is gone.
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

// clearInflight retires the entry for purpose only when it still belongs to
// request id. A reply from a request that has already been replaced leaves
// the newer entry alone, which keeps the newer request cancelable.
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
