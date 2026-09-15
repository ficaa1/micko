package app

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/logs"
)

// `|` opens the editor prefilled with the configured command, and enter hands
// the command to the root.
func TestPipeKeyEmitsTheTypedCommand(t *testing.T) {
	m := logs.NewModel(core.Ref{Namespace: "ns", Name: "wf"}, "pod", "main")
	m.SetPipeCommand("lnav")
	m.Update(tea.KeyPressMsg{Code: '|', Text: "|"})
	for _, r := range " -q" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter emitted no pipe intent")
	}
	intent, ok := cmd().(logs.PipeIntent)
	if !ok {
		t.Fatalf("intent = %T", cmd())
	}
	if intent.Command != "lnav -q" {
		t.Fatalf("command = %q, want the prefilled default plus what was typed", intent.Command)
	}
}

// An empty command runs nothing. Handing the whole terminal to a guess is not
// a place for a default.
func TestEmptyPipeCommandRunsNothing(t *testing.T) {
	m := logs.NewModel(core.Ref{Namespace: "ns", Name: "wf"}, "pod", "main")
	m.Update(tea.KeyPressMsg{Code: '|', Text: "|"})
	for i := 0; i < len("lnav"); i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("an empty command was accepted: %#v", cmd())
	}
}

// The lines reach the child as a file, one per line.
func TestWriteLinesToTemp(t *testing.T) {
	name, err := writeLinesToTemp([]string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(name)
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "first\nsecond\n" {
		t.Fatalf("file content = %q", b)
	}
}

// An empty buffer says so rather than opening another program on nothing.
func TestPipeWithAnEmptyBufferReportsIt(t *testing.T) {
	m := NewRoot(&testkit.FakeReader{}, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second)
	m.logsView = logs.NewModel(core.Ref{Namespace: "ns", Name: "wf"}, "", "main")
	if cmd := m.startPipe(logs.PipeIntent{Command: "lnav"}); cmd != nil {
		t.Fatal("an empty buffer was piped")
	}
	if !strings.Contains(m.flash, "nothing to pipe") {
		t.Fatalf("flash = %q", m.flash)
	}
}

// The temporary file never outlives the run, and a failed command says so.
func TestPipeDoneCleansUpAndReports(t *testing.T) {
	m := NewRoot(&testkit.FakeReader{}, testkit.NewFakeClock(testkit.FixtureEpoch), "ns", time.Second)
	name, err := writeLinesToTemp([]string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	m.handlePipeDone(pipeDoneMsg{command: "lnav", tmp: name, lines: 1})
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("the temporary file survived: %v", err)
	}
	if !strings.Contains(m.flash, "piped 1 line to lnav") {
		t.Fatalf("flash = %q", m.flash)
	}
	m.handlePipeDone(pipeDoneMsg{command: "nope", err: os.ErrNotExist})
	if !strings.Contains(m.flash, "failed") {
		t.Fatalf("a failed command was not reported: %q", m.flash)
	}
}
