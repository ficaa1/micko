package app

import (
	"context"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/namespaces"
)

// namespacesLoadedMsg carries the candidate names into the update loop.
type namespacesLoadedMsg struct {
	Conn int
	// RequestID ties the reply to its fetch, so a stale reply cannot retire a
	// newer one.
	RequestID uint64
	Names     []string
	Note      string
	Err       error
}

// SetNamespaceSeed records the namespaces the profile names. They are
// offered even when the server does not report them.
func (m *Root) SetNamespaceSeed(names []string) { m.nsSeed = names }

// openNamespacePicker shows the dialog and starts the fetch behind it. In the
// all-namespaces view the dialog marks no namespace current.
func (m *Root) openNamespacePicker() tea.Cmd {
	if m.nsView == nil {
		return nil
	}
	current := m.deps.namespace
	if m.deps.allNamespaces {
		current = ""
	}
	m.nsView.Open(current)
	// With no lister the dialog offers the configured names and typed entry.
	if m.deps.nsLister == nil {
		m.nsView.SetNames(nil, "no namespace list from this server; type one", m.nsSeed)
		return nil
	}
	m.nsView.SetLoading()
	return m.fetchNamespaces()
}

// fetchNamespaces asks the server for namespace names, for the picker and
// the palette's ns completion.
func (m *Root) fetchNamespaces() tea.Cmd {
	if m.deps.nsLister == nil {
		return nil
	}
	m.cancelInflight("namespaces")
	ctx, cancel := context.WithCancel(context.Background())
	id := m.ids.newID()
	m.setInflight("namespaces", id, cancel)
	lister := m.deps.nsLister
	gen := m.connGen
	return func() tea.Msg {
		names, note, err := lister.ListNamespaces(ctx)
		return namespacesLoadedMsg{Conn: gen, RequestID: id, Names: names, Note: note, Err: err}
	}
}

// handleNamespacesLoaded applies a fetch result. A failure is shown inside
// the dialog, whose typed entry still works.
func (m *Root) handleNamespacesLoaded(msg namespacesLoadedMsg) tea.Cmd {
	m.clearInflight("namespaces", msg.RequestID)
	if msg.Conn != m.connGen || m.nsView == nil {
		return nil
	}
	if msg.Err != nil {
		if ae := core.AsAPIError(msg.Err); ae != nil {
			m.nsView.SetError(ae.Message)
		} else {
			m.nsView.SetError(msg.Err.Error())
		}
		m.nsView.SetNames(nil, "", m.nsSeed)
		return nil
	}
	m.nsDiscovered = append([]string{}, msg.Names...)
	m.nsView.SetNames(msg.Names, msg.Note, m.nsSeed)
	return nil
}

// switchNamespace moves the whole session to another namespace. From the
// all-namespaces view the session's own namespace is a valid target.
func (m *Root) switchNamespace(ns string) tea.Cmd {
	if ns == "" || (ns == m.deps.namespace && !m.deps.allNamespaces) {
		return nil
	}
	m.deps.namespace = ns
	m.deps.allNamespaces = false
	return m.restartList("namespace: " + ns)
}

// toggleAllNamespaces switches between the session's namespace and every
// namespace the token may read; toggling back returns to the one it had.
func (m *Root) toggleAllNamespaces() tea.Cmd {
	if !m.connected() {
		m.flash = "no profile connected"
		return nil
	}
	m.deps.allNamespaces = !m.deps.allNamespaces
	if m.deps.allNamespaces {
		cmd := m.restartList("namespace: all")
		m.beginSpan("allns", "list")
		return cmd
	}
	return m.restartList("namespace: " + m.deps.namespace)
}

// restartList starts a new connection generation on the list after its scope
// changed, so late replies for the old scope are dropped, and clears the
// snapshot. A kind's route collects in the new scope; a drill-down ends.
func (m *Root) restartList(flash string) tea.Cmd {
	from := m.route
	m.cancelAll()
	m.connGen++
	m.selGen++
	m.resetRoutes()
	m.flash = flash
	cmds := []tea.Cmd{m.startListGeneration()}
	if m.kind(from) != nil {
		m.route = from
		cmds = append(cmds, m.startKindFetch(from))
	}
	return tea.Batch(cmds...)
}

// namespaceLabel is the namespace the header shows: "(cluster-scoped)" on a
// cluster-scoped kind, a drill-down owner's, "all", or the session's.
func (m *Root) namespaceLabel() string {
	if m.clusterScopedRoute() {
		return "(cluster-scoped)"
	}
	if m.drill != nil && m.deps.drillNamespace != "" {
		return m.deps.drillNamespace
	}
	if m.deps.allNamespaces {
		return "all"
	}
	return m.deps.namespace
}

// namespaceDialogOpen reports whether the picker owns the keyboard.
func (m *Root) namespaceDialogOpen() bool { return m.nsView != nil && m.nsView.IsOpen() }

var _ = namespaces.SwitchMsg{}
