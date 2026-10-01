package detail

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// Model is the detail pane. It emits intents only; the root turns them into
// requests.
type Model struct {
	// state is the loaded workflow detail (or zero value while loading).
	state workflowState
	// rawResource keeps the workflow's raw payload for session-only
	// reveal re-renders (nil until a workflow is applied).
	rawResource []byte
	// loaded marks whether a workflow detail has been applied.
	loaded bool
	// loading renders the loading state (root-driven).
	loading bool
	// lastErr renders the error state (root-driven; sanitized at render).
	lastErr string
	// notFound renders the not-found state (root-driven).
	notFound bool
	// archived marks a workflow read from the workflow archive. The title
	// and the summary say so, and the actions hint is dropped: an archived
	// run has no live object to act on.
	archived bool

	// tab is the active detail section, one of the IDs in sections.
	tab string
	// redactByDefault is the profile's redactValues setting: when set, each
	// workflow opens with its values hidden. revealResource is what the
	// reader sees now; it starts from the setting for every workflow and `v`
	// flips it for the session.
	redactByDefault bool
	// revealResource is the session-only explicit reveal for the resource
	// tab. It resets when the workflow (by UID) changes and is
	// never persisted anywhere.
	revealResource bool

	// nodes is the flattened, scrollable outline the nodes tab renders, and
	// hiddenSkipped is how many rows hideSkipped removed from it. Both are
	// rebuilt whenever the workflow or a toggle changes, so the cursor and
	// the drawn rows always share one index space.
	nodes         []FlatRow
	hiddenSkipped int
	// nodeMap is the workflow's node map as it arrived. The info panel and
	// the progress header read their facts here, so neither needs a request.
	nodeMap map[string]core.Node
	// now is the injected clock's time, for the elapsed time of running
	// nodes. The root sets it before each render; SetWorkflow sets it too.
	now time.Time
	// folded holds the node IDs whose subtrees are folded away, and
	// folds/foldedNodes count the folded rows drawn and the rows they hide.
	// Folds are keyed by node ID, so they survive a refresh that reorders
	// or grows the tree.
	folded      map[string]bool
	folds       int
	foldedNodes int
	// autoFolded remembers the nodes the default rule has already folded,
	// so a node the reader unfolds stays open across refreshes.
	autoFolded map[string]bool
	// nameNeed and templateNeed are the widest name cell and template among
	// the rows, so every row lays its columns out at the same place.
	nameNeed     int
	templateNeed int
	// showInfo shows the node info panel for the row under the cursor.
	showInfo bool
	// find is the nodes tab's find-by-name state.
	find nodeFind
	// hideSkipped drops nodes a condition excluded. It defaults to on: a
	// large deployment workflow is mostly skipped branches, and they bury the
	// handful of nodes that actually ran.
	hideSkipped bool
	// nodePhase narrows the tab to one phase. It is separate from
	// hideSkipped: hiding skipped branches tidies the tree, while this
	// replaces the tree with the matching nodes alone.
	nodePhase NodePhase
	// nodeSort is the sibling order in the nodes tree, cycled by s.
	nodeSort NodeSort
	// nodeCursor is the selected row of the nodes tab; nodeTop is the first
	// visible row. resourceTop and summaryTop are the scroll anchors of the
	// tabs that have no cursor.
	nodeCursor int
	nodeTop    int
	// totalNodes is how many rows the tab had before the phase filter, so
	// the status line can say what it is holding back.
	totalNodes  int
	resourceTop int
	summaryTop  int
	// gPending is the armed half of vim's gg (see the list pane).
	gPending bool

	// tl is the timeline section's chart, rebuilt with the node tree, and
	// tlNameNeed the widest name cell among its rows. tlCursor is its
	// selected row and tlTop its first visible one.
	tl         timeline
	tlNameNeed int
	tlCursor   int
	tlTop      int
	// tlStale marks a chart the tree has changed under since it was laid
	// out; showSection lays it out again before it is shown.
	tlStale bool

	// ex is the Explain section: its report, its log evidence and its
	// scroll position.
	ex explainState

	// ev is the Events section: the events streamed in, the order, the
	// filter and the stream's status.
	ev eventsState

	// theme styles the node rows. It is injected so goldens can force the
	// plain theme.
	theme shared.Theme

	width, height int
}

