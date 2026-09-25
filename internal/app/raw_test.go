package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/actions"
)

func rawRoot(t *testing.T) (*Root, core.Workflow) {
	t.Helper()
	wf := workflowFixture("deploy-multi-layer-abc")
	f := &testkit.FakeReader{Workflows: map[core.Ref]core.Workflow{wf.Summary.Ref: wf}}
	m := NewRootWithOptions(f, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second,
		actions.Options{ReadOnly: true, Server: "http://127.0.0.1:2746", Profile: "team-prod"})
	m.width, m.height = 120, 30
	m.listView.SetItems([]core.Summary{wf.Summary}, testkit.FixtureEpoch)
	return m, wf
}

func rawKey(m *Root, key string) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return cmd
}

// The bordered pane wraps every content line, so a mouse selection picks up
// border columns. The raw view exists to remove them.
func TestRawViewDropsEveryBorderAndBand(t *testing.T) {
	m, wf := rawRoot(t)
	framed := screen(m)
	if !strings.Contains(framed, "│") {
		t.Fatal("the normal view is expected to be bordered")
	}

	rawKey(m, "f")
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

	rawKey(m, "esc")
	if m.rawMode {
		t.Fatal("esc did not leave the raw view")
	}
}

// f is the full-screen key on every route, logs included: one key, one
// meaning. Follow moved to t (tail). ctrl+f stays as a second way in.
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

// A copy that silently produced nothing is the failure mode here, so the key
// always reports what it did.
func TestCopyReportsWhatItPutOnTheClipboard(t *testing.T) {
	m, wf := rawRoot(t)
	cmd := rawKey(m, "y")
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

// The report is cleared by the next key, or a stale "copied" line would sit
// beside a screen it does not describe.
func TestTheReportClearsOnTheNextKey(t *testing.T) {
	m, _ := rawRoot(t)
	rawKey(m, "y")
	if m.flash == "" {
		t.Fatal("expected a report after copying")
	}
	rawKey(m, "j")
	if m.flash != "" {
		t.Fatalf("the report survived the next key: %q", m.flash)
	}
}

// Without a webURL there is no honest link to build, so the key says so
// instead of opening something wrong.
func TestOpenInBrowserNeedsAConfiguredWebAddress(t *testing.T) {
	m, _ := rawRoot(t)
	var opened []string
	m.openURL = func(u string) error { opened = append(opened, u); return nil }

	rawKey(m, "o")
	if len(opened) != 0 {
		t.Fatalf("opened %v with no web address configured", opened)
	}
	if !strings.Contains(m.flash, "webURL") {
		t.Fatalf("the reason was not stated: %q", m.flash)
	}

	m.SetWebURL("https://argo.example.com/")
	rawKey(m, "o")
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

	cmd := rawKey(m, "o")
	if cmd == nil {
		t.Fatal("a failed open must still return the clipboard command")
	}
	if !strings.Contains(m.flash, "clipboard") {
		t.Fatalf("the fallback was not stated: %q", m.flash)
	}
}

// Actions must never be reachable by accident from the raw view; it is a
// read-only surface over content the reader already sees.
func TestTheRawViewOnlyScrollsAndLeaves(t *testing.T) {
	m, _ := rawRoot(t)
	rawKey(m, "f")
	for _, k := range []string{"a", "s", "u", "r", "enter"} {
		rawKey(m, k)
		if m.actionView != nil && m.actionView.State() != actions.StateIdle {
			t.Fatalf("key %q opened the action pane from the raw view", k)
		}
		if !m.rawMode {
			t.Fatalf("key %q left the raw view", k)
		}
	}
}
