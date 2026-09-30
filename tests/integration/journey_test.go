//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ficaa1/micko/internal/app"
	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/ui/actions"
)

const testNS = "synthetic-ns"

// newSeededServer starts a fixture server with three synthetic workflows.
func newSeededServer(t *testing.T) *FixtureServer {
	t.Helper()
	fs := NewFixtureServer(t)
	for _, wf := range []WireWorkflow{
		FixtureWorkflow(testNS, "wf-a", "Running", "uid-a"),
		FixtureWorkflow(testNS, "wf-b", "Failed", "uid-b"),
		FixtureWorkflow(testNS, "wf-c", "Succeeded", "uid-c"),
	} {
		fs.PutWorkflow(wf)
	}
	return fs
}

// harness drives the real root model over the production Argo client
// without a terminal. Like tea.Program it runs every command in its own
// goroutine and feeds each message back through Update, one at a time.
type harness struct {
	t    *testing.T
	root *app.Root
	msgs chan tea.Msg
}

// newHarness is a read-only root on ns of fs, sized as a 140x40 terminal
// and settled on its first list.
func newHarness(t *testing.T, fs *FixtureServer, ns string) *harness {
	t.Helper()
	client, err := argo.NewClient(argo.Options{Server: fs.URL(), TokenFn: func() (string, error) { return "synthetic-token", nil }})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, root: app.NewRoot(client, fixedClock{}, ns, time.Hour, actions.Options{ReadOnly: true}), msgs: make(chan tea.Msg, 64)}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.run(h.root.Init())
	h.settle()
	return h
}

// fixedClock is the fixture epoch, so every age on screen is stable.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return FixtureEpoch }

// run starts cmd the way the program does, each batched command on its own.
func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				h.run(c)
			}
		default:
			h.msgs <- msg
		}
	}()
}

// send delivers one message to the model and starts the command it returns.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	next, cmd := h.root.Update(msg)
	h.root = next.(*app.Root)
	h.run(cmd)
}

// settle delivers messages until none arrives for a moment. A model that
// keeps producing them without an input is a request loop and fails.
func (h *harness) settle() {
	h.t.Helper()
	for n := 0; ; n++ {
		if n > 1000 {
			h.t.Fatal("the model never settled")
		}
		select {
		case msg := <-h.msgs:
			h.send(msg)
		case <-time.After(300 * time.Millisecond):
			return
		}
	}
}

// press sends each key as the terminal would, then settles.
func (h *harness) press(keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			h.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			h.send(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			h.send(tea.KeyPressMsg{Code: []rune(k)[0], Text: k})
		}
	}
	h.settle()
}

// screen is the rendered frame without styling.
func (h *harness) screen() string { return ansi.Strip(h.root.View().Content) }

// A read-only session over real HTTP lists the namespace, opens a workflow
// and its logs, walks back with esc, and sends nothing but GETs.
func TestReadOnlyJourney(t *testing.T) {
	fs := newSeededServer(t)
	fs.SetLogs(FixtureLogEntries(4))
	h := newHarness(t, fs, testNS)
	steps := []struct {
		keys []string
		want []string
	}{
		{nil, []string{"list: 3 workflows", "wf-a", "wf-b", "wf-c"}},
		{[]string{"enter"}, []string{"workflow wf-b phase=Failed"}},
		{[]string{"esc"}, []string{"list: 3 workflows"}},
		{[]string{"l"}, []string{"stream ended (4 records)"}},
		{[]string{"esc"}, []string{"list: 3 workflows"}},
	}
	for i, s := range steps {
		h.press(s.keys...)
		v := h.screen()
		for _, want := range s.want {
			if !strings.Contains(v, want) {
				t.Fatalf("step %d %v: screen lacks %q:\n%s", i, s.keys, want, v)
			}
		}
	}
	for method, n := range fs.RequestsByMethod() {
		if method != "GET" {
			t.Errorf("the journey sent %s x%d", method, n)
		}
	}
}

// A refusal after a good list keeps the rows and says they are stale; an
// empty namespace says so rather than looking like a load or a failure.
func TestListStates(t *testing.T) {
	cases := []struct {
		name  string
		ns    string
		fault bool
		want  []string
		not   []string
	}{
		{"refused after a good list", testNS, true,
			[]string{"list: STALE (last good 3 workflows): synthetic rbac denial", "wf-b"}, nil},
		{"empty namespace", "empty-ns", false, []string{"list: empty"}, []string{"loading", "error"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newSeededServer(t)
			h := newHarness(t, fs, c.ns)
			if c.fault {
				fs.SetFault("list", Fault{Status: 403, Code: grpcPermDenied, Message: "synthetic rbac denial"})
				h.press("r")
			}
			v := h.screen()
			for _, want := range c.want {
				if !strings.Contains(v, want) {
					t.Errorf("screen lacks %q:\n%s", want, v)
				}
			}
			for _, bad := range c.not {
				if strings.Contains(v, bad) {
					t.Errorf("screen shows %q:\n%s", bad, v)
				}
			}
		})
	}
}

// A workflow gone since the list and one the token may not read are two
// different screens, and neither shows a workflow.
func TestDetailStates(t *testing.T) {
	cases := []struct {
		name  string
		fault string
		want  string
		not   string
	}{
		{"gone", "get-notfound", "workflow no longer available: wf-b", "phase=Failed"},
		{"forbidden", "get-forbidden", "detail error: synthetic detail denial", "no longer available"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newSeededServer(t)
			h := newHarness(t, fs, testNS)
			switch c.fault {
			case "get-notfound":
				fs.SetFault("get:"+testNS+"/wf-b", Fault{Status: 404, Code: grpcNotFound, Message: "workflows.argoproj.io \"wf-b\" not found"})
			case "get-forbidden":
				fs.SetFault("get:"+testNS+"/wf-b", Fault{Status: 403, Code: grpcPermDenied, Message: "synthetic detail denial"})
			}
			h.press("enter")
			v := h.screen()
			if !strings.Contains(v, c.want) || strings.Contains(v, c.not) {
				t.Errorf("screen, want %q and not %q:\n%s", c.want, c.not, v)
			}
		})
	}
}

// A log stream shows every record before it ends, and an error in the
// stream reaches the screen.
func TestLogStates(t *testing.T) {
	cases := []struct {
		name  string
		fault *Fault
		want  []string
	}{
		{"ended", nil, []string{"stream ended (10 records)", "synthetic log line 10"}},
		{"in-band error", &Fault{Code: grpcInternal, Message: "synthetic in-band boom"},
			[]string{"stream error: ", "synthetic in-band boom", "synthetic log line 10"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newSeededServer(t)
			fs.SetLogs(FixtureLogEntries(10))
			if c.fault != nil {
				fs.SetLogError(*c.fault)
			}
			h := newHarness(t, fs, testNS)
			h.press("l")
			v := h.screen()
			for _, want := range c.want {
				if !strings.Contains(v, want) {
					t.Errorf("screen lacks %q:\n%s", want, v)
				}
			}
		})
	}
}
