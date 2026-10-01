package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/shared"
	"github.com/ficaa1/micko/internal/ui/workflowlist"
)

// kinds.go runs the list routes of the resource kinds beside workflows —
// cron workflows and templates — and the drill-down from one of their rows
// to the workflows it owns.
//
// A kind is polled on the list's tick while its route is the active one, and
// only then; there is no watch for these kinds. Its snapshot survives leaving
// the route, so returning to it shows the rows and the cursor as they were
// while a fresh collection runs.
//
// The drill-down is the workflow list with a server-side label selector: the
// controller labels every workflow it starts from a cron workflow or a
// template with the owner's name. Entering and leaving it change what the
// list asks for, so both are a selection-generation change of the list,
// like a namespace switch in miniature.

// kindPane is what the root needs from a kind's list view. Each kind is a
// kindlist.Model over its own item type.
type kindPane interface {
	Update(tea.Msg) tea.Cmd
	SetSize(w, h int)
	BodyLines(now time.Time) []string
	PaneTitle() string
	Hints() string
	WindowStatus() string
	Searching() bool
	RawLines() []string
	SelectedName() (string, string)
	SetStatus(kindlist.Status, string, time.Duration)
	SetAllNamespaces(bool)
	Namespaced() bool
	Reset()
	Len() int
	SetRedact(bool)
	SetTheme(shared.Theme)
}

// kindState is one kind's collection state.
type kindState struct {
	loading    bool
	lastErr    *core.APIError
	staleSince time.Time
}

// kindLoadedMsg carries one kind collection. items is the kind's own slice
// type; the kind's apply function knows it.
type kindLoadedMsg struct {
	Conn      int
	RequestID uint64
	Route     Route
	Items     any
	Canceled  bool
	Err       *core.APIError
}

// kindDef ties a kind's route to its view and its fetch.
type kindDef struct {
	pane kindPane
	// noun is the singular the pane's count uses ("cron workflow").
	noun string
	// fetcher is called on the update loop and returns the call that lists
	// the kind in a namespace ("" for every namespace). The call runs off
	// the loop, so it captures the lister it needs here instead of reading
	// the root later. A nil call means this connection cannot list the kind,
	// and missing says why.
	fetcher func() (call func(ctx context.Context, namespace string) (any, error), missing string)
	// apply installs a successful answer into the view.
	apply func(items any, now time.Time)
}

// drillState is an active drill-down: which route it was entered from, and
// the owner the pane title names.
type drillState struct {
	from  Route
	title string
}

// kind returns the definition of route r, or nil when r is not a kind route.
func (m *Root) kind(r Route) *kindDef {
	if d, ok := m.kindDefs[r]; ok {
		return d
	}
	return nil
}

// kindPurpose names a kind's in-flight collection.
func kindPurpose(r Route) string { return "kind:" + r.String() }

// showKind makes a kind's route active from any route, the way esc leaves
// detail and logs, and starts a collection unless one is running. A
// drill-down in progress ends: the palette went somewhere else.
func (m *Root) showKind(r Route, label string) tea.Cmd {
	m.leaveDetailAndLogs()
	m.endDrill()
	m.route = r
	m.flash = label
	if st := m.kindStates[r]; st == nil || !st.loading {
		cmd := m.startKindFetch(r)
		// Existing rows let the reader use the pane during the fetch.
		if def := m.kind(r); def != nil && def.pane.Len() == 0 {
			m.beginSpan(r.String()+"_open", kindPurpose(r))
		}
		return cmd
	}
	return nil
}

// leaveDetailAndLogs stops the requests of the detail and logs routes when
// the palette moves away from them.
func (m *Root) leaveDetailAndLogs() {
	switch m.route {
	case RouteLogs:
		m.cancelInflight("logs")
		m.logState.running = false
	case RouteDetail:
		m.cancelInflight("detail")
		m.detailState.loading = false
	}
}