// workflowState is what the pane draws from, derived from a core.Workflow.
type workflowState struct {
	Summary  core.Summary
	Outline  Outline
	Resource string // pre-rendered, redacted + sanitized YAML
	Message  string
	// Nodes is the node map as it arrived, for the timeline's span.
	Nodes map[string]core.Node
}

// New builds the detail child model.
func New() *Model {
	return &Model{
		tab: "nodes", hideSkipped: true, nodePhase: NodePhaseAll, nodeSort: NodeSortPipeline,
		theme: shared.NewTheme(false), folded: map[string]bool{}, autoFolded: map[string]bool{}, revealResource: true,
	}
}

// SetTheme injects the style set used for node rows.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// SetNow gives the pane the injected clock's current time, which the elapsed
// time of running nodes and their timing bars are measured to.
func (m *Model) SetNow(t time.Time) { m.now = t }

// SetWorkflow applies a freshly loaded detail.
func (m *Model) SetWorkflow(wf core.Workflow, now time.Time) {
	var sameWorkflow bool
	if m.loaded {
		sameWorkflow = m.state.Summary.Ref.UID == wf.Summary.Ref.UID
	}
	m.state = workflowState{
		Summary:  wf.Summary,
		Outline:  BuildNodeOutline(wf, OutlineOptions{}),
		Resource: RenderResource(wf, false),
		Message:  wf.Summary.Message,
		Nodes:    wf.Nodes,
	}
	m.rawResource = append([]byte(nil), wf.Resource...)
	m.nodeMap = wf.Nodes
	m.now = now
	keep := m.selectedID()
	if !sameWorkflow {
		// Folds, the find and the default-fold memory describe one
		// workflow's nodes; another workflow starts from its own defaults.
		m.folded = map[string]bool{}
		m.autoFolded = map[string]bool{}
		m.find = nodeFind{}
		m.ex = explainState{}
		m.ev = eventsState{order: m.ev.order}
		keep = ""
	}
	m.explainChanged()
	m.ev.stale = true
	m.applyDefaultFolds()
	m.rebuildNodes(keep)
	if !sameWorkflow {
		m.nodeCursor, m.nodeTop = 0, 0
		m.tlCursor, m.tlTop = 0, 0
		m.resourceTop, m.summaryTop = 0, 0
	}
	m.loaded = true
	m.loading = false
	m.lastErr = ""
	m.notFound = false
	if !sameWorkflow {
		// Reveal is session-only per workflow: a different UID resets it.
		m.revealResource = !m.redactByDefault
	}
}

// SetArchived says whether the workflow shown comes from the archive.
func (m *Model) SetArchived(on bool) { m.archived = on }

// SetLoading marks the loading state (root-driven, before data arrives).
func (m *Model) SetLoading() {
	m.loading = true
}

// SetError renders an error state (sanitized by the view at render time).
func (m *Model) SetError(msg string) {
	m.loading = false
	m.notFound = false
	m.lastErr = msg
}

// SetNotFound renders the "workflow no longer available" state.
func (m *Model) SetNotFound() {
	m.loading = false
	m.lastErr = ""
	m.notFound = true
}

// SetSize implements the child view sizing contract.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
}

// SetRedactByDefault applies the profile's redactValues setting. The
// workflow on screen follows it at once, so switching to a profile that
// redacts never leaves the previous profile's revealed values up.
func (m *Model) SetRedactByDefault(redact bool) {
	m.redactByDefault = redact
	m.revealResource = !redact
}

