package palette

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

var testCommands = []Command{
	{Name: "workflows", Aliases: []string{"wf"}, Desc: "the workflow list"},
	{Name: "ns", Arg: "namespace", Desc: "switch namespace"},
	{Name: "all", Desc: "toggle all namespaces"},
	{Name: "profile", Aliases: []string{"ctx"}, Arg: "profile", Desc: "switch profile"},
	{Name: "quit", Aliases: []string{"q"}, Desc: "leave"},
}

// newPalette is an open palette over cmds, with namespace and profile values.
func newPalette(cmds []Command) *Model {
	m := New(shared.NewTheme(true))
	m.SetCommands(cmds)
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

var namedKeys = map[string]tea.KeyPressMsg{
	"enter":     {Code: tea.KeyEnter},
	"esc":       {Code: tea.KeyEscape},
	"tab":       {Code: tea.KeyTab},
	"up":        {Code: tea.KeyUp},
	"down":      {Code: tea.KeyDown},
	"backspace": {Code: tea.KeyBackspace},
	"space":     {Code: tea.KeySpace, Text: " "},
	"ctrl+p":    {Code: 'p', Mod: tea.ModCtrl},
	"ctrl+n":    {Code: 'n', Mod: tea.ModCtrl},
	"ctrl+u":    {Code: 'u', Mod: tea.ModCtrl},
}

// press sends each key; a word that names no key is typed letter by letter.
// It returns the message of the last key that emitted one.
func press(m *Model, keys ...string) tea.Msg {
	var last tea.Msg
	send := func(k tea.KeyPressMsg) {
		if cmd := m.Update(k); cmd != nil {
			last = cmd()
		}
	}
	for _, k := range keys {
		if msg, ok := namedKeys[k]; ok {
			send(msg)
			continue
		}
		for _, r := range k {
			send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	return last
}

// input is the palette's input line as drawn.
func input(m *Model) string { return ansi.Strip(m.BodyLines(80, MaxRows)[0]) }

// listed are the labels of the suggestion rows, top to bottom.
func listed(m *Model) []string {
	lines := m.BodyLines(200, MaxRows)
	var out []string
	for _, l := range lines[1 : len(lines)-1] {
		if f := strings.Fields(strings.TrimPrefix(ansi.Strip(l), "›")); len(f) > 0 && !strings.HasPrefix(l, "  no ") {
			out = append(out, f[0])
		}
	}
	return out
}

// Printable keys are typed while the palette is open, q and ? included, and
// nothing is emitted until enter; backspace and ctrl+u edit the line.
func TestTyping(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"q?"}, ": q?_"},
		{[]string{"q?", "space"}, ": q? _"},
		{[]string{"q?", "space", "backspace", "backspace"}, ": q_"},
		{[]string{"q?", "ctrl+u"}, ": _"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.keys, " "), func(t *testing.T) {
			m := newPalette(testCommands)
			if msg := press(m, c.keys...); msg != nil {
				t.Errorf("typing emitted %#v", msg)
			}
			if got := input(m); got != c.want {
				t.Errorf("input = %q, want %q", got, c.want)
			}
			if !m.IsOpen() {
				t.Error("typing closed the palette")
			}
		})
	}
}

// Enter runs a command named by its name or any alias with the trimmed
// argument, never guesses from a partial word, and closes; esc and an empty
// line close without running anything.
func TestEnter(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want tea.Msg
	}{
		{"alias", []string{"wf", "enter"}, RunMsg{Name: "workflows"}},
		{"name in capitals", []string{"WORKFLOWS", "enter"}, RunMsg{Name: "workflows"}},
		{"alias with an argument", []string{"ctx", "space", "prod", "enter"}, RunMsg{Name: "profile", Arg: "prod"}},
		{"argument trimmed", []string{"ns", "space", "space", "space", "demo-ml", "space", "enter"}, RunMsg{Name: "ns", Arg: "demo-ml"}},
		{"short alias", []string{"q", "enter"}, RunMsg{Name: "quit"}},
		{"partial word", []string{"work", "enter"}, UnknownMsg{Input: "work"}},
		{"empty line", []string{"enter"}, nil},
		{"esc", []string{"wf", "esc"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPalette(testCommands)
			if got := press(m, c.keys...); got != c.want {
				t.Errorf("emitted %#v, want %#v", got, c.want)
			}
			if m.IsOpen() {
				t.Error("the palette stayed open")
			}
		})
	}
}

// Tab completes the highlighted row, keeping a typed alias; a command with
// an argument gets a trailing space, so the next tab completes the value. The
// arrows move the highlight and wrap.
func TestTabCompletion(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"n", "tab"}, ": ns _"},
		{[]string{"n", "tab", "ml", "tab"}, ": ns demo-ml_"},
		{[]string{"ct", "tab"}, ": ctx _"},
		{[]string{"ct", "tab", "tab"}, ": ctx dev_"},
		{[]string{"ns", "space", "down", "tab"}, ": ns demo_"},
		{[]string{"ns", "space", "up", "tab"}, ": ns demo-ml_"},
		{[]string{"ns", "space", "down", "down", "down", "tab"}, ": ns argo_"},
		{[]string{"all", "space", "tab"}, ": all _"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.keys, " "), func(t *testing.T) {
			m := newPalette(testCommands)
			press(m, c.keys...)
			if got := input(m); got != c.want {
				t.Errorf("input = %q, want %q", got, c.want)
			}
		})
	}
}

