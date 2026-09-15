package profiles

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"argo-tui/internal/ui/shared"
)

func testModel(items ...Item) *Model {
	m := New(shared.NewTheme(true))
	m.SetItems(items, "/home/x/.config/argo-tui/config.yaml")
	return m
}

func press(m *Model, keys string) tea.Cmd {
	var last tea.Cmd
	for _, r := range keys {
		last = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return last
}

func body(m *Model) string { return strings.Join(m.BodyLines(), "\n") }

// Enter emits the chosen profile. The root turns that into a reconnection, so
// the picker must never emit anything else.
func TestEnterEmitsTheProfileUnderTheCursor(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on another profile produced no switch")
	}
	msg, ok := cmd().(SwitchMsg)
	if !ok {
		t.Fatalf("message = %T, want SwitchMsg", cmd())
	}
	if msg.Profile != "prod" {
		t.Errorf("Profile = %q, want prod", msg.Profile)
	}
}

// Choosing the connected profile is not a reconnection. Treating it as one
// would drop a working session and rebuild it for nothing.
func TestEnterOnTheCurrentProfileJustCloses(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter on the connected profile started a reconnection")
	}
	if m.IsOpen() {
		t.Error("the dialog stayed open")
	}
}

// Typed text narrows the list and nothing more. A profile carries a server and
// a credential source, so a name that is in no config file names nothing that
// could be connected to.
func TestTypedTextThatMatchesNothingConnectsNothing(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	press(m, "zzz")
	if got := m.Selected(); got != "" {
		t.Errorf("Selected() = %q, want empty", got)
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter connected to a profile that is not configured")
	}
	if !strings.Contains(body(m), "no profile matches zzz") {
		t.Errorf("body does not say why the list is empty:\n%s", body(m))
	}
}

// Filtering leaves the current profile marked, so a reader who narrows the
// list can still see where they are.
func TestFilterNarrowsAndKeepsTheCurrentMark(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"}, Item{Name: "dev-eu"})
	m.Open("dev", "dev")
	press(m, "dev")
	if got := body(m); strings.Contains(got, "prod") {
		t.Errorf("filter kept a row that does not match:\n%s", got)
	}
	if got := body(m); !strings.Contains(got, "* dev") {
		t.Errorf("the connected profile lost its mark:\n%s", got)
	}
}

// The reader is told where the config file goes and what to put in it. An
// empty box would look like a broken program rather than a missing file.
func TestEmptyStateNamesThePathAndASampleFile(t *testing.T) {
	m := New(shared.NewTheme(true))
	m.SetItems(nil, "/home/x/.config/argo-tui/config.yaml")
	m.Open("", "")
	got := body(m)
	for _, want := range []string{
		"no profiles configured",
		"/home/x/.config/argo-tui/config.yaml",
		"profiles:",
		"tokenEnv: ARGO_TOKEN",
		"--demo",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("empty state does not mention %q:\n%s", want, got)
		}
	}
}

// Before the first connection there is no session behind the dialog, so the
// footer must not offer to cancel back to one.
func TestTheFooterOffersQuitBeforeTheFirstConnection(t *testing.T) {
	m := testModel(Item{Name: "dev"})
	m.Open("", "dev")
	if got := m.Hints(); !strings.Contains(got, "esc quit") {
		t.Errorf("Hints() = %q, want esc to quit", got)
	}
	m.Open("dev", "dev")
	if got := m.Hints(); !strings.Contains(got, "esc cancel") {
		t.Errorf("Hints() = %q, want esc to cancel", got)
	}
}

// While a reconnection runs the old connection is already closed and the new
// one is not up. A second choice would have nothing to cancel and would race
// the first, so every key is ignored until it finishes.
func TestKeysAreIgnoredWhileConnecting(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	m.SetConnecting("prod")
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter started a second reconnection")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.IsOpen() {
		t.Error("esc closed the dialog onto a session with no connection")
	}
	if !strings.Contains(body(m), "connecting to prod") {
		t.Errorf("the dialog does not say it is connecting:\n%s", body(m))
	}
}

// A row says which cluster it is, so two profiles that differ only by server
// are not a guess.
func TestRowsShowTheServerAndNamespace(t *testing.T) {
	m := testModel(Item{Name: "dev", Server: "https://argo.dev", Namespace: "argo"})
	m.Open("", "dev")
	got := body(m)
	if !strings.Contains(got, "https://argo.dev") || !strings.Contains(got, "ns=argo") {
		t.Errorf("row does not identify the cluster:\n%s", got)
	}
}

// Reopening must not carry the previous search over: the reader has forgotten
// it, and it hides profiles that are configured.
func TestOpenClearsThePreviousFilter(t *testing.T) {
	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	press(m, "pro")
	m.Close()
	m.Open("dev", "dev")
	if got := body(m); !strings.Contains(got, "dev") || !strings.Contains(got, "prod") {
		t.Errorf("a reopened picker is still narrowed:\n%s", got)
	}
}
