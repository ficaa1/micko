package detail

import (
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/shared"
)

// Model is the detail route's child Tea model (bubbletea v2 Model surface
// frozen in docs/contracts.md §7). It renders the composed detail view and
// emits intents only — the root alone converts them to network effects
// (plan §4). It never starts goroutines and holds no Reader.
type Model struct {
	// state is the loaded workflow detail (or zero value while loading).
	state DetailViewState
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

	// tab is the active detail tab: "summary" | "nodes" | "resource".
	tab string
	// revealResource is the session-only explicit reveal for the resource
	// tab (DET-12 ⛨). It resets when the workflow (by UID) changes and is
	// never persisted anywhere.
	revealResource bool

	// nodes is the flattened, scrollable outline the nodes tab renders, and
	// hiddenSkipped is how many rows hideSkipped removed from it. Both are
	// rebuilt whenever the workflow or the toggle changes, so the cursor and
	// the drawn rows always share one index space.
	nodes         []FlatRow
	hiddenSkipped int
	// hideSkipped drops nodes a condition excluded. It defaults to on: a
	// large deployment workflow is mostly skipped branches, and they bury the
	// handful of nodes that actually ran.
	hideSkipped bool
	// nodeCursor is the selected row of the nodes tab; nodeTop is the first
	// visible row. resourceTop and summaryTop are the scroll anchors of the
	// tabs that have no cursor.
	nodeCursor  int
	nodeTop     int
	resourceTop int
	summaryTop  int
	// gPending is the armed half of vim's gg (see the list pane).
	gPending bool

	// theme styles the node rows. It is injected so goldens can force the
	// plain theme.
	theme shared.Theme

	width, height int
}

// New builds the detail child model.
func New() *Model {
	return &Model{tab: "summary", hideSkipped: true, theme: shared.NewTheme(false)}
}

// SetTheme injects the style set used for node rows.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

// Compile-time interface check against the frozen v2 surface.
var _ tea.Model = (*Model)(nil)

// SetWorkflow applies a freshly loaded detail.
func (m *Model) SetWorkflow(wf core.Workflow, now time.Time) {
	var sameWorkflow bool
	if m.loaded {
		sameWorkflow = m.state.Summary.Ref.UID == wf.Summary.Ref.UID
	}
	m.state = DetailViewState{
		Summary:  wf.Summary,
		Outline:  BuildNodeOutline(wf, OutlineOptions{}),
		Resource: RenderResource(wf, false),
		Message:  wf.Summary.Message,
	}
	m.rawResource = append([]byte(nil), wf.Resource...)
	m.rebuildNodes()
	if !sameWorkflow {
		m.nodeCursor, m.nodeTop = 0, 0
		m.resourceTop, m.summaryTop = 0, 0
	}
	m.loaded = true
	m.loading = false
	m.lastErr = ""
	m.notFound = false
	if !sameWorkflow {
		// Reveal is session-only per workflow: a different UID resets it.
		m.revealResource = false
	}
}

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

// SetSize implements the child view sizing contract (contracts §7).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
}

// Reveal is the session-only reveal state accessor (for tests/root).
func (m *Model) Reveal() bool { return m.revealResource }

// Update implements tea.Model. Tab switching (Tab), Esc back intent,
// session-only resource reveal (`v`) live here; intents bubble to the
// root as messages — the model never talks to a Reader (plan §4).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg.String())

	default:
		return m, nil
	}
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

	switch key {
	case "tab":
		m.tab = nextTab(m.tab)
		return nil
	case "shift+tab":
		m.tab = prevTab(m.tab)
		return nil
	case "esc":
		return backCmd()
	case "v":
		// Explicit, session-only reveal of redacted resource values.
		m.revealResource = !m.revealResource
		return nil
	case "h":
		// Toggle the skipped branches. A deployment workflow is mostly
		// skipped nodes, so this is the difference between a readable tree
		// and forty lines of "when 'false' evaluated false".
		m.hideSkipped = !m.hideSkipped
		m.rebuildNodes()
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
		return m.nodeLogsCmd()
	default:
		return nil
	}
}

// rebuildNodes re-flattens the outline and re-clamps the cursor, so the
// cursor can never point past the rows the view will draw.
func (m *Model) rebuildNodes() {
	m.nodes, m.hiddenSkipped = FlattenOutline(m.state.Outline, m.hideSkipped)
	if m.nodeCursor >= len(m.nodes) {
		m.nodeCursor = len(m.nodes) - 1
	}
	if m.nodeCursor < 0 {
		m.nodeCursor = 0
	}
}

// SelectedNode returns the node row under the cursor on the nodes tab, and
// false when there is none (another tab, or an empty outline).
func (m *Model) SelectedNode() (OutlineRow, bool) {
	if m.tab != "nodes" || m.nodeCursor < 0 || m.nodeCursor >= len(m.nodes) {
		return OutlineRow{}, false
	}
	return m.nodes[m.nodeCursor].Row, true
}

// NodeLogsIntent is the "show me this node's logs" intent. The root alone
// turns it into a stream (plan §4).
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
	case "resource":
		return len(strings.Split(strings.TrimRight(m.resolvedState().Resource, "\n"), "\n"))
	default:
		return len(strings.Split(strings.TrimRight(m.summaryText(), "\n"), "\n"))
	}
}

