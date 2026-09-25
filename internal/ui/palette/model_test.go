package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

var testCommands = []Command{
	{Name: "workflows", Aliases: []string{"wf"}, Desc: "the workflow list"},
	{Name: "ns", Arg: "namespace", Desc: "switch namespace"},
	{Name: "all", Desc: "toggle all namespaces"},
	{Name: "profile", Aliases: []string{"ctx"}, Arg: "profile", Desc: "switch profile"},
	{Name: "quit", Aliases: []string{"q"}, Desc: "leave"},
}

func newTestPalette() *Model {
	m := New(shared.NewTheme(true))
	m.SetCommands(testCommands)
	m.SetArgSource(func(name string) []string {
		switch name {
		case "ns":
			return []string{"argo", "demo", "demo-ml"}
		case "profile":
			return []string{"dev", "prod"}
		}
		return nil
	})
	m.Open()
	return m
}

func press(m *Model, keys ...string) tea.Msg {
	var last tea.Msg
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "ctrl+p":
			msg = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
		case "ctrl+n":
			msg = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
		case "ctrl+u":
			msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
		case " ":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			for _, r := range k {
				if cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)}); cmd != nil {
					last = cmd()
				}
			}
			continue
		}
		if cmd := m.Update(msg); cmd != nil {
			last = cmd()
		}
	}
	return last
}

// Printable keys are text while the palette is open: q and ? are letters of
// a command, and nothing is emitted until enter.
func TestPaletteTypesCommandLetters(t *testing.T) {
	m := newTestPalette()
	if msg := press(m, "q?"); msg != nil {
		t.Fatalf("typing emitted %T", msg)
	}
	if m.Value() != "q?" || !m.IsOpen() {
		t.Fatalf("value = %q open = %v, want q? typed into an open palette", m.Value(), m.IsOpen())
	}
	press(m, " ")
	if m.Value() != "q? " {
		t.Fatalf("space was not typed: %q", m.Value())
	}
	press(m, "backspace", "backspace")
	if m.Value() != "q" {
		t.Fatalf("backspace left %q", m.Value())
	}
	press(m, "ctrl+u")
	if m.Value() != "" {
		t.Fatalf("ctrl+u left %q", m.Value())
	}
}

func TestPaletteEscClosesWithoutRunning(t *testing.T) {
	m := newTestPalette()
	press(m, "wf")
	if msg := press(m, "esc"); msg != nil {
		t.Fatalf("esc emitted %T", msg)
	}
	if m.IsOpen() {
		t.Fatal("esc left the palette open")
	}
}

// Enter runs a command named by its name or any alias, and always reports the
// canonical name with the trimmed argument.
func TestPaletteEnterRunsByNameOrAlias(t *testing.T) {
	for line, want := range map[string]RunMsg{
		"wf":            {Name: "workflows"},
		"WORKFLOWS":     {Name: "workflows"},
		"ctx prod":      {Name: "profile", Arg: "prod"},
		"ns   demo-ml ": {Name: "ns", Arg: "demo-ml"},
		"q":             {Name: "quit"},
	} {
		m := newTestPalette()
		m.SetValue(line)
		got, ok := press(m, "enter").(RunMsg)
		if !ok || got != want {
			t.Errorf("%q ran %+v, want %+v", line, got, want)
		}
		if m.IsOpen() {
			t.Errorf("%q: the palette stayed open after running", line)
		}
	}
}

// Enter never guesses: a partial word, even one with a single suggestion, is
// reported as unknown rather than run as its best match.
func TestPaletteEnterNeverGuesses(t *testing.T) {
	m := newTestPalette()
	press(m, "work")
	if len(m.Suggestions()) == 0 {
		t.Fatal("precondition: work should suggest workflows")
	}
	msg, ok := press(m, "enter").(UnknownMsg)
	if !ok || msg.Input != "work" {
		t.Fatalf("enter on a partial word = %#v, want UnknownMsg{work}", msg)
	}
	if len(m.History()) != 0 {
		t.Fatal("an unknown command was stored in the history")
	}
}

// An empty line only closes.
func TestPaletteEnterOnNothingCloses(t *testing.T) {
	m := newTestPalette()
	if msg := press(m, "enter"); msg != nil {
		t.Fatalf("enter on nothing emitted %T", msg)
	}
	if m.IsOpen() {
		t.Fatal("enter on nothing left the palette open")
	}
}

// Tab completes the highlighted command. A command with an argument gets a
// trailing space, which starts the argument stage, so one more tab completes
// the value.
func TestPaletteTabCompletesCommandThenArgument(t *testing.T) {
	m := newTestPalette()
	press(m, "n", "tab")
	if m.Value() != "ns " {
		t.Fatalf("tab on n = %q, want %q", m.Value(), "ns ")
	}
	press(m, "ml")
	s := m.Suggestions()
	if len(s) == 0 || s[0].Label != "demo-ml" {
		t.Fatalf("argument suggestions for ml = %+v, want demo-ml first", s)
	}
	press(m, "tab")
	if m.Value() != "ns demo-ml" {
		t.Fatalf("tab on the argument = %q", m.Value())
	}
	if got, _ := press(m, "enter").(RunMsg); got != (RunMsg{Name: "ns", Arg: "demo-ml"}) {
		t.Fatalf("enter after completion ran %+v", got)
	}
}

