package namespaces

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

func newPicker(current string, names ...string) *Model {
	m := New(shared.NewTheme(true))
	m.SetSize(40, 20)
	m.Open(current)
	m.SetNames(names, "test", nil)
	return m
}

var namedKeys = map[string]tea.KeyPressMsg{
	"enter":     {Code: tea.KeyEnter},
	"esc":       {Code: tea.KeyEscape},
	"up":        {Code: tea.KeyUp},
	"down":      {Code: tea.KeyDown},
	"backspace": {Code: tea.KeyBackspace},
	"ctrl+n":    {Code: 'n', Mod: tea.ModCtrl},
	"ctrl+p":    {Code: 'p', Mod: tea.ModCtrl},
}

// keys sends each space-separated key and returns the last command.
func keys(m *Model, seq string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range strings.Fields(seq) {
		msg, ok := namedKeys[k]
		if !ok {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		cmd = m.Update(msg)
	}
	return cmd
}

func body(m *Model) string { return ansi.Strip(strings.Join(m.BodyLines(), "\n")) }

// rows are the dialog's namespace rows as drawn, the current one starred.
func rows(m *Model) []string {
	var out []string
	for _, l := range strings.Split(body(m), "\n") {
		if l = strings.TrimRight(l, " "); strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "* ") {
			out = append(out, l)
		}
	}
	return out
}

// The cursor starts on the namespace in use, typing narrows the list, the
// arrows move and wrap, enter switches to the row or to the typed text, and
// enter on the current namespace or esc only closes.
func TestPicker(t *testing.T) {
	cases := []struct {
		name       string
		current    string
		names      []string
		keys       string
		wantRows   []string
		wantCursor string
		wantSwitch string
		wantOpen   bool
	}{
		{"starts on the current namespace", "beta", []string{"alpha", "beta", "gamma"}, "",
			[]string{"  alpha", "* beta", "  gamma"}, "beta", "", true},
		{"the current namespace is always offered", "zeta", []string{"alpha"}, "",
			[]string{"  alpha", "* zeta"}, "zeta", "", true},
		{"typing narrows", "alpha", []string{"batch-cd-prd", "batch-cd-tst", "reports-prd"}, "t s t",
			[]string{"  batch-cd-tst"}, "batch-cd-tst", "", true},
		{"typing ignores case", "alpha", []string{"batch-cd-prd", "Batch-CD-tst"}, "c d - t",
			[]string{"  Batch-CD-tst"}, "Batch-CD-tst", "", true},
		{"backspace widens", "alpha", []string{"batch-cd-prd", "batch-cd-tst", "reports-prd"}, "t s t backspace backspace backspace",
			[]string{"* alpha", "  batch-cd-prd", "  batch-cd-tst", "  reports-prd"}, "", "", true},
		{"down wraps", "gamma", []string{"alpha", "beta", "gamma"}, "down", nil, "alpha", "", true},
		{"up wraps", "alpha", []string{"alpha", "beta", "gamma"}, "up", nil, "gamma", "", true},
		{"ctrl+n and ctrl+p", "alpha", []string{"alpha", "beta", "gamma"}, "ctrl+n ctrl+n ctrl+p", nil, "beta", "", true},
		{"enter switches to the row", "alpha", []string{"alpha", "beta"}, "down enter", nil, "", "beta", false},
		{"enter switches to a typed namespace", "alpha", []string{"alpha", "beta"}, "b r a n d - n e w enter", nil, "", "brand-new", false},
		{"enter on the current namespace only closes", "alpha", []string{"alpha", "beta"}, "enter", nil, "", "", false},
		{"esc cancels", "alpha", []string{"alpha", "beta"}, "down esc", nil, "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPicker(c.current, c.names...)
			var got string
			if cmd := keys(m, c.keys); cmd != nil {
				msg, ok := cmd().(SwitchMsg)
				if !ok {
					t.Fatalf("command = %#v, want a switch", cmd())
				}
				got = msg.Namespace
			}
			if got != c.wantSwitch {
				t.Errorf("switch = %q, want %q", got, c.wantSwitch)
			}
			if m.IsOpen() != c.wantOpen {
				t.Errorf("open = %v, want %v", m.IsOpen(), c.wantOpen)
			}
			if c.wantRows != nil && !slices.Equal(rows(m), c.wantRows) {
				t.Errorf("rows = %q, want %q", rows(m), c.wantRows)
			}
			if c.wantCursor != "" && m.Selected() != c.wantCursor {
				t.Errorf("cursor on %q, want %q", m.Selected(), c.wantCursor)
			}
		})
	}
}

// Every state of the list says what it is: a denied request must not look
// like a cluster with no namespaces, and the typed entry stays usable.
func TestStates(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Model)
		want  []string
		not   []string
	}{
		{"loading again", func(m *Model) { m.SetNames([]string{"beta"}, "from the cluster", nil); m.SetLoading() },
			[]string{"asking the server…"}, []string{"from the cluster"}},
		{"listed", func(m *Model) { m.SetNames([]string{"beta"}, "from the cluster", nil) }, []string{"  beta", "from the cluster"}, nil},
		{"refused", func(m *Model) {
			m.SetError("namespaces are forbidden for this token")
			m.SetNames(nil, "", []string{"beta"})
		}, []string{"the server would not list namespaces: namespaces are forbidden for this token", "type the namespace and press enter", "  beta"}, nil},
		{"nothing listed", func(m *Model) { m.Open(""); m.SetNames(nil, "", nil) },
			[]string{"no namespace names available — type one and press enter"}, nil},
		{"no match", func(m *Model) { m.SetNames([]string{"beta"}, "", nil); keys(m, "z z") },
			[]string{"filter: zz_", "no match — enter uses zz as typed"}, []string{"beta"}},
		{"reopened", func(m *Model) { m.SetNames([]string{"beta"}, "", nil); keys(m, "z z esc"); m.Open("alpha") },
			[]string{"filter: _", "  beta"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(shared.NewTheme(true))
			m.SetSize(80, 20)
			m.Open("alpha")
			c.setup(m)
			got := body(m)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("dialog lacks %q:\n%s", want, got)
				}
			}
			for _, bad := range c.not {
				if strings.Contains(got, bad) {
					t.Errorf("dialog shows %q:\n%s", bad, got)
				}
			}
		})
	}
}