// viewRows is the number of content rows the active tab may draw, after the
// tab strip and the status line the pane always shows.
func (m *Model) viewRows() int {
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
	if m.tab == "nodes" {
		if n := len(m.nodes) - 1; n > 0 {
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
	case "resource":
		m.resourceTop = pos
	default:
		m.summaryTop = pos
	}
}

// backCmd emits the back intent (the root converts it to routing).
// BackMsg matches the root app's frozen back intent (app.BackMsg shape;
// detail defines its own message struct so the child package stays
// independent of internal/app — the root routes by type-agnostic handling
// or app maps it; contracts §4 pins the intent names).
type BackMsg struct{}

func backCmd() tea.Cmd { return func() tea.Msg { return BackMsg{} } }

// Init implements tea.Model — detail children have no autonomous effects.
func (m *Model) Init() tea.Cmd { return nil }

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.loading {
		return tea.NewView("detail: loading...")
	}
	switch {
	case m.notFound:
		return tea.NewView("workflow no longer available")
	case m.lastErr != "":
		return tea.NewView("detail error: " + m.lastErr)
	case !m.loaded:
		return tea.NewView("detail: (no workflow loaded)")
	}
	state := m.state
	// The resource pane reflects the session reveal state: re-render the
	// resource with values revealed when toggled this session.
	if m.revealResource {
		wf := core.Workflow{
			Summary:  state.Summary,
			Resource: m.rawResource,
		}
		state.Resource = RenderResource(wf, true)
	}
	return tea.NewView(RenderDetail(state, m.tab))
}

// PaneTitle is the shell border title: the sanitized workflow name (SEC-02).
// Before a workflow loads there is no name to show, so the title states the
// route instead of rendering an empty border.
func (m *Model) PaneTitle() string {
	if !m.loaded {
		return "Detail"
	}
	return "Detail " + shared.Sanitize(m.state.Summary.Ref.Name)
}

// Hints is the detail key contract, mirrored by the `?` overlay.
func (m *Model) Hints() string {
	if m.tab == "nodes" {
		return "tab section  j/k move  h skipped  l logs  f raw  esc back"
	}
	return "tab section  j/k scroll  v reveal  y copy  f raw  esc back"
}

// PaneStatus is the right-aligned footer cell: the workflow phase, carried
// as symbol and word so color is never the only channel (UI-03/07).
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
	lines := append([]string{tabStrip(m.tab), m.tabStatusLine()}, m.windowedLines()...)
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
		out := make([]string, 0, len(m.nodes))
		for _, r := range m.nodes {
			out = append(out, RenderFlatRow(r, 0, shared.NewTheme(true), false))
		}
		return out
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
		if !m.state.Outline.Available {
			return "node status unavailable"
		}
		s := itoaDetail(len(m.nodes)) + " nodes"
		if m.hiddenSkipped > 0 {
			s += " · " + itoaDetail(m.hiddenSkipped) + " skipped hidden (h shows them)"
		} else if !m.hideSkipped {
			s += " · skipped shown (h hides them)"
		}
		if len(m.nodes) > 0 {
			s += " · " + itoaDetail(m.nodeCursor+1) + "/" + itoaDetail(len(m.nodes))
		}
		if row, ok := m.SelectedNode(); ok && row.HasPod && row.PodName != "" {
			s += " · l logs"
		}
		return m.theme.Dim.Render(s)
	case "resource":
		reveal := "redacted (v reveals)"
		if m.revealResource {
			reveal = "REVEALED (v redacts)"
		}
		return m.theme.Dim.Render("resource · " + reveal + " · " +
			itoaDetail(m.resourceTop+1) + "/" + itoaDetail(m.scrollLines()))
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
		if !m.state.Outline.Available {
			reason := m.state.Outline.UnavailableReason
			if reason == "" {
				return []string{"the server did not send node status for this workflow"}
			}
			return []string{shared.Sanitize(reason)}
		}
		if len(m.nodes) == 0 {
			return []string{"(no nodes yet — workflow not started)"}
		}
		top := m.nodeTop
		if m.nodeCursor < top {
			top = m.nodeCursor
		}
		if m.nodeCursor >= top+h {
			top = m.nodeCursor - h + 1
		}
		if max := len(m.nodes) - h; top > max {
			top = max
		}
		if top < 0 {
			top = 0
		}
		m.nodeTop = top
		end := top + h
		if end > len(m.nodes) {
			end = len(m.nodes)
		}
		out := make([]string, 0, end-top)
		for i := top; i < end; i++ {
			out = append(out, RenderFlatRow(m.nodes[i], m.width, m.theme, i == m.nodeCursor))
		}
		return out
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

func itoaDetail(n int) string {
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

// prevTab cycles the tab strip backwards (shift+tab).
func prevTab(cur string) string {
	switch cur {
	case "summary":
		return "resource"
	case "nodes":
		return "summary"
	default:
		return "nodes"
	}
}

// bodyText picks the body for the current state. Every state renders
// something: an empty pane would say nothing about why it is empty.
func (m *Model) bodyText() string {
	switch {
	case m.loading:
		return "loading detail…"
	case m.notFound:
		return "workflow no longer available"
	case m.lastErr != "":
		return "detail error: " + shared.Sanitize(m.lastErr)
	case !m.loaded:
		return "(no workflow loaded)"
	}
	return RenderDetailBody(m.resolvedState(), m.tab)
}

// resolvedState applies the session-only resource reveal, which View also
// does; both compositions must show the same content.
func (m *Model) resolvedState() DetailViewState {
	state := m.state
	if m.revealResource {
		state.Resource = RenderResource(core.Workflow{
			Summary:  state.Summary,
			Resource: m.rawResource,
		}, true)
	}
	return state
}

// nextTab cycles summary → nodes → resource → summary.
func nextTab(cur string) string {
	switch cur {
	case "summary":
		return "nodes"
	case "nodes":
		return "resource"
	default:
		return "summary"
	}
}