// startKindFetch starts one collection of route r's kind in the session's
// scope. The reply is stamped with the connection generation, so an answer
// for a namespace or profile the reader has left is dropped.
func (m *Root) startKindFetch(r Route) tea.Cmd {
	def := m.kind(r)
	if def == nil || !m.connected() {
		return nil
	}
	st := m.kindStates[r]
	fetch, missing := def.fetcher()
	if fetch == nil {
		def.pane.SetStatus(kindlist.StatusUnsupported, missing, 0)
		st.loading = false
		return nil
	}
	purpose := kindPurpose(r)
	m.cancelInflight(purpose)
	ctx, cancel := context.WithCancel(context.Background())
	id := m.ids.newID()
	m.setInflight(purpose, id, cancel)
	st.loading = true
	if def.pane.Len() == 0 && st.lastErr == nil {
		def.pane.SetStatus(kindlist.StatusLoading, "", 0)
	}
	ns := m.deps.scopeNamespace()
	if !def.pane.Namespaced() {
		ns = ""
	}
	gen := m.connGen
	return func() tea.Msg {
		items, err := fetch(ctx, ns)
		msg := kindLoadedMsg{Conn: gen, RequestID: id, Route: r, Items: items}
		if err != nil {
			if ctx.Err() != nil {
				msg.Canceled = true
				return msg
			}
			ae := core.AsAPIError(err)
			if ae == nil {
				ae = core.WrapAPIError(core.ErrUnavailable, 0, "list failed: transport error", err)
			}
			msg.Err = ae
		}
		return msg
	}
}

// handleKindLoaded applies one kind collection. A failure keeps the last
// good rows and marks them stale; with no rows, a refusal is shown as the
// refusal it is.
func (m *Root) handleKindLoaded(msg kindLoadedMsg) tea.Cmd {
	purpose := kindPurpose(msg.Route)
	m.clearInflight(purpose, msg.RequestID)
	def := m.kind(msg.Route)
	st := m.kindStates[msg.Route]
	if def == nil || st == nil {
		return nil
	}
	st.loading = m.hasInflight(purpose)
	if msg.Canceled || msg.Conn != m.connGen {
		return nil
	}
	m.endSpan(msg.Route.String()+"_open", msg.RequestID, msg.Err != nil)
	now := m.deps.clock.Now()
	if msg.Err != nil {
		st.lastErr = msg.Err
		if st.staleSince.IsZero() {
			st.staleSince = now
		}
		def.pane.SetStatus(m.kindErrorStatus(def, msg.Err), m.kindErrorText(def, msg.Err), now.Sub(st.staleSince))
		return nil
	}
	st.lastErr = nil
	st.staleSince = time.Time{}
	def.apply(msg.Items, now)
	def.pane.SetStatus(kindlist.StatusIdle, "", 0)
	return m.armTick()
}

// kindErrorStatus picks how a failed kind collection shows. With rows on
// screen it is a stale snapshot; with none, a refusal or a missing API is
// shown as such rather than as an empty namespace.
func (m *Root) kindErrorStatus(def *kindDef, ae *core.APIError) kindlist.Status {
	if def.pane.Len() > 0 {
		return kindlist.StatusStale
	}
	switch ae.Kind {
	case core.ErrForbidden:
		return kindlist.StatusForbidden
	case core.ErrUnauthenticated:
		return kindlist.StatusUnauthenticated
	case core.ErrNotFound, core.ErrUnsupported:
		return kindlist.StatusUnsupported
	default:
		return kindlist.StatusStale
	}
}

// kindErrorText is the reason a failed kind collection shows. Across
// namespaces it names the way back, as the workflow list does.
func (m *Root) kindErrorText(def *kindDef, ae *core.APIError) string {
	text := ae.Message
	if ae.Kind == core.ErrNotFound && ae.Status != 0 {
		text = "the server has no such list (HTTP 404: " + ae.Message + ")"
	}
	if def.pane.Namespaced() && m.deps.allNamespaces {
		return "all namespaces: " + text + " — press 0 to return to " + m.deps.namespace
	}
	return text
}

// kindSummary is the right-hand border title of a kind's pane.
func (m *Root) kindSummary(r Route, noun string) string {
	def, st := m.kind(r), m.kindStates[r]
	if def == nil || st == nil {
		return ""
	}
	n := def.pane.Len()
	switch {
	case !m.connected():
		return "no profile connected"
	case st.loading && n == 0 && st.lastErr == nil:
		return "loading..."
	case st.lastErr != nil && n == 0:
		return "error: " + shortReason(st.lastErr.Message)
	case st.lastErr != nil:
		return "STALE (last good " + plural(n, noun) + "): " + shortReason(st.lastErr.Message)
	default:
		return plural(n, noun)
	}
}

