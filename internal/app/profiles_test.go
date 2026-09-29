package app

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/profiles"
)

// fakeConnector records the profiles asked for and the connections closed.
type fakeConnector struct {
	asked  []string
	err    error
	closed int
	states chan ConnectionStateMsg
}

func (c *fakeConnector) Connect(ctx context.Context, profile string) (*Connection, error) {
	c.asked = append(c.asked, profile)
	if c.err != nil {
		return nil, c.err
	}
	wf := workflowFixture("wf")
	reader := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	return &Connection{
		Reader:    reader,
		Profile:   profile,
		Server:    "https://" + profile + ".example.com",
		Namespace: profile + "-ns",
		Interval:  time.Second,
		States:    c.states,
		Close:     func() { c.closed++ },
	}, nil
}

func profileRoot(t *testing.T, conn *fakeConnector) *Root {
	t.Helper()
	m := newRoot(nil, "", time.Second)
	m.SetConnector(conn)
	m.SetProfiles(ProfileList{
		Items:      []profiles.Item{{Name: "dev"}, {Name: "prod"}},
		ConfigPath: "/home/x/.config/micko/config.yaml",
		Current:    "prod",
	})
	return m
}

// Without a profile the session starts in the picker.
func TestStartWithNoProfileOpensThePicker(t *testing.T) {
	m := profileRoot(t, &fakeConnector{})
	m.Init()
	if !m.profileDialogOpen() {
		t.Fatal("the session started without asking which profile to use")
	}
	if m.connected() {
		t.Error("a reader was installed before a profile was chosen")
	}
}

// currentProfile only places the cursor.
func TestCurrentProfileOnlyPlacesTheCursor(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Init()
	if len(conn.asked) != 0 {
		t.Fatalf("opening the picker connected to %v", conn.asked)
	}
	if got := m.profView.Selected(); got != "prod" {
		t.Errorf("cursor is on %q, want the file's currentProfile", got)
	}
}

func TestCapitalPOpensThePickerOnEveryRoute(t *testing.T) {
	for _, route := range []Route{RouteList, RouteDetail, RouteLogs} {
		m := profileRoot(t, &fakeConnector{})
		m.Adopt(mustConnect(t, &fakeConnector{}, "dev"))
		m.route = route
		m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
		if !m.profileDialogOpen() {
			t.Errorf("P did not open the picker on the %s route", route)
		}
	}
}

// p is the phase filter, not the profile picker.
func TestLowercasePDoesNotOpenTheProfilePicker(t *testing.T) {
	m := profileRoot(t, &fakeConnector{})
	m.Adopt(mustConnect(t, &fakeConnector{}, "dev"))
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.profileDialogOpen() {
		t.Fatal("p opened the profile picker; it is the phase filter")
	}
}

// A profile switch drops everything from the old connection.
func TestSwitchProfileStartsAConnectionGeneration(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "dev"))
	gen := m.connGen
	m.listState = listState{items: []core.Summary{workflowFixture("wf").Summary}}
	m.route = RouteDetail
	m.selection = core.Ref{UID: "old"}

	cmd := m.switchProfile("prod")
	if cmd == nil {
		t.Fatal("the switch started nothing")
	}
	if m.connGen == gen {
		t.Error("the connection generation did not move; stale replies would be accepted")
	}
	if m.route != RouteList {
		t.Errorf("route = %v, want the list", m.route)
	}
	if len(m.listState.items) != 0 {
		t.Error("the previous cluster's workflows survived the switch")
	}
	if m.selection.UID != "" {
		t.Error("a workflow from the previous cluster is still selected")
	}
	if m.connected() {
		t.Error("the old reader is still installed during the switch")
	}

	msg, ok := cmd().(profileConnectedMsg)
	if !ok {
		t.Fatalf("message = %T, want profileConnectedMsg", cmd())
	}
	if conn.closed != 1 {
		t.Errorf("closed %d connections, want the old one closed exactly once", conn.closed)
	}
	m.Update(msg)
	if !m.connected() {
		t.Fatal("the new connection was not installed")
	}
	if m.deps.namespace != "prod-ns" {
		t.Errorf("namespace = %q, want the new profile's", m.deps.namespace)
	}
	if m.actionOpts.Server != "https://prod.example.com" {
		t.Errorf("server label = %q, want the new profile's", m.actionOpts.Server)
	}
	if m.profileDialogOpen() {
		t.Error("the dialog stayed open after a successful switch")
	}
}

// An overtaken switch closes the connection it carries.
func TestAnOvertakenSwitchClosesTheConnectionItCarries(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	stale := mustConnect(t, conn, "dev")
	m.Update(profileConnectedMsg{Conn: m.connGen - 1, Profile: "dev", Connection: stale})
	if conn.closed != 1 {
		t.Errorf("closed %d connections, want the overtaken one released", conn.closed)
	}
	if m.connected() {
		t.Error("an overtaken reply installed its connection")
	}
}

func TestAFailedSwitchKeepsThePickerOpen(t *testing.T) {
	conn := &fakeConnector{err: errors.New("port-forward readiness: timed out")}
	m := profileRoot(t, conn)
	m.Init()
	cmd := m.switchProfile("prod")
	msg := cmd().(profileConnectedMsg)
	m.Update(msg)
	if !m.profileDialogOpen() {
		t.Fatal("the picker closed onto a session with no connection")
	}
	if m.connected() {
		t.Error("a failed switch installed a reader")
	}
}

func TestEscQuitsBeforeTheFirstConnection(t *testing.T) {
	m := profileRoot(t, &fakeConnector{})
	m.Init()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.quitting {
		t.Fatal("esc left the reader in a dialog with nothing behind it")
	}
}

// Esc after a connection returns to the session it was opened from.
func TestEscReturnsToTheSessionAfterAConnection(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "dev"))
	m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.quitting {
		t.Fatal("esc quit a session that had a connection")
	}
	if m.profileDialogOpen() {
		t.Error("esc left the dialog open")
	}
}

func TestAStaleTransportEventCannotMarkTheNewConnectionLost(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "dev"))
	m.connGen = 4
	m.Update(ConnectionStateMsg{Conn: 3, Ready: false})
	if !m.connectionReady {
		t.Fatal("an event from a closed forward marked the live connection lost")
	}
}

// Quitting closes the connection's port-forward.
func TestQuitClosesTheConnection(t *testing.T) {
	conn := &fakeConnector{}
	m := profileRoot(t, conn)
	m.Adopt(mustConnect(t, conn, "dev"))
	m.quit()
	if conn.closed != 1 {
		t.Errorf("closed %d connections, want the live one released on quit", conn.closed)
	}
}

func mustConnect(t *testing.T, c *fakeConnector, name string) *Connection {
	t.Helper()
	conn, err := c.Connect(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// P says why it does nothing when there is no connector.
func TestPIsRefusedWithNothingToConnectTo(t *testing.T) {
	m := newRoot(&testkit.FakeReader{}, "demo", time.Second)
	m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	if m.profileDialogOpen() {
		t.Fatal("P opened a picker with nothing to connect to")
	}
	if m.flash == "" {
		t.Error("P did nothing and said nothing")
	}
}
