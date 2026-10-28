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

// A kind's list is polled on the tick only while its route is active, and
// its snapshot survives leaving the route. A drill-down is the workflow list
// with the owner's label selector, so entering and leaving it bump the
// selection generation.

// kindPane is what the root needs from a kind's list view.
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

// kindLoadedMsg carries one kind collection; items is the kind's own slice
// type.
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
	// fetcher returns the call that lists the kind in a namespace ("" for all).
	// It runs on the update loop so the call captures its lister. A nil call
	// means the connection cannot list the kind; missing says why.
	fetcher func() (call func(ctx context.Context, namespace string) (any, error), missing string)
	// apply installs a successful answer into the view.
	apply func(items any, now time.Time)
}

// drillState is an active drill-down: the route it came from and the owner
// the title names.
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

// showKind makes a kind's route active, ends any drill-down and starts a
// collection unless one is running.
func (m *Root) showKind(r Route, label string) tea.Cmd {
	m.leaveDetailAndLogs()
	m.endDrill()
	m.route = r
	m.flash = label
	if st := m.kindStates[r]; st == nil || !st.loading {
		cmd := m.startKindFetch(r)
		// Only an empty pane waits on the fetch, so only it is timed.
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
// scope, stamped so an answer for a scope the reader left is dropped.
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
// good rows and marks them stale.
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

// kindErrorStatus picks how a failed collection shows: stale rows, or with
// none a refusal or missing API rather than an empty namespace.
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

// resetKinds drops every kind's snapshot after a change of scope.
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

// endDrill leaves an active drill-down and returns the workflow list to the
// session's scope. It drops the list rather than refetching it and does not
// change the route.
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

// leaveDrill returns from a drilled list to the kind's route.
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

// resetWorkflowList drops the workflow list's snapshot and requests, and
// bumps the selection generation so replies for the old scope are dropped.
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

// listAcrossNamespaces reports whether the workflow list spans every
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