// Update handles a key or a resize and returns the intent it emits, if any.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		// The find input owns every key while it is open, so a name can
		// hold any letter the tab otherwise binds.
		if m.find.editing {
			return m.handleFindKey(msg.String())
		}
		if m.tab == "events" && m.ev.editing {
			m.handleEventsFilterKey(msg.String())
			return nil
		}
		return m.handleKey(msg.String())
	}
	return nil
}

// handleKey implements the detail key matrix. Every tab scrolls; only the
// nodes tab has a cursor, because only there does a row mean something a
// reader can act on.
func (m *Model) handleKey(key string) tea.Cmd {
	// vim's gg: the first g arms, the second jumps to the top.
	if m.gPending {
		m.gPending = false
		if key == "g" {
			m.jumpTo(0)
			return nil
		}
	} else if key == "g" {
		m.gPending = true
		return nil
	}

	if id, ok := sectionForKey(key); ok {
		m.showSection(id)
		return nil
	}
	switch key {
	case "tab":
		m.showSection(nextTab(m.tab))
		return nil
	case "shift+tab":
		m.showSection(prevTab(m.tab))
		return nil
	case "esc":
		// A find is cleared before esc leaves the workflow, so the key
		// undoes the last thing the reader set up, one thing at a time.
		if m.tab == "nodes" && m.find.active() {
			m.clearFind()
			return nil
		}
		if m.eventsFilterActive() {
			m.handleEventsFilterKey("esc")
			return nil
		}
		return backCmd()
	case "v":
		// Explicit, session-only reveal of redacted values: the resource
		// tab and the node info panel share it.
		m.revealResource = !m.revealResource
		return nil
	case "p":
		// Narrow the nodes tab to one phase. It belongs to that tab only:
		// the summary and the resource have no rows to filter.
		if m.tab == "nodes" {
			m.nodePhase = m.nodePhase.Next()
			m.nodeCursor, m.nodeTop = 0, 0
			m.rebuildNodes("")
		}
		return nil
	case "s":
		// Cycle the sibling order in the nodes tree. Like p, it belongs to
		// that tab only: the summary and the resource have no rows to order.
		// The cursor stays on its node, wherever the new order puts it.
		if m.tab == "nodes" {
			m.nodeSort = m.nodeSort.Next()
			m.rebuildNodes(m.selectedID())
		}
		if m.tab == "events" {
			m.handleEventsKey(key)
		}
		return nil
	case "h":
		// Toggle the skipped branches. A deployment workflow is mostly
		// skipped nodes, so this is the difference between a readable tree
		// and forty lines of "when 'false' evaluated false".
		m.hideSkipped = !m.hideSkipped
		m.rebuildNodes(m.selectedID())
		return nil
	case "j", "down":
		m.scrollBy(1)
		return nil
	case "k", "up":
		m.scrollBy(-1)
		return nil
	case "pgdown", "ctrl+d":
		m.scrollBy(m.pageStep())
		return nil
	case "pgup", "ctrl+u":
		m.scrollBy(-m.pageStep())
		return nil
	case "home":
		m.jumpTo(0)
		return nil
	case "G", "end":
		m.jumpTo(m.scrollMax())
		return nil
	case "l", "enter":
		if m.tab == "explain" {
			return m.explainLogsCmd()
		}
		return m.nodeLogsCmd()
	default:
		switch m.tab {
		case "nodes":
			m.handleNodesKey(key)
		case "timeline":
			m.handleTimelineKey(key)
		case "events":
			m.handleEventsKey(key)
		}
		return nil
	}
}

// Section is the active detail section's ID.
func (m *Model) Section() string { return m.tab }

// SetSection shows the section id, and reports false, changing nothing,
// when id names no section.
func (m *Model) SetSection(id string) bool {
	if !HasSection(id) {
		return false
	}
	m.showSection(id)
	return true
}

