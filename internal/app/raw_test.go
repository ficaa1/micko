package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
)

func rawRoot(t *testing.T) (*Root, core.Workflow) {
	t.Helper()
	wf := workflowFixture("deploy-multi-layer-abc")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := NewRoot(f, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second,
		actions.Options{ReadOnly: true, Server: "http://127.0.0.1:2746", Profile: "team-prod"})
	m.width, m.height = 120, 30
	m.listView.SetItems([]core.Summary{wf.Summary}, testkit.FixtureEpoch)
	return m, wf
}

// The raw view drops the border and the bands.
func TestRawViewDropsEveryBorderAndBand(t *testing.T) {
	m, wf := rawRoot(t)
	framed := screen(m)
	if !strings.Contains(framed, "│") {
		t.Fatal("the normal view is expected to be bordered")
	}

	keys(m, "f")
	if !m.rawMode {
		t.Fatal("f did not enter the raw view")
	}
	raw := screen(m)
	if strings.Contains(raw, "│") || strings.Contains(raw, "┌") {
		t.Fatalf("the raw view still draws a border:\n%s", raw)
	}
	if !strings.Contains(raw, wf.Summary.Ref.Name) {
		t.Fatalf("the raw view lost the content:\n%s", raw)
	}
	if !strings.Contains(raw, "f or esc leaves") {
		t.Fatal("the raw view must say how to leave it")
	}

	keys(m, "esc")
	if m.rawMode {
		t.Fatal("esc did not leave the raw view")
	}
}

// f and ctrl+f enter the raw view from the logs too.
func TestFEntersTheRawViewFromLogs(t *testing.T) {
	m, wf := rawRoot(t)
	m.Update(OpenLogsMsg{Ref: wf.Summary.Ref, Container: "main"})
	if m.route != RouteLogs {
		t.Fatalf("route = %v, want logs", m.route)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !m.rawMode {
		t.Fatal("f must enter the raw view from the logs route")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})

	_, _ = m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.rawMode {
		t.Fatal("ctrl+f must still enter the raw view")
	}
}

// y copies the selection and says so.
func TestCopyReportsWhatItPutOnTheClipboard(t *testing.T) {
	m, wf := rawRoot(t)
	cmd := keys(m, "y")
	if cmd == nil {
		t.Fatal("y produced no clipboard command")
	}
	if !strings.Contains(m.flash, "copied") {
		t.Fatalf("y did not report its result: %q", m.flash)
	}
	if got := m.copyText(); got != wf.Summary.Ref.Name {
		t.Fatalf("copied %q, want the selected workflow name", got)
	}
}

func TestTheReportClearsOnTheNextKey(t *testing.T) {
	m, _ := rawRoot(t)
	keys(m, "y")
	if m.flash == "" {
		t.Fatal("expected a report after copying")
	}
	keys(m, "j")
	if m.flash != "" {
		t.Fatalf("the report survived the next key: %q", m.flash)
	}
}

// o needs a webURL and says so without one.
func TestOpenInBrowserNeedsAConfiguredWebAddress(t *testing.T) {
	m, _ := rawRoot(t)
	var opened []string
	m.openURL = func(u string) error { opened = append(opened, u); return nil }

	keys(m, "o")
	if len(opened) != 0 {
		t.Fatalf("opened %v with no web address configured", opened)
	}
	if !strings.Contains(m.flash, "webURL") {
		t.Fatalf("the reason was not stated: %q", m.flash)
	}

	m.SetWebURL("https://argo.example.com/")
	keys(m, "o")
	want := "https://argo.example.com/workflows/ns/deploy-multi-layer-abc"
	if len(opened) != 1 || opened[0] != want {
		t.Fatalf("opened %v, want [%s]", opened, want)
	}
}

// If no browser starts, the link must still reach the reader.
func TestAFailedBrowserOpenStillCopiesTheLink(t *testing.T) {
	m, _ := rawRoot(t)
	m.SetWebURL("https://argo.example.com")
	m.openURL = func(string) error { return errors.New("no browser") }

	cmd := keys(m, "o")
	if cmd == nil {
		t.Fatal("a failed open must still return the clipboard command")
	}
	if !strings.Contains(m.flash, "clipboard") {
		t.Fatalf("the fallback was not stated: %q", m.flash)
	}
}

// The raw view only scrolls and leaves; no key there starts an action.
func TestTheRawViewOnlyScrollsAndLeaves(t *testing.T) {
	m, _ := rawRoot(t)
	keys(m, "f")
	for _, k := range []string{"a", "s", "u", "r", "enter"} {
		keys(m, k)
		if m.actionView != nil && m.actionView.State() != actions.StateIdle {
			t.Fatalf("key %q opened the action pane from the raw view", k)
		}
		if !m.rawMode {
			t.Fatalf("key %q left the raw view", k)
		}
	}
}
