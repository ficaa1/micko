package app

import (
	"os"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/logs"
)

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
	m := testRoot(t, fixtureReader())
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
	m := testRoot(t, fixtureReader())
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