// Up and down move the highlight, wrapping, and tab takes the highlighted row.
func TestPaletteArrowsChooseTheSuggestion(t *testing.T) {
	m := newTestPalette()
	press(m, "ns ")
	press(m, "down")
	if m.Selected() != 1 {
		t.Fatalf("down selected %d", m.Selected())
	}
	press(m, "up", "up")
	if m.Selected() != 2 {
		t.Fatalf("up from the top should wrap to the last row, got %d", m.Selected())
	}
	press(m, "tab")
	if m.Value() != "ns demo-ml" {
		t.Fatalf("tab took %q, want the highlighted demo-ml", m.Value())
	}
}

// A typed alias is kept on completion: the reader chose that vocabulary.
func TestPaletteTabKeepsTheTypedAlias(t *testing.T) {
	m := newTestPalette()
	press(m, "ct", "tab")
	if m.Value() != "ctx " {
		t.Fatalf("tab on ct = %q, want ctx and the argument stage", m.Value())
	}
	press(m, "tab")
	if m.Value() != "ctx dev" {
		t.Fatalf("argument tab = %q", m.Value())
	}
}

// ctrl+p walks back through the commands that ran, newest first, and ctrl+n
// walks forward, ending on the line the reader was typing.
func TestPaletteHistory(t *testing.T) {
	m := newTestPalette()
	for _, line := range []string{"wf", "ns demo", "ns demo", "all"} {
		m.Open()
		m.SetValue(line)
		press(m, "enter")
	}
	if got := strings.Join(m.History(), "|"); got != "wf|ns demo|all" {
		t.Fatalf("history = %q, want repeats collapsed", got)
	}
	m.Open()
	press(m, "pro")
	press(m, "ctrl+p")
	if m.Value() != "all" {
		t.Fatalf("first ctrl+p = %q", m.Value())
	}
	press(m, "ctrl+p", "ctrl+p", "ctrl+p")
	if m.Value() != "wf" {
		t.Fatalf("ctrl+p past the oldest = %q, want it to stay on wf", m.Value())
	}
	press(m, "ctrl+n")
	if m.Value() != "ns demo" {
		t.Fatalf("ctrl+n = %q", m.Value())
	}
	press(m, "ctrl+n", "ctrl+n")
	if m.Value() != "pro" {
		t.Fatalf("ctrl+n past the newest = %q, want the draft back", m.Value())
	}
}

// Suggestions say why they are empty, so an empty list never looks broken.
func TestPaletteEmptyStatesExplainThemselves(t *testing.T) {
	for line, want := range map[string]string{
		"zzz":      "no command matches",
		"zzz ":     "no command named",
		"all x":    "all takes no argument",
		"ns nope!": "no known namespace matches",
	} {
		m := newTestPalette()
		m.SetValue(line)
		body := strings.Join(m.BodyLines(80, MaxRows), "\n")
		if !strings.Contains(body, want) {
			t.Errorf("%q renders %q, want %q", line, body, want)
		}
	}
}

// The highlighted row carries a marker, so it is visible with no color.
func TestPaletteRenderMarksTheHighlightWithoutColor(t *testing.T) {
	m := newTestPalette()
	lines := m.BodyLines(60, MaxRows)
	if !strings.HasPrefix(lines[0], ": _") {
		t.Fatalf("input line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "› workflows") || !strings.Contains(lines[1], "wf") ||
		!strings.Contains(lines[1], "the workflow list") {
		t.Fatalf("first row = %q, want the marked workflows row with alias and description", lines[1])
	}
	if !strings.Contains(strings.Join(lines, "\n"), "ns [namespace]") {
		t.Fatal("a command with an argument must show its usage")
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 60 {
			t.Fatalf("line %q is %d cells wide, want at most 60", l, w)
		}
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "─") {
		t.Fatalf("last line = %q, want the rule that separates the pane", last)
	}
}

// The row window follows the highlight past the rows shown.
func TestPaletteWindowFollowsTheHighlight(t *testing.T) {
	m := newTestPalette()
	press(m, "down", "down", "down")
	lines := m.BodyLines(80, 2)
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want input, two rows, rule", len(lines))
	}
	if !strings.HasPrefix(lines[2], "› profile") {
		t.Fatalf("window = %q, want the highlighted profile row shown", lines[1:3])
	}
}

// Typed text reaches the screen sanitized.
func TestPaletteSanitizesTheEchoedLine(t *testing.T) {
	m := newTestPalette()
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "\x1b[31m"})
	m.SetValue(m.Value() + "ok\x1b]0;t\x07")
	for _, l := range m.BodyLines(80, MaxRows) {
		if strings.ContainsRune(l, 0x1b) {
			t.Fatalf("escape reached the render: %q", l)
		}
	}
}