// SelectedNode returns the node row under the cursor on the nodes tab or
// the timeline, and false when there is none (another tab, or no rows).
func (m *Model) SelectedNode() (OutlineRow, bool) {
	switch m.tab {
	case "nodes":
		if m.nodeCursor < 0 || m.nodeCursor >= len(m.nodes) {
			return OutlineRow{}, false
		}
		return m.nodes[m.nodeCursor].Row, true
	case "timeline":
		r, ok := m.tlCursorRow()
		return r.Row, ok
	}
	return OutlineRow{}, false
}

// NodeLogsIntent is the "show me this node's logs" intent. The root alone
// turns it into a stream.
//
// PodName is empty when the server did not state its pod-naming scheme; the
// root then falls back to workflow-wide logs rather than requesting a pod
// that might not exist.
type NodeLogsIntent struct {
	NodeID  string
	Name    string
	PodName string
}

// nodeLogsCmd emits the log intent for the node under the cursor. Nodes that
// own no pod (Steps, DAG, Suspend) have no logs, so the key stays inert
// rather than opening an empty stream.
func (m *Model) nodeLogsCmd() tea.Cmd {
	row, ok := m.SelectedNode()
	if !ok || !row.HasPod {
		return nil
	}
	intent := NodeLogsIntent{NodeID: row.NodeID, Name: rowDisplayName(row), PodName: row.PodName}
	return func() tea.Msg { return intent }
}

// scrollLines is the total number of scrollable lines on the active tab.
func (m *Model) scrollLines() int {
	switch m.tab {
	case "nodes":
		return len(m.nodes)
	case "timeline":
		return len(m.tl.rows)
	case "explain":
		return len(m.explainLines())
	case "events":
		return len(m.eventsLines())
	case "resource":
		return len(strings.Split(strings.TrimRight(m.resolvedState().Resource, "\n"), "\n"))
	default:
		return len(strings.Split(strings.TrimRight(m.summaryText(), "\n"), "\n"))
	}
}

// viewRows is the number of content rows the active tab may draw, after the
// tab strip and the status line the pane always shows. The nodes tab also
// gives rows to its progress header, its column heads and a bottom info
// panel, and nodesLayout accounts for them.
func (m *Model) viewRows() int {
	switch m.tab {
	case "nodes":
		return m.nodesLayout().treeRows
	case "timeline":
		return m.timelineLayout().treeRows
	}
	h := m.height - detailChromeRows
	if h < 1 {
		return 1
	}
	return h
}

// detailChromeRows is the tab strip plus the one status line under it.
const detailChromeRows = 2

func (m *Model) pageStep() int {
	if n := m.viewRows() - 1; n > 0 {
		return n
	}
	return 1
}

// scrollMax is the largest valid cursor or scroll anchor for the active tab.
func (m *Model) scrollMax() int {
	if m.tab == "nodes" || m.tab == "timeline" {
		if n := m.scrollLines() - 1; n > 0 {
			return n
		}
		return 0
	}
	// A cursorless tab stops scrolling when its last line is on screen;
	// scrolling into blank space below the text tells the reader nothing.
	if n := m.scrollLines() - m.viewRows(); n > 0 {
		return n
	}
	return 0
}

func (m *Model) scrollBy(delta int) { m.jumpTo(m.scrollPos() + delta) }

func (m *Model) scrollPos() int {
	switch m.tab {
	case "nodes":
		return m.nodeCursor
	case "timeline":
		return m.tlCursor
	case "explain":
		return m.ex.top
	case "events":
		return m.ev.top
	case "resource":
		return m.resourceTop
	default:
		return m.summaryTop
	}
}

func (m *Model) jumpTo(pos int) {
	if pos < 0 {
		pos = 0
	}
	if max := m.scrollMax(); pos > max {
		pos = max
	}
	switch m.tab {
	case "nodes":
		m.nodeCursor = pos
	case "timeline":
		m.tlCursor = pos
	case "explain":
		m.ex.top = pos
	case "events":
		m.ev.top = pos
	case "resource":
		m.resourceTop = pos
	default:
		m.summaryTop = pos
	}
}

