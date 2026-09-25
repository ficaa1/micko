package app

import (
	"context"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/namespaces"
)

// namespaces.go owns the `n` key: the namespace the session is looking at.
//
// It was a config value fixed at startup, so a second namespace meant a second
// run of the program. Switching is a connection-generation change, exactly
// like reconnecting: every in-flight request belongs to the old namespace and
// is canceled, every cached snapshot is dropped, and the list starts again.
//
// It also owns the `0` key, the all-namespaces view: the same list with an
// empty namespace, which Argo answers with every workflow the token may read.
// Entering and leaving it is the same generation change as a switch.

// namespacesLoadedMsg carries the candidate names into the update loop.
type namespacesLoadedMsg struct {
	Conn int
	// RequestID names the fetch this reply belongs to, so a reply that lost
	// the race cannot retire the fetch that replaced it.
	RequestID uint64
	Names     []string
	Note      string
	Err       error
}

// SetNamespaceSeed records the namespaces the profile names. They are offered
// even when the server's own answer does not mention them: a namespace with no
// workflows right now is still a namespace the reader configured.
func (m *Root) SetNamespaceSeed(names []string) { m.nsSeed = names }

// openNamespacePicker shows the dialog and starts the fetch behind it.
//
// In the all-namespaces view no single namespace is current, so the dialog
// marks none, and choosing the namespace the session had before is a switch
// back to it rather than a no-op.
func (m *Root) openNamespacePicker() tea.Cmd {
	if m.nsView == nil {
		return nil
	}
	current := m.deps.namespace
	if m.deps.allNamespaces {
		current = ""
	}
	m.nsView.Open(current)
	// With no lister there is nothing to ask, so the dialog opens straight
	// onto the configured names and the typed entry.
	if m.deps.nsLister == nil {
		m.nsView.SetNames(nil, "no namespace list from this server; type one", m.nsSeed)
		return nil
	}
	m.nsView.SetLoading()
	return m.fetchNamespaces()
}

// fetchNamespaces asks the server for the namespace names. The picker and the
// palette's `ns` completion share the answer.
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

// handleNamespacesLoaded applies a fetch result. A failure is reported inside
// the dialog rather than closing it: the typed entry still works, and closing
// the dialog would look like the key had failed.
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
// all-namespaces view, the session's own namespace is a valid target: it
// leaves the cluster-wide list.
func (m *Root) switchNamespace(ns string) tea.Cmd {
	if ns == "" || (ns == m.deps.namespace && !m.deps.allNamespaces) {
		return nil
	}
	m.deps.namespace = ns
	m.deps.allNamespaces = false
	return m.restartList("namespace: " + ns)
}

// toggleAllNamespaces switches between the session's namespace and every
// namespace the token may read (the `0` key and `:all`).
//
// Argo lists across namespaces when the namespace segment of the path is
// empty, so the list and the watch keep their shape and only their scope
// changes. The namespace the session had is kept, and toggling back returns
// to it.
func (m *Root) toggleAllNamespaces() tea.Cmd {
	if !m.connected() {
		m.flash = "no profile connected"
		return nil
	}
	m.deps.allNamespaces = !m.deps.allNamespaces
	if m.deps.allNamespaces {
		return m.restartList("namespace: all")
	}
	return m.restartList("namespace: " + m.deps.namespace)
}

// restartList starts a connection generation on the list after the scope of
// the list changed.
//
// Everything in flight was requested for the old scope. A late reply carries
// the old workflows, and the generation bump is what makes the update loop
// discard it. The snapshot is dropped for the same reason: rows from the old
// scope under the new header would be a lie until the first list lands.
//
// A kind's route stays where it is and collects its own list in the new
// scope; a drill-down ends, since its owner belongs to the old scope.
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

// namespaceLabel is the namespace the header shows: the session's, or "all"
// in the all-namespaces view. A drill-down shows the owner's namespace, which
// is the one its list asks for.
//
// A cluster-scoped kind's route shows that instead: its list ignores the
// namespace.
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
