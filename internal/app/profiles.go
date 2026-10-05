package app

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/profiles"
	"github.com/ficaa1/micko/internal/ui/workflowlist"
)

// ProfileList is the config file's profiles, as read by the entrypoint.
type ProfileList struct {
	Items []profiles.Item
	// ConfigPath is the file the items came from, or where one should be
	// written when there are none.
	ConfigPath string
	// Current is the profile named by currentProfile. It places the cursor on
	// the first open and selects nothing by itself.
	Current string
	// Err says why the file yielded no profiles, so an unreadable config does
	// not look like an empty one.
	Err string
}

// profileConnectedMsg carries the result of one reconnection.
type profileConnectedMsg struct {
	// Conn is the generation the switch was started for; an older one is stale.
	Conn       int
	Profile    string
	Connection *Connection
	Err        error
}

// SetConnector installs what builds connections; without it, choosing a
// profile does nothing.
func (m *Root) SetConnector(c Connector) { m.connector = c }

// SetProfiles loads the config file's profiles into the picker.
func (m *Root) SetProfiles(l ProfileList) {
	m.profileCursor = l.Current
	if m.profView == nil {
		return
	}
	m.profView.SetItems(l.Items, l.ConfigPath)
	if l.Err != "" {
		m.profView.SetError(l.Err)
	}
}

// Adopt makes c the live connection. The caller has already canceled the old
// connection's work and closed its transport.
func (m *Root) Adopt(c *Connection) {
	if c == nil {
		return
	}
	m.conn = c
	m.deps.reader = c.Reader
	m.dropWatches()
	m.deps.watcher, _ = c.Reader.(core.Watcher)
	m.deps.actioner, _ = c.Reader.(core.Actioner)
	m.deps.nsLister, _ = c.Reader.(core.NamespaceLister)
	m.deps.eventWatcher, _ = c.Reader.(core.EventWatcher)
	m.deps.cronLister, _ = c.Reader.(core.CronLister)
	m.deps.templateLister, _ = c.Reader.(core.TemplateLister)
	m.deps.clusterTemplateLister, _ = c.Reader.(core.ClusterTemplateLister)
	m.deps.archive, _ = c.Reader.(core.ArchiveReader)
	m.deps.namespace = c.Namespace
	// A new connection starts in its profile's namespace, not all namespaces.
	m.deps.allNamespaces = false
	m.listView.SetAllNamespaces(false)
	m.nsDiscovered = nil
	if c.Interval > 0 {
		m.deps.interval = c.Interval
	}
	m.webURL = c.WebURL
	m.pipeCommand = c.PipeCommand
	if c.Skin != "" {
		// The name was validated on load; an unknown skin keeps the current one.
		_, _ = m.ApplySkin(c.Skin)
	}
	m.SetRedactValues(c.Redact)
	m.nsSeed = c.Namespaces
	m.actionOpts.Server = c.Server
	m.actionOpts.Profile = c.Profile
	m.profileCurrent = c.Profile
	// Without a port-forward there is no lifecycle event, so it starts ready.
	m.connectionReady = true
	m.connectionFresh = c.States == nil
	m.connectionTarget = ""
}

// connected reports whether a reader is installed.
func (m *Root) connected() bool { return m.deps.reader != nil }

// openProfilePicker shows the dialog over the profiles the entrypoint read.
func (m *Root) openProfilePicker() tea.Cmd {
	if m.profView == nil {
		return nil
	}
	// Without a connector, as in the demo, there is nothing to connect to.
	if m.connector == nil {
		m.flash = "this session has no profiles to switch between"
		return nil
	}
	cursor := m.profileCurrent
	if cursor == "" {
		cursor = m.profileCursor
	}
	m.profView.Open(m.profileCurrent, cursor)
	return nil
}

// switchProfile moves the session to another cluster. The dialog stays open
// while the port-forward binds, so the wait does not look like a lost server.
func (m *Root) switchProfile(name string) tea.Cmd {
	if name == "" || m.connector == nil {
		return nil
	}
	// Everything in flight belongs to the old server; the generation bump drops
	// late replies.
	m.cancelAll()
	m.connGen++
	m.selGen++
	old := m.conn
	m.conn = nil
	m.deps.reader = nil
	m.deps.watcher, m.deps.actioner, m.deps.nsLister, m.deps.eventWatcher = nil, nil, nil, nil
	m.deps.cronLister = nil
	m.deps.templateLister, m.deps.clusterTemplateLister = nil, nil
	m.deps.archive = nil
	m.resetRoutes()
	if m.profView != nil {
		m.profView.SetConnecting(name)
	}
	gen := m.connGen
	connector := m.connector
	return func() tea.Msg {
		// Close the old forward first, so two clusters are never forwarded at once.
		if old != nil && old.Close != nil {
			old.Close()
		}
		c, err := connector.Connect(context.Background(), name)
		return profileConnectedMsg{Conn: gen, Profile: name, Connection: c, Err: err}
	}
}

// handleProfileConnected installs a finished reconnection, or reports its
// failure in the dialog, which stays open with no connection.
func (m *Root) handleProfileConnected(msg profileConnectedMsg) tea.Cmd {
	if msg.Conn != m.connGen {
		// A newer switch replaced this one, so release the connection it opened.
		if msg.Connection != nil && msg.Connection.Close != nil {
			msg.Connection.Close()
		}
		return nil
	}
	if m.profView != nil {
		m.profView.SetConnecting("")
	}
	if msg.Err != nil {
		if m.profView != nil {
			m.profView.SetError(msg.Err.Error())
		}
		return nil
	}
	m.Adopt(msg.Connection)
	if m.profView != nil {
		m.profView.Close()
	}
	m.flash = "profile: " + msg.Profile
	list := m.startListGeneration()
	m.beginSpan("first_list", "list")
	return tea.Batch(list, m.waitConnStates(), m.backgroundQuery())
}

// resetRoutes drops everything the previous connection produced, so one
// cluster's workflows never show under another's name.
func (m *Root) resetRoutes() {
	m.route = RouteList
	m.selection = core.Ref{}
	m.listState = listState{loading: true}
	m.detailState = detailState{}
	m.logState = logState{}
	m.logsView = nil
	m.detailView = m.newDetailView()
	m.detailFrom = RouteList
	m.watchRV, m.watchMode, m.watchRetries = "", "", 0
	m.drill = nil
	m.deps.drillNamespace, m.deps.labelSelector = "", ""
	m.resetKinds()
	m.listView.SetAllNamespaces(m.listAcrossNamespaces())
	// Marks belong to the old scope; a bulk action must not cross a switch.
	m.listView.ClearMarks()
	m.listView.SetItems(nil, m.deps.clock.Now())
	m.listView.SetStatus(workflowlist.StatusLoading, "", 0)
}

// profileDialogOpen reports whether the picker owns the keyboard.
func (m *Root) profileDialogOpen() bool { return m.profView != nil && m.profView.IsOpen() }

// waitConnStates delivers the connection's next lifecycle event, stamped
// with its generation. Each event re-arms the wait; a closed channel ends it.
func (m *Root) waitConnStates() tea.Cmd {
	if m.conn == nil || m.conn.States == nil {
		return nil
	}
	ch := m.conn.States
	gen := m.connGen
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		msg.Conn = gen
		return msg
	}
}