// BackMsg asks the root to leave the workflow.
type BackMsg struct{}

func backCmd() tea.Cmd { return func() tea.Msg { return BackMsg{} } }

// PaneTitle is the shell border title: the sanitized workflow name.
// Before a workflow loads there is no name to show, so the title states the
// route instead of rendering an empty border.
func (m *Model) PaneTitle() string {
	title := "Detail"
	if m.loaded {
		title += " " + shared.Sanitize(m.state.Summary.Ref.Name)
	}
	if m.archived {
		title += " (archived)"
	}
	return title
}

// Hints is the detail key contract, mirrored by the `?` overlay.
//
// An archived run is a record, not a live workflow: no action applies to it,
// so its hints never offer one.
func (m *Model) Hints() string {
	var h string
	switch m.tab {
	case "nodes":
		h = m.nodesHints()
	case "timeline":
		h = m.timelineHints()
	case "explain":
		h = m.explainHints()
	case "events":
		h = m.eventsHints()
	default:
		h = "tab section  1-9 jump  v reveal  y copy  a actions  f raw  r refresh  esc back"
	}
	if m.archived {
		h = strings.Replace(h, "  a actions", "", 1)
	}
	return h
}

// TextEntry reports whether the pane owns printable keys: the find input is
// open, so q, ? and every other letter are part of the name being typed.
func (m *Model) TextEntry() bool { return m.find.editing || (m.tab == "events" && m.ev.editing) }

// EscapeConsumed reports whether esc belongs to the pane rather than leaving
// the workflow: it cancels an open find or events filter, or clears a kept
// one.
func (m *Model) EscapeConsumed() bool {
	return (m.tab == "nodes" && m.find.active()) || m.eventsFilterActive()
}

// PaneStatus is the right-aligned footer cell: the workflow phase, carried
// as symbol and word so color is never the only channel.
func (m *Model) PaneStatus() string {
	if !m.loaded {
		return ""
	}
	p := m.state.Summary.Phase
	if p == "" {
		return ""
	}
	return shared.PhaseSymbol(p) + " " + shared.Sanitize(p)
}

// BodyLines renders the pane content for the shell: the tab strip and the
// active section, with no title. It is clipped to the height SetSize gave
// it, so the shell never has to discard rows the pane thinks are visible.
func (m *Model) BodyLines() []string {
	if lines, ok := m.stateLines(); ok {
		return lines
	}
	var lines []string
	switch m.tab {
	case "nodes":
		lines = append([]string{tabStripFit(m.tab, m.theme, m.width)}, m.nodesBody()...)
	case "timeline":
		lines = append([]string{tabStripFit(m.tab, m.theme, m.width)}, m.timelineBody()...)
	default:
		lines = append([]string{tabStripFit(m.tab, m.theme, m.width), m.tabStatusLine()}, m.windowedLines()...)
	}
	if m.height > 0 {
		lines = shared.ClampLines(lines, m.height)
	}
	return lines
}

// RawLines is the whole active section with no window and no styling: the
// borderless full-screen view copies and pages over this. Nothing is clipped,
// so a 300-node tree comes out complete.
func (m *Model) RawLines() []string {
	if lines, ok := m.stateLines(); ok {
		return lines
	}
	switch m.tab {
	case "nodes":
		return m.nodesRawLines()
	case "timeline":
		return m.timelineRawLines()
	case "explain":
		return m.explainRawLines()
	case "events":
		return m.eventsRawLines()
	case "resource":
		return strings.Split(strings.TrimRight(m.resolvedState().Resource, "\n"), "\n")
	default:
		return strings.Split(strings.TrimRight(m.summaryText(), "\n"), "\n")
	}
}

// stateLines returns the body for a non-loaded state (loading, error, not
// found) and false when a workflow is actually loaded.
func (m *Model) stateLines() ([]string, bool) {
	switch {
	case m.loading:
		return []string{"loading detail…"}, true
	case m.notFound:
		return []string{"workflow no longer available"}, true
	case m.lastErr != "":
		return []string{"detail error: " + shared.Sanitize(m.lastErr)}, true
	case !m.loaded:
		return []string{"(no workflow loaded)"}, true
	}
	return nil, false
}

