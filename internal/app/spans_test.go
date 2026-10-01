package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/diagnostics"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
)

// Debug spans report waits from the initiating action to the accepted reply.
func TestDebugReportsTheWaitsTheReaderSees(t *testing.T) {
	const wait = 250 * time.Millisecond
	cases := []struct {
		name string
		prep func(m *Root, f *testkit.FakeReader)
		act  func(m *Root) tea.Cmd
		want []string
	}{
		{"first list", nil,
			func(m *Root) tea.Cmd { return m.Init() },
			[]string{"first_list ok 250"}},
		{"first list after a profile switch", loaded,
			func(m *Root) tea.Cmd {
				next := *m.conn
				next.Profile = "other"
				_, cmd := m.Update(profileConnectedMsg{Conn: m.connGen, Profile: "other", Connection: &next})
				return cmd
			},
			[]string{"first_list ok 250"}},
		{"all namespaces", loaded,
			func(m *Root) tea.Cmd { return keys(m, "0") },
			[]string{"allns ok 250"}},
		{"all namespaces left before the answer", loaded,
			func(m *Root) tea.Cmd { return keys(m, "0", "0") },
			nil},
		{"detail", loaded,
			func(m *Root) tea.Cmd {
				_, cmd := m.Update(OpenWorkflowMsg{Ref: m.listView.SelectedRef()})
				return cmd
			},
			[]string{"detail_open ok 250"}},
		{"failed detail", func(m *Root, f *testkit.FakeReader) {
			loaded(m, f)
			f.GetErr = errors.New("unavailable")
		},
			func(m *Root) tea.Cmd {
				_, cmd := m.Update(OpenWorkflowMsg{Ref: m.listView.SelectedRef()})
				return cmd
			},
			[]string{"detail_open failed 250"}},
		{"logs", loaded,
			func(m *Root) tea.Cmd {
				_, cmd := m.Update(OpenLogsMsg{Ref: m.listView.SelectedRef(), Container: "main"})
				return cmd
			},
			[]string{"first_log ok 250"}},
		{"empty kind", loaded,
			func(m *Root) tea.Cmd { return paletteEnter(m, "cron") },
			[]string{"cron_open ok 250"}},
		{"kind with rows", func(m *Root, f *testkit.FakeReader) {
			loaded(m, f)
			runLine(m, "cron")
			runLine(m, "wf")
		},
			func(m *Root) tea.Cmd { return paletteEnter(m, "cron") },
			nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			clock := testkit.NewFakeClock(testkit.FixtureEpoch)
			f := testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch))
			m := NewRoot(nil, clock, "", time.Millisecond, actions.Options{ReadOnly: true})
			m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			m.Adopt(&Connection{Reader: f, Namespace: "demo", Diagnostics: diagnostics.New(&out)})
			if c.prep != nil {
				c.prep(m, f)
			}
			out.Reset()
			cmd := c.act(m)
			clock.Advance(wait)
			deliver(m, cmd)
			if got := spans(t, &out); !slices.Equal(got, c.want) {
				t.Fatalf("spans = %q, want %q", got, c.want)
			}
		})
	}
}

func loaded(m *Root, _ *testkit.FakeReader) { deliver(m, m.Init()) }

// paletteEnter returns the commands started by a palette line.
func paletteEnter(m *Root, line string) tea.Cmd {
	deliver(m, typeKeys(m, ":"))
	typeKeys(m, line)
	var cmds []tea.Cmd
	for _, msg := range runCmd(pressKey(m, tea.KeyEnter)) {
		_, cmd := m.Update(msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// spans reads the span lines of a diagnostics stream as "name state ms".
func spans(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()
	var got []string
	dec := json.NewDecoder(out)
	for dec.More() {
		var e diagnostics.Event
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if e.Stage == diagnostics.StageSpan {
			got = append(got, fmt.Sprintf("%s %s %g", e.Span, e.State, e.TotalMS))
		}
	}
	return got
}
