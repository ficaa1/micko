package profiles

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/ui/shared"
)

const configPath = "/home/x/.config/micko/config.yaml"

func testModel(items ...Item) *Model {
	m := New(shared.NewTheme(true))
	m.SetItems(items, configPath)
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

// rows are the dialog's profile rows as drawn, the connected one starred.
func rows(m *Model) []string {
	var out []string
	for _, l := range strings.Split(body(m), "\n") {
		if l = strings.TrimRight(l, " "); strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "* ") {
			out = append(out, l)
		}
	}
	return out
}

// Enter emits only a configured profile other than the connected one; typed
// text narrows the list and names nothing to connect to; esc and enter on the
// connected profile close.
func TestPicker(t *testing.T) {
	three := []Item{{Name: "dev"}, {Name: "prod"}, {Name: "Dev-EU"}}
	cases := []struct {
		name       string
		current    string
		cursor     string
		keys       string
		wantRows   []string
		wantSwitch string
		wantOpen   bool
	}{
		{"starts on the cursor profile", "", "prod", "", []string{"  dev", "  prod", "  Dev-EU"}, "", true},
		{"enter switches to the row", "dev", "dev", "down enter", nil, "prod", true},
		{"enter before the first connection connects", "", "prod", "enter", nil, "prod", true},
		{"enter on the connected profile closes", "dev", "dev", "enter", nil, "", false},
		{"up wraps", "dev", "dev", "up enter", nil, "Dev-EU", true},
		{"ctrl+n and ctrl+p", "dev", "dev", "ctrl+n ctrl+n ctrl+p enter", nil, "prod", true},
		{"typing narrows and keeps the mark", "dev", "dev", "d e v", []string{"* dev", "  Dev-EU"}, "", true},
		{"typing ignores case", "dev", "dev", "P R O", []string{"  prod"}, "", true},
		{"backspace widens", "dev", "dev", "p r o backspace backspace backspace", []string{"* dev", "  prod", "  Dev-EU"}, "", true},
		{"typed text that matches nothing connects nothing", "dev", "dev", "z z z enter", []string{}, "", false},
		{"esc closes", "dev", "dev", "down esc", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testModel(three...)
			m.Open(c.current, c.cursor)
			var got string
			if cmd := keys(m, c.keys); cmd != nil {
				msg, ok := cmd().(SwitchMsg)
				if !ok {
					t.Fatalf("command = %#v, want a switch", cmd())
				}
				got = msg.Profile
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
		})
	}
}

// Every state of the dialog says what it is: rows that tell clusters apart, a
// missing file with where to write one, a config error, a filter with no
// match, a running reconnection and a reopened picker.
func TestStates(t *testing.T) {
	cases := []struct {
		name  string
		items []Item
		setup func(*Model)
		want  []string
		not   []string
	}{
		{"rows identify the cluster", []Item{
			{Name: "dev", Server: "https://argo.dev", Namespace: "argo"},
			{Name: "production", Server: "https://argo.prod.example", Namespace: "wf"},
		}, func(m *Model) { m.Open("", "dev") }, []string{
			"choose a profile",
			"  dev         https://argo.dev           ns=argo",
			"  production  https://argo.prod.example  ns=wf",
			configPath,
		}, nil},
		{"no profiles", nil, func(m *Model) { m.Open("", "") }, []string{
			"no profiles configured", "write them to:\n  " + configPath,
			"  profiles:\n    dev:\n      server: https://argo.example.com\n      namespace: argo\n      tokenEnv: ARGO_TOKEN",
			"micko --demo looks around without a cluster",
		}, nil},
		{"config error", nil, func(m *Model) { m.SetError("profiles: not a map"); m.Open("", "") },
			[]string{"config: profiles: not a map", "no profiles configured"}, nil},
		{"no match", []Item{{Name: "dev"}}, func(m *Model) { m.Open("dev", "dev"); keys(m, "z z z") },
			[]string{"switch profile — current: dev", "filter: zzz_", "no profile matches zzz"}, []string{"* dev"}},
		{"connecting", []Item{{Name: "dev"}, {Name: "prod"}}, func(m *Model) { m.Open("dev", "dev"); m.SetConnecting("prod") },
			[]string{"connecting to prod…", "a port-forward can take a few seconds"}, []string{"filter:"}},
		{"reopened", []Item{{Name: "dev"}, {Name: "prod"}}, func(m *Model) {
			m.Open("dev", "dev")
			keys(m, "p r o")
			m.Close()
			m.Open("dev", "dev")
		}, []string{"filter: _", "* dev", "  prod"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testModel(c.items...)
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

// The footer offers only what the keys do: quit before the first connection,
// cancel after it, and nothing while a reconnection runs, when every key is
// ignored.
func TestHintsAndConnecting(t *testing.T) {
	cases := []struct {
		name       string
		current    string
		connecting string
		want       string
	}{
		{"before the first connection", "", "", "type to filter  ↑↓ move  enter connect  esc quit"},
		{"connected", "dev", "", "type to filter  ↑↓ move  enter switch  esc cancel"},
		{"connecting", "dev", "prod", "connecting to prod…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
			m.Open(c.current, "dev")
			if c.connecting != "" {
				m.SetConnecting(c.connecting)
			}
			if got := m.Hints(); got != c.want {
				t.Errorf("Hints() = %q, want %q", got, c.want)
			}
		})
	}

	m := testModel(Item{Name: "dev"}, Item{Name: "prod"})
	m.Open("dev", "dev")
	m.SetConnecting("prod")
	if cmd := keys(m, "down enter esc"); cmd != nil || !m.IsOpen() || !m.Connecting() {
		t.Errorf("keys acted during a reconnection: open %v connecting %v", m.IsOpen(), m.Connecting())
	}
}
