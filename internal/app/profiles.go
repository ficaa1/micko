package app

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/profiles"
	"github.com/ficaa1/argo-tui/internal/ui/workflowlist"
)

// profiles.go owns the capital P key: which cluster this session talks to.
//
// The profile was fixed at startup by --profile or by currentProfile, so a
// second cluster meant a second run of the program. Switching is the widest
// generation change there is: the server, the credentials and the port-forward
// all change, so every in-flight request is canceled, the snapshot is dropped,
// the old transport is closed, and the list starts again from nothing.
//
// The key is capital P because p is the phase filter on the list and on the
// nodes tab, and n is already the namespace picker.

// ProfileList is what the entrypoint read out of the config file. The root
// only renders it; it never opens the file itself.
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
	// Conn is the generation the switch was started for. A reply from an
	// older generation belongs to a profile the reader has already left.
	Conn       int
	Profile    string
	Connection *Connection
	Err        error
}

// SetConnector installs the thing that builds connections. Without one the P
// key still opens the picker, but choosing a profile can do nothing, so the
// entrypoint always sets it for a real run.
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

// Adopt installs a connection as the live one: every request from here on goes
// through its reader, and the panes show its profile.
//
// The caller has already canceled whatever belonged to the previous
// connection. Adopt does not cancel, and it does not close the connection it
// replaces, because the switch path closes the old transport before it opens
// the new one.
func (m *Root) Adopt(c *Connection) {
	if c == nil {
		return
	}
	m.conn = c
	m.deps.reader = c.Reader
	m.deps.watcher, _ = c.Reader.(core.Watcher)
	m.deps.actioner, _ = c.Reader.(core.Actioner)
	m.deps.nsLister, _ = c.Reader.(core.NamespaceLister)
	m.deps.eventWatcher, _ = c.Reader.(core.EventWatcher)
	m.deps.cronLister, _ = c.Reader.(core.CronLister)
	m.deps.templateLister, _ = c.Reader.(core.TemplateLister)
	m.deps.clusterTemplateLister, _ = c.Reader.(core.ClusterTemplateLister)
	m.deps.archive, _ = c.Reader.(core.ArchiveReader)
	m.deps.namespace = c.Namespace
	// A new connection starts in its profile's namespace. Carrying the
	// all-namespaces view across would send the next cluster a cluster-wide
	// list the reader never asked it for.
	m.deps.allNamespaces = false
	m.listView.SetAllNamespaces(false)
	m.nsDiscovered = nil
	if c.Interval > 0 {
		m.deps.interval = c.Interval
	}
	m.webURL = c.WebURL
	m.pipeCommand = c.PipeCommand
	if c.Skin != "" {
		// The name was checked when the config file was read. Should it
		// still be unknown, the session keeps the skin it has rather than
		// refusing a connection over a colour.
		_, _ = m.ApplySkin(c.Skin)
	}
	m.SetRedactValues(c.Redact)
	m.nsSeed = c.Namespaces
	m.actionOpts.Server = c.Server
	m.actionOpts.Profile = c.Profile
	m.profileCurrent = c.Profile
	// A connection with a port-forward reports its own readiness. One without
	// has no transport to lose, so it starts ready rather than waiting for a
	// lifecycle event that never arrives.
	m.connectionReady = true
	m.connectionFresh = c.States == nil
	m.connectionTarget = ""
}

// connected reports whether a reader is installed. Before the first profile is
// chosen there is none, and every route has nothing to show.
func (m *Root) connected() bool { return m.deps.reader != nil }

// openProfilePicker shows the dialog. There is nothing to fetch: the profiles
// come from the config file the entrypoint already read.
func (m *Root) openProfilePicker() tea.Cmd {
	if m.profView == nil {
		return nil
	}
	// Without a connector nothing could be connected to, so the dialog would
	// open on an empty list and invite the reader to write a config file that
	// this session would not read. The demo is the case that reaches here.
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

// switchProfile moves the whole session to another cluster.
//
// The dialog stays open for the wait. A port-forward takes seconds to bind,
// and the alternative — closing onto an empty list — looks exactly like a
// session that lost its server.
func (m *Root) switchProfile(name string) tea.Cmd {
	if name == "" || m.connector == nil {
		return nil
	}
	// Everything in flight was requested from the old server, with the old
	// credentials. The generation bump is what makes the update loop discard
	// a reply that arrives after the switch.
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
		// The old forward is closed before the new one starts, so two
		// forwards to two clusters are never alive at the same time.
		if old != nil && old.Close != nil {
			old.Close()
		}
		c, err := connector.Connect(context.Background(), name)
		return profileConnectedMsg{Conn: gen, Profile: name, Connection: c, Err: err}
	}
}

// handleProfileConnected installs a finished reconnection, or reports why it
// failed. A failure leaves the dialog open with no connection: there is
// nothing to fall back to, and the reader has to choose again.
func (m *Root) handleProfileConnected(msg profileConnectedMsg) tea.Cmd {
	if msg.Conn != m.connGen {
		// A newer switch has replaced this one. The connection this reply
		// carries is live and nothing points at it, so it must be released.
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
	return tea.Batch(m.startListGeneration(), m.waitConnStates(), m.backgroundQuery())
}

// resetRoutes drops everything the previous connection produced and returns
// the session to a loading list. Keeping any of it would show one cluster's
// workflows under another cluster's name.
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
	// Marks name workflows of the scope being left; a bulk action must never
	// reach across a switch.
	m.listView.ClearMarks()
	m.listView.SetItems(nil, m.deps.clock.Now())
	m.listView.SetStatus(workflowlist.StatusLoading, "", 0)
}

// profileDialogOpen reports whether the picker owns the keyboard.
func (m *Root) profileDialogOpen() bool { return m.profView != nil && m.profView.IsOpen() }

// waitConnStates delivers the live connection's next transport lifecycle
// event into the update loop, stamped with the generation it belongs to.
//
// The forwarding manager runs on its own goroutine and must never touch model
// state. Each event re-arms the wait; when the connection is closed the
// channel closes and the chain ends with no message.
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