// tabStatusLine states what the active tab is showing and what it is not.
// Hidden rows and a scroll position both have to be visible; a pane that
// quietly shows a subset of the data is the defect this line prevents.
func (m *Model) tabStatusLine() string {
	switch m.tab {
	case "nodes":
		return m.nodesStatusLine()
	case "timeline":
		return m.timelineStatusLine()
	case "explain":
		return m.explainStatusLine()
	case "events":
		return m.eventsStatusLine()
	case "resource":
		reveal := "values redacted (v reveals)"
		if m.revealResource {
			reveal = "values shown (v redacts)"
		}
		return m.theme.Dim.Render("resource · " + reveal + " · " +
			strconv.Itoa(m.resourceTop+1) + "/" + strconv.Itoa(m.scrollLines()))
	default:
		return m.theme.Dim.Render("summary · tab changes section")
	}
}

// windowedLines renders exactly the rows that fit, scrolled to the current
// position. The nodes tab moves its window only far enough to keep the cursor
// visible, so paging stays steady instead of jumping.
func (m *Model) windowedLines() []string {
	h := m.viewRows()
	switch m.tab {
	case "nodes":
		return m.nodeTreeLines(m.nodesLayout())
	case "timeline":
		return m.timelineLines(m.timelineLayout())
	case "explain":
		return sliceLines(m.explainLines(), m.ex.top, h)
	case "events":
		return sliceLines(m.eventsLines(), m.ev.top, h)
	case "resource":
		return sliceLines(strings.Split(strings.TrimRight(m.resolvedState().Resource, "\n"), "\n"), m.resourceTop, h)
	default:
		return sliceLines(strings.Split(strings.TrimRight(m.summaryText(), "\n"), "\n"), m.summaryTop, h)
	}
}

// sliceLines returns at most h lines starting at top, clamped.
func sliceLines(lines []string, top, h int) []string {
	if top < 0 {
		top = 0
	}
	if top > len(lines) {
		top = len(lines)
	}
	end := top + h
	if end > len(lines) {
		end = len(lines)
	}
	return lines[top:end]
}

// summaryText is the summary tab's content.
func (m *Model) summaryText() string {
	st := m.state
	var b strings.Builder
	if m.archived {
		b.WriteString("source:    the workflow archive — a record kept after the run; actions do not apply\n")
	}
	b.WriteString("name:      " + shared.Sanitize(st.Summary.Ref.Name) + "\n")
	b.WriteString("namespace: " + shared.Sanitize(st.Summary.Ref.Namespace) + "\n")
	b.WriteString("uid:       " + shared.Sanitize(st.Summary.Ref.UID) + "\n")
	phase := shared.Sanitize(phaseName(st.Summary.Phase))
	if st.Summary.Suspended {
		phase += "  (suspended: waiting for a resume)"
	}
	b.WriteString("phase:     " + shared.PhaseSymbol(st.Summary.Phase) + " " + phase + "\n")
	if st.Message != "" {
		b.WriteString("message:   " + shared.Sanitize(st.Message) + "\n")
	} else if st.Summary.Phase == "Failed" || st.Summary.Phase == "Error" {
		b.WriteString("message:   (none)\n")
	}
	if len(st.Summary.Labels) > 0 {
		b.WriteString("labels:    " + renderLabelsSorted(st.Summary.Labels) + "\n")
	}
	return b.String()
}

// resolvedState applies the session-only resource reveal. Every resource
// view reads through it, so the scroll bounds and the lines shown agree.
func (m *Model) resolvedState() workflowState {
	state := m.state
	if m.revealResource {
		state.Resource = RenderResource(core.Workflow{
			Summary:  state.Summary,
			Resource: m.rawResource,
		}, true)
	}
	return state
}
