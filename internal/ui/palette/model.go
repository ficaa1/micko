// Package palette is the `:` command palette: a one-line input that moves the
// session around by name, the way k9s does.
//
// The reader types a command, the palette ranks the registered commands
// against it and shows the best few with their aliases and a one-line
// description. Once a known command is followed by a space, the argument
// completes too, from values the root supplies (namespace names, profile
// names).
//
// The palette is a dialog: while it is open it owns every
// key except Ctrl-C, so q and ? type letters. It holds no client, starts
// nothing and knows nothing about what a command does. Enter produces one of
// two messages, RunMsg for a known command or UnknownMsg for anything else,
// and the root turns RunMsg into an effect through its own registry.
//
// Enter runs exactly what was typed. A highlighted suggestion is inserted by
// tab, never by enter: a palette that ran its best guess would turn a typo
// into a namespace switch.
package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// Command is one registry entry as the palette sees it.
type Command struct {
	// Name is the canonical command word. RunMsg always carries it, whichever
	// alias was typed.
	Name string
	// Aliases are the other words that run the command.
	Aliases []string
	// Arg names the command's optional argument ("namespace"). Empty means
	// the command takes none, and the argument stage never opens for it.
	Arg string
	// Desc is the one-line description shown beside the suggestion.
	Desc string
}

// Terms lists every word that names the command, canonical first.
func (c Command) Terms() []string { return append([]string{c.Name}, c.Aliases...) }

// Usage is the command as the suggestion list shows it: its name and, when it
// takes one, its optional argument.
func (c Command) Usage() string {
	if c.Arg == "" {
		return c.Name
	}
	return c.Name + " [" + c.Arg + "]"
}

// RunMsg is the palette's output for a known command: the canonical name and
// the trimmed text after it (empty when none was typed).
type RunMsg struct {
	Name string
	Arg  string
}

// UnknownMsg reports that enter was pressed on text that names no command.
// Input is the trimmed line; the root sanitizes it before showing it.
type UnknownMsg struct{ Input string }

// historyCap bounds the session history. It is a recall aid for the last few
// commands, not a log.
const historyCap = 50

// Model is the palette state.
type Model struct {
	open     bool
	commands []Command
	// args returns the candidate values for a command's argument, by the
	// command's canonical name. It is called on every render, so the values
	// follow whatever the root has learned since the palette opened.
	args func(name string) []string

	// buf is the typed line. The cursor is always at its end: the palette
	// edits one short word at a time, and tab and history replace the whole
	// line anyway.
	buf string
	// sel indexes the suggestion list. It resets to the best match on every
	// edit, so the highlight never points at a row that moved.
	sel int

	// history holds the lines of commands that ran, oldest first. histPos is
	// the entry on screen while recalling; len(history) means the reader's
	// own draft, which draft keeps so walking past the newest entry gives it
	// back.
	history []string
	histPos int
	draft   string

	theme shared.Theme
}

// New builds a closed palette with no commands.
func New(theme shared.Theme) *Model { return &Model{theme: theme} }

// SetTheme restyles the palette.
func (m *Model) SetTheme(theme shared.Theme) { m.theme = theme }

// SetCommands installs the registry, in the order the empty palette lists it.
func (m *Model) SetCommands(cmds []Command) { m.commands = append([]Command(nil), cmds...) }

// SetArgSource installs the function that supplies argument values.
func (m *Model) SetArgSource(fn func(name string) []string) { m.args = fn }

// IsOpen reports whether the palette owns the keyboard.
func (m *Model) IsOpen() bool { return m.open }

// Open shows an empty palette. History survives; the draft does not.
func (m *Model) Open() {
	m.open = true
	m.buf = ""
	m.sel = 0
	m.histPos = len(m.history)
	m.draft = ""
}

// Close hides the palette.
func (m *Model) Close() { m.open = false }

// Parse splits a line into its command word and argument. The argument is the
// trimmed rest of the line; staged reports whether the reader has typed past
// the word, which is what moves completion from commands to arguments.
func Parse(line string) (word, arg string, staged bool) {
	line = strings.TrimLeft(line, " ")
	i := strings.IndexByte(line, ' ')
	if i < 0 {
		return line, "", false
	}
	return line[:i], strings.TrimSpace(line[i+1:]), true
}

// Find resolves a typed word to a command by exact name or alias, ignoring
// case. It never resolves a partial word: that is the rule that keeps enter
// from guessing.
func Find(cmds []Command, word string) (Command, bool) {
	w := strings.ToLower(strings.TrimSpace(word))
	if w == "" {
		return Command{}, false
	}
	for _, c := range cmds {
		for _, t := range c.Terms() {
			if strings.ToLower(t) == w {
				return c, true
			}
		}
	}
	return Command{}, false
}

// suggestion is one row of the list under the input.
type suggestion struct {
	// Value is the whole line tab puts in the input.
	Value string
	// Label is what the row names: a command's usage or an argument value.
	Label string
	// Aliases are a command row's other names; empty for an argument row.
	Aliases []string
	// Desc is the row's description: a command's summary, or the kind of
	// value an argument row offers.
	Desc string
}