// resetKinds drops every kind's snapshot after a change of scope. Rows from
// the old namespace or profile under the new header would be wrong until the
// first answer lands.
func (m *Root) resetKinds() {
	for r, def := range m.kindDefs {
		m.cancelInflight(kindPurpose(r))
		def.pane.Reset()
		def.pane.SetAllNamespaces(m.deps.allNamespaces)
		m.kindStates[r] = &kindState{}
	}
}

// handleDrill opens the workflow list narrowed to the runs one row owns.
func (m *Root) handleDrill(msg kindlist.DrillMsg) tea.Cmd {
	if !m.connected() || m.kind(m.route) == nil {
		return nil
	}
	m.drill = &drillState{from: m.route, title: msg.Title}
	m.deps.drillNamespace = msg.Namespace
	m.deps.labelSelector = msg.Selector
	m.route = RouteList
	return m.restartWorkflowList()
}

// endDrill leaves a drill-down, if one is active, and returns the workflow
// list to the session's scope. It does not change the route. The list is
// dropped rather than refetched: the caller is moving to another route, and
// the list starts again when it is shown.
func (m *Root) endDrill() {
	if m.drill == nil {
		return
	}
	m.drill = nil
	m.deps.drillNamespace = ""
	m.deps.labelSelector = ""
	m.resetWorkflowList()
	m.listState.loading = false
}

// leaveDrill is esc on a drilled list: back to the kind's route, where the
// cursor still sits on the row that was opened.
func (m *Root) leaveDrill() tea.Cmd {
	from := m.drill.from
	m.endDrill()
	m.route = from
	if st := m.kindStates[from]; st != nil && !st.loading {
		return m.startKindFetch(from)
	}
	return nil
}

// restartWorkflowList starts the workflow list again after its scope
// changed, without touching the other routes.
func (m *Root) restartWorkflowList() tea.Cmd {
	m.resetWorkflowList()
	return m.startListGeneration()
}

// resetWorkflowList drops the workflow list's snapshot and every request
// that belongs to it. The selection generation goes up, so a reply for the
// old scope that is already on its way is discarded.
func (m *Root) resetWorkflowList() {
	m.cancelInflight("list")
	m.cancelInflight("watch")
	m.watchAttempt++
	m.selGen++
	m.listState = listState{loading: true}
	m.watchRV, m.watchMode, m.watchRetries = "", "", 0
	m.listView.SetAllNamespaces(m.listAcrossNamespaces())
	m.listView.SetQuery("")
	m.listView.SetItems(nil, m.deps.clock.Now())
	m.listView.SetStatus(workflowlist.StatusLoading, "", 0)
}

// listAcrossNamespaces reports whether the workflow list spans namespaces:
// the all-namespaces view outside a drill-down, which asks for one owner's
// namespace.
func (m *Root) listAcrossNamespaces() bool { return m.deps.listNamespace() == "" }

// listPaneTitle is the workflow list's border title. In a drill-down it
// names what the list is narrowed to.
func (m *Root) listPaneTitle() string {
	t := m.listView.PaneTitle()
	if m.drill != nil {
		t += " ← " + shared.Sanitize(m.drill.title)
	}
	return t
}

// kindURL is the Argo UI address of the selected row on a kind route.
func (m *Root) kindURL() string {
	def := m.kind(m.route)
	if def == nil || m.webURL == "" {
		return ""
	}
	ns, name := def.pane.SelectedName()
	if name == "" {
		return ""
	}
	base := m.webURL
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	switch m.route {
	case RouteCron:
		return base + "/cron-workflows/" + ns + "/" + name
	case RouteTemplates:
		return base + "/workflow-templates/" + ns + "/" + name
	case RouteClusterTemplates:
		return base + "/cluster-workflow-templates/" + name
	case RouteArchived:
		if w, ok := m.archView.Selected(); ok {
			return base + "/archived-workflows/" + ns + "/" + w.Summary.Ref.UID
		}
	}
	return ""
}