// ctrl+p walks back through the commands that ran, newest first, with a
// repeat stored once and an unknown command not at all; ctrl+n walks forward
// to the line the reader was typing.
func TestHistory(t *testing.T) {
	m := newPalette(testCommands)
	for _, line := range [][]string{{"wf"}, {"ns", "space", "demo"}, {"ns", "space", "demo"}, {"all"}, {"work"}} {
		m.Open()
		press(m, append(line, "enter")...)
	}
	m.Open()
	press(m, "pro")
	steps := []struct {
		key  string
		want string
	}{
		{"ctrl+p", ": all_"},
		{"ctrl+p", ": ns demo_"},
		{"ctrl+p", ": wf_"},
		{"ctrl+p", ": wf_"},
		{"ctrl+n", ": ns demo_"},
		{"ctrl+n", ": all_"},
		{"ctrl+n", ": pro_"},
	}
	for i, s := range steps {
		press(m, s.key)
		if got := input(m); got != s.want {
			t.Fatalf("step %d, %s: input = %q, want %q", i, s.key, got, s.want)
		}
	}
}

// The list ranks an exact match, then a prefix, then a later word's start,
// then a substring, then a subsequence in order; ties go to the shorter term,
// then the registry order; case is ignored and an empty line lists the
// registry as it is.
func TestRanking(t *testing.T) {
	cmds := func(names ...string) []Command {
		var out []Command
		for _, n := range names {
			out = append(out, Command{Name: n})
		}
		return out
	}
	cases := []struct {
		name  string
		cmds  []Command
		typed string
		want  []string
	}{
		{"tiers", cmds("xwxoxrxk", "rework", "cron-work", "workflows", "work", "help", "krow"), "work",
			[]string{"work", "workflows", "cron-work", "rework", "xwxoxrxk"}},
		{"ties", cmds("profile-long", "profile", "proxy", "prune"), "pr",
			[]string{"proxy", "prune", "profile", "profile-long"}},
		{"case", cmds("zeta", "alpha"), "ALP", []string{"alpha"}},
		{"empty line", cmds("zeta", "alpha"), "", []string{"zeta", "alpha"}},
		{"alias", []Command{{Name: "cwf-archive"}, {Name: "workflows", Aliases: []string{"wf"}}}, "wf",
			[]string{"workflows", "cwf-archive"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPalette(c.cmds)
			press(m, c.typed)
			if got := listed(m); !slices.Equal(got, c.want) {
				t.Errorf("listed %v, want %v", got, c.want)
			}
		})
	}
}

// An empty list says why, so it never looks broken.
func TestEmptyStates(t *testing.T) {
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"zzz"}, "no command matches “zzz” — esc closes"},
		{[]string{"zzz", "space"}, "no command named “zzz”"},
		{[]string{"all", "space", "x"}, "all takes no argument"},
		{[]string{"ns", "space", "nope!"}, "no known namespace matches “nope!”"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.keys, " "), func(t *testing.T) {
			m := newPalette(testCommands)
			press(m, c.keys...)
			if got := ansi.Strip(m.BodyLines(80, MaxRows)[1]); got != "  "+c.want {
				t.Errorf("list = %q, want %q", got, c.want)
			}
		})
	}
}

// The highlighted row carries a marker, so it is visible with no color; rows
// show usage, aliases and description, fit the width, and a rule ends the
// palette.
func TestRender(t *testing.T) {
	m := newPalette(testCommands)
	lines := m.BodyLines(60, MaxRows)
	want := []string{
		": _",
		"› workflows          wf   the workflow list",
		"  ns [namespace]          switch namespace",
		"  all                     toggle all namespaces",
		"  profile [profile]  ctx  switch profile",
		"  quit               q    leave",
		strings.Repeat("─", 60),
	}
	for i, l := range lines {
		if i < len(want) && ansi.Strip(l) != want[i] {
			t.Errorf("line %d = %q, want %q", i, ansi.Strip(l), want[i])
		}
		if w := ansi.StringWidth(l); w > 60 {
			t.Errorf("line %d is %d cells wide, want at most 60", i, w)
		}
	}
	if len(lines) != len(want) {
		t.Errorf("%d lines, want %d", len(lines), len(want))
	}
}

// The row window follows the highlight past the rows shown.
func TestWindowFollowsTheHighlight(t *testing.T) {
	m := newPalette(testCommands)
	press(m, "down", "down", "down")
	lines := m.BodyLines(80, 2)
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want input, two rows, rule", len(lines))
	}
	if got := ansi.Strip(lines[2]); !strings.HasPrefix(got, "› profile") {
		t.Fatalf("window = %q, want the highlighted profile row shown", lines[1:3])
	}
}

// Control characters are never typed, so they reach neither the echoed line
// nor the command enter reports.
func TestControlTextIsNotTyped(t *testing.T) {
	m := newPalette(testCommands)
	press(m, "ok")
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "\x1b[31m"})
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "\x1b]0;t\x07"})
	if got := input(m); got != ": ok_" {
		t.Errorf("input = %q, want only the printable text", got)
	}
	if got := press(m, "enter"); got != (UnknownMsg{Input: "ok"}) {
		t.Errorf("enter = %#v, want the typed text alone", got)
	}
}
