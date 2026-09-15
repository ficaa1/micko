package app

import (
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/ui/logs"
)

// pipe.go hands the retained log lines to another program.
//
// argo-tui is a reader, not a pager: lnav, grep, less and jq are better at
// what they do than any pane this program could grow. The pipe is how the
// buffer leaves without a copy-paste round trip.
//
// Two rules keep it safe. The command is never assembled from server data —
// the reader types it and the lines only ever arrive on standard input, so
// nothing in a log line can become part of a command. And the lines go to a
// private temporary file rather than an inherited pipe, so the child reads a
// real file and argo-tui never has to keep a writer goroutine alive across
// the screen handover.

// pipeDoneMsg reports the finished run back into the update loop.
type pipeDoneMsg struct {
	command string
	tmp     string
	lines   int
	err     error
}

// pipeShell is the interpreter the command is given to. It is a variable so a
// test can substitute something that does not need a real shell.
var pipeShell = "/bin/sh"

// startPipe writes the retained lines to a temporary file and runs the
// command with that file as its standard input, giving it the whole terminal
// until it exits.
func (m *Root) startPipe(intent logs.PipeIntent) tea.Cmd {
	if m.logsView == nil {
		return nil
	}
	lines := m.logsView.RawLines()
	if len(lines) == 0 {
		m.flash = "nothing to pipe: the buffer is empty"
		return nil
	}
	name, err := writeLinesToTemp(lines)
	if err != nil {
		m.flash = "could not write the log lines: " + err.Error()
		return nil
	}
	in, err := os.Open(name)
	if err != nil {
		os.Remove(name)
		m.flash = "could not reopen the log lines: " + err.Error()
		return nil
	}
	c := exec.Command(pipeShell, "-c", intent.Command)
	c.Stdin = in
	n := len(lines)
	cmd := intent.Command
	return tea.ExecProcess(c, func(err error) tea.Msg {
		in.Close()
		return pipeDoneMsg{command: cmd, tmp: name, lines: n, err: err}
	})
}

// writeLinesToTemp puts the lines in a private temporary file and returns its
// path. The file is the child's standard input, so the child reads a real
// file and argo-tui keeps no writer alive across the screen handover.
func writeLinesToTemp(lines []string) (string, error) {
	f, err := os.CreateTemp("", "argo-tui-logs-*.log")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, werr := f.WriteString(strings.Join(lines, "\n") + "\n")
	cerr := f.Close()
	if werr != nil {
		os.Remove(name)
		return "", werr
	}
	if cerr != nil {
		os.Remove(name)
		return "", cerr
	}
	return name, nil
}

// handlePipeDone cleans up the temporary file and reports what happened. A
// failed command must say so: the screen has just come back from another
// program, and a silent return looks like the key did nothing.
func (m *Root) handlePipeDone(msg pipeDoneMsg) tea.Cmd {
	if msg.tmp != "" {
		os.Remove(msg.tmp)
	}
	if msg.err != nil {
		m.flash = msg.command + " failed: " + msg.err.Error()
		return nil
	}
	m.flash = "piped " + plural(msg.lines, "line") + " to " + msg.command
	return nil
}
