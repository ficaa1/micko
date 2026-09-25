package app

import (
	"context"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/namespaces"
	"github.com/ficaa1/argo-tui/internal/ui/workflowlist"
)

// namespaces.go owns the `n` key: the namespace the session is looking at.
//
// It was a config value fixed at startup, so a second namespace meant a second
// run of the program. Switching is a connection-generation change, exactly
// like reconnecting: every in-flight request belongs to the old namespace and
// is canceled, every cached snapshot is dropped, and the list starts again.

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
func (m *Root) openNamespacePicker() tea.Cmd {
	if m.nsView == nil {
		return nil
	}
	m.nsView.Open(m.deps.namespace)
	// With no lister there is nothing to ask, so the dialog opens straight
	// onto the configured names and the typed entry.
	if m.deps.nsLister == nil {
		m.nsView.SetNames(nil, "no namespace list from this server; type one", m.nsSeed)
		return nil
	}
	m.nsView.SetLoading()
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
	m.nsView.SetNames(msg.Names, msg.Note, m.nsSeed)
	return nil
}

// switchNamespace moves the whole session to another namespace.
func (m *Root) switchNamespace(ns string) tea.Cmd {
	if ns == "" || ns == m.deps.namespace {
		return nil
	}
	// Everything in flight was requested for the old namespace. A late reply
	// carries the old workflows, and the generation bump is what makes the
	// update loop discard it.
	m.cancelAll()
	m.connGen++
	m.selGen++
	m.deps.namespace = ns
	m.route = RouteList
	m.selection = core.Ref{}
	m.listState = listState{loading: true}
	m.detailState = detailState{}
	m.logState = logState{}
	m.logsView = nil
	m.detailView = m.newDetailView()
	m.watchRV, m.watchMode, m.watchRetries = "", "", 0
	m.listView.SetItems(nil, m.deps.clock.Now())
	m.listView.SetStatus(workflowlist.StatusLoading, "", 0)
	m.flash = "namespace: " + ns
	return m.startListGeneration()
}

// namespaceDialogOpen reports whether the picker owns the keyboard.
func (m *Root) namespaceDialogOpen() bool { return m.nsView != nil && m.nsView.IsOpen() }

var _ = namespaces.SwitchMsg{}
