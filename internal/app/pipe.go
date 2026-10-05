package app

import (
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/logs"
)

// Piping writes the retained log lines to a temporary file and runs the
// reader's command with that file as standard input. Log data never forms
// part of the command.

// pipeDoneMsg reports the finished run back into the update loop.
type pipeDoneMsg struct {
	command string
	tmp     string
	lines   int
	err     error
}

// pipeShell runs the command; tests replace it.
var pipeShell = "/bin/sh"

// startPipe runs the command with the retained lines as standard input and
// hands it the terminal until it exits.
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

// writeLinesToTemp writes lines to a private temporary file and returns its
// path.
func writeLinesToTemp(lines []string) (string, error) {
	f, err := os.CreateTemp("", "micko-logs-*.log")
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

// handlePipeDone removes the temporary file and reports the result; a failed
// command must say so.
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
