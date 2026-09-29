package detail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// demoWorkflow is one workflow of the demo dataset, at FixtureEpoch.
func demoWorkflow(t testing.TB, name string) core.Workflow {
	t.Helper()
	r := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
	for ref, wf := range r.Workflows {
		if ref.Name == name {
			return wf
		}
	}
	t.Fatalf("no demo workflow %q", name)
	return core.Workflow{}
}

// demoNames is every workflow of the demo dataset.
func demoNames() []string {
	var out []string
	for ref := range testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch)).Workflows {
		out = append(out, ref.Name)
	}
	return out
}

// workflowModel is a w×h pane in the plain theme showing wf on its summary.
func workflowModel(wf core.Workflow, w, h int) *Model {
	m := New()
	m.SetTheme(shared.NewTheme(true))
	m.SetSize(w, h)
	m.SetWorkflow(wf, testkit.FixtureEpoch)
	return m
}

// demoModel is the nodes tab of one demo workflow in the plain theme.
func demoModel(t testing.TB, name string, w, h int) *Model {
	t.Helper()
	m := workflowModel(demoWorkflow(t, name), w, h)
	press(m, "tab")
	return m
}

// sectionModel is one section of a demo workflow in the plain theme.
func sectionModel(t testing.TB, name, section string, w, h int) *Model {
	t.Helper()
	m := demoModel(t, name, w, h)
	if !m.SetSection(section) {
		t.Fatalf("no %s section", section)
	}
	return m
}

// namedKeys are the non-printable keys the tests press, by the name Update sees.
var namedKeys = map[string]tea.KeyPressMsg{
	"esc":       {Code: tea.KeyEscape},
	"enter":     {Code: tea.KeyEnter},
	"backspace": {Code: tea.KeyBackspace},
	"space":     {Code: tea.KeySpace, Text: " "},
	"tab":       {Code: tea.KeyTab},
	"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
	"left":      {Code: tea.KeyLeft},
	"right":     {Code: tea.KeyRight},
	"pgdown":    {Code: tea.KeyPgDown},
}

// press sends one key through Update and returns the intent it emits.
func press(m *Model, key string) tea.Cmd {
	if msg, ok := namedKeys[key]; ok {
		return m.Update(msg)
	}
	return m.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
}

// typeText presses each character of s.
func typeText(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// body is the pane body as plain text.
func body(m *Model) string { return ansi.Strip(strings.Join(m.BodyLines(), "\n")) }

// cursorTo puts the nodes tab's cursor on the row with this display name.
func cursorTo(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, r := range m.nodes {
		if r.Row.DisplayName == name {
			m.nodeCursor = i
			return
		}
	}
	t.Fatalf("no row %q among %v", name, rowNames(m))
}

// rowNames is the nodes tab's rows by display name.
func rowNames(m *Model) []string {
	out := make([]string, 0, len(m.nodes))
	for _, r := range m.nodes {
		out = append(out, r.Row.DisplayName)
	}
	return out
}

// skin is a truecolor skin's theme, built the way the app builds it.
func skin(t testing.TB) shared.Theme {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	s, ok := shared.LookupSkin("nord")
	if !ok {
		t.Fatal("no nord skin")
	}
	return s.Theme(false)
}

// copyWorkflow copies wf with a node map of its own.
func copyWorkflow(wf core.Workflow) core.Workflow {
	out := wf
	out.Nodes = make(map[string]core.Node, len(wf.Nodes))
	for k, v := range wf.Nodes {
		out.Nodes[k] = v
	}
	return out
}

// golden compares got with testdata/<name>.golden; UPDATE_GOLDEN=1 rewrites it.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run UPDATE_GOLDEN=1 once and review)", path, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// treeFixture is a node map in the controller's shape; starts are minutes after the epoch, -1 for none.
type treeFixture struct {
	name  string
	nodes map[string]core.Node
}

func newTreeFixture(name string) *treeFixture {
	return &treeFixture{name: name, nodes: map[string]core.Node{}}
}

func (f *treeFixture) node(id, name, display, typ, phase, boundary string, startMin int, children ...string) *treeFixture {
	n := core.Node{
		ID: id, Name: name, DisplayName: display, Type: typ, Phase: phase,
		BoundaryID: boundary, Children: children,
	}
	if startMin >= 0 {
		at := testkit.FixtureEpoch.Add(time.Duration(startMin) * time.Minute)
		n.StartedAt = &at
	}
	f.nodes[id] = n
	return f
}

func (f *treeFixture) with(id string, edit func(*core.Node)) *treeFixture {
	n := f.nodes[id]
	edit(&n)
	f.nodes[id] = n
	return f
}

func (f *treeFixture) workflow() core.Workflow {
	return core.Workflow{
		Summary:        core.Summary{Ref: core.Ref{Namespace: "ns", Name: f.name, UID: "u-" + f.name}, Phase: "Running"},
		Nodes:          f.nodes,
		NodesAvailable: true,
	}
}