// suggestions ranks the candidates for the current line, best first.
//
// Before a space the candidates are the commands. After a known command that
// takes an argument and a space, they are that argument's values. Anything
// else — an unknown word followed by a space, an argument to a command that
// takes none — has no candidates, and the list says so instead.
func (m *Model) suggestions() []suggestion {
	word, arg, staged := Parse(m.buf)
	if !staged {
		cands := make([]Candidate, len(m.commands))
		for i, c := range m.commands {
			cands[i] = Candidate{Terms: c.Terms()}
		}
		ranked := rank(word, cands)
		out := make([]suggestion, 0, len(ranked))
		for _, r := range ranked {
			c := m.commands[r.Index]
			value := r.Term
			if c.Arg != "" {
				// The trailing space opens the argument stage, so one tab
				// both finishes the word and starts completing its value.
				value += " "
			}
			out = append(out, suggestion{Value: value, Label: c.Usage(), Aliases: c.Aliases, Desc: c.Desc})
		}
		return out
	}
	c, ok := Find(m.commands, word)
	if !ok || c.Arg == "" || m.args == nil {
		return nil
	}
	values := m.args(c.Name)
	cands := make([]Candidate, len(values))
	for i, v := range values {
		cands[i] = Candidate{Terms: []string{v}}
	}
	ranked := rank(arg, cands)
	out := make([]suggestion, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, suggestion{Value: word + " " + r.Term, Label: r.Term, Desc: c.Arg})
	}
	return out
}

// Update implements the dialog's key matrix.
//
//	esc              close
//	enter            run the typed line exactly as typed
//	tab              complete the highlighted suggestion
//	up / down        move the highlight
//	ctrl+p / ctrl+n  previous / next line from this session's history
//	backspace        delete the last character; ctrl+u clears the line
//
// Every other key that carries text is typed, q and ? included.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.open {
		return nil
	}
	switch key.String() {
	case "esc":
		m.Close()
		return nil
	case "enter":
		return m.submit()
	case "tab":
		if s := m.suggestions(); len(s) > 0 {
			m.edit(s[m.clampSel(len(s))].Value)
		}
		return nil
	case "up":
		m.moveSel(-1)
		return nil
	case "down":
		m.moveSel(1)
		return nil
	case "ctrl+p":
		m.recall(-1)
		return nil
	case "ctrl+n":
		m.recall(1)
		return nil
	case "backspace":
		if r := []rune(m.buf); len(r) > 0 {
			m.edit(string(r[:len(r)-1]))
		}
		return nil
	case "ctrl+u":
		m.edit("")
		return nil
	}
	if text := key.Text; text != "" && printable(text) {
		m.edit(m.buf + text)
	}
	return nil
}

// submit closes the palette and reports the line. An empty line only closes:
// enter on nothing is the reader changing their mind.
func (m *Model) submit() tea.Cmd {
	line := strings.TrimSpace(m.buf)
	m.Close()
	if line == "" {
		return nil
	}
	word, arg, _ := Parse(line)
	c, ok := Find(m.commands, word)
	if !ok {
		return func() tea.Msg { return UnknownMsg{Input: line} }
	}
	m.remember(line)
	name := c.Name
	return func() tea.Msg { return RunMsg{Name: name, Arg: arg} }
}

// edit replaces the line and puts the highlight back on the best match. Any
// edit also ends a history walk: the recalled line is now the reader's draft.
func (m *Model) edit(s string) {
	m.buf = s
	m.sel = 0
	m.histPos = len(m.history)
	m.draft = s
}

// remember appends a line to the history. A repeat of the newest entry is not
// stored twice, so recalling steps through different commands.
func (m *Model) remember(line string) {
	if n := len(m.history); n > 0 && m.history[n-1] == line {
		return
	}
	m.history = append(m.history, line)
	if len(m.history) > historyCap {
		m.history = m.history[len(m.history)-historyCap:]
	}
}

// recall walks the history. Walking forward past the newest entry returns the
// line the reader was typing before they started walking.
func (m *Model) recall(delta int) {
	if len(m.history) == 0 {
		return
	}
	pos := m.histPos + delta
	if pos < 0 || pos > len(m.history) {
		return
	}
	m.histPos = pos
	if pos == len(m.history) {
		m.buf = m.draft
	} else {
		m.buf = m.history[pos]
	}
	m.sel = 0
}

func (m *Model) moveSel(delta int) {
	n := len(m.suggestions())
	if n == 0 {
		m.sel = 0
		return
	}
	m.sel = (m.clampSel(n) + delta + n) % n
}

func (m *Model) clampSel(n int) int {
	if m.sel < 0 || m.sel >= n {
		return 0
	}
	return m.sel
}

// printable reports whether text is safe to insert: no control characters,
// which would otherwise reach the terminal through the echoed line.
func printable(text string) bool {
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
