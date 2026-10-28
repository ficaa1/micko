package profiles

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeSetup answers the wizard from fixed data and records what it was asked.
type fakeSetup struct {
	servers []Server
	// checks answers each probe in turn; the last one repeats.
	checks []Outcome
	probed []Draft
	saved  []string
}

func (f *fakeSetup) KubeContexts(context.Context) ([]string, string, error) {
	return []string{"kind-dev", "arn:aws:eks:eu-west-1:1:cluster/prod"}, "kind-dev", nil
}

func (f *fakeSetup) ArgoServers(context.Context, string) ([]Server, error) { return f.servers, nil }

func (f *fakeSetup) Namespaces(context.Context, string) ([]string, error) {
	return nil, errors.New(`namespaces is forbidden`)
}

func (f *fakeSetup) Probe(_ context.Context, d Draft) Check {
	f.probed = append(f.probed, d)
	out := f.checks[min(len(f.probed), len(f.checks))-1]
	d.ServerAuth = out == Reachable && d.TokenEnv == ""
	return Check{Outcome: out, Draft: d}
}

func (f *fakeSetup) Save(name string, d Draft) ([]Item, error) {
	f.saved = append(f.saved, name)
	return []Item{{Name: name}}, nil
}

// drive sends keys like keys does, but runs each wizard request to its reply
// at once, and returns only what leaves the dialog.
func drive(m *Model, cmd tea.Cmd, seq string) []tea.Msg {
	var out []tea.Msg
	run := func(cmd tea.Cmd) {
		for cmd != nil {
			msg := cmd()
			if r, ok := msg.(ReplyMsg); ok {
				cmd = m.Update(r)
				continue
			}
			out = append(out, msg)
			cmd = nil
		}
	}
	run(cmd)
	for _, k := range strings.Fields(seq) {
		key, ok := namedKeys[k]
		if !ok {
			key = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		run(m.Update(key))
	}
	return out
}

var argoServer = Server{Namespace: "argo", Name: "argo-server", Port: 2746}

// The wizard walks context, server and namespace, then goes where the probe
// sends it: straight to a name, to a token, or to trusting the certificate.
// A context with no Argo Server says what was looked for.
// It saves under the name and connects to it; esc steps back one screen.
func TestSetupWizard(t *testing.T) {
	cases := []struct {
		name     string
		items    []Item
		checks   []Outcome
		keys     string
		want     []string
		wantSave string
		// wantDraft is the last draft probed.
		wantDraft Draft
		inSetup   bool
	}{
		{"no profiles: server auth", nil, []Outcome{Reachable}, "enter enter enter enter",
			[]string{"new profile"}, "kind-dev",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo"}, false},
		{"from the list, an EKS context", []Item{{Name: "dev"}}, []Outcome{Reachable}, "down enter down enter enter enter enter",
			nil, "prod",
			Draft{Context: "arn:aws:eks:eu-west-1:1:cluster/prod", Server: argoServer, Namespace: "argo"}, false},
		{"typed namespace", nil, []Outcome{Reachable}, "enter enter w f enter",
			[]string{"connected, no token needed", "profile name: kind-dev_"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "wf"}, true},
		{"token", nil, []Outcome{NeedsToken, Reachable}, "enter enter enter $ T O K enter",
			[]string{"connected with the token from $TOK"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo", TokenEnv: "TOK"}, true},
		{"token from a file", nil, []Outcome{NeedsToken, Reachable}, "enter enter enter / t enter enter",
			nil, "kind-dev",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo", TokenFile: "/t"}, false},
		{"not a token source", nil, []Outcome{NeedsToken}, "enter enter enter - enter",
			[]string{"the server wants a token", "give an environment variable name or an absolute file path"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo"}, true},
		{"trust the certificate", nil, []Outcome{Untrusted, Reachable}, "enter enter enter t",
			[]string{"connected, no token needed"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo", Insecure: true}, true},
		{"a CA file", nil, []Outcome{Untrusted, Reachable}, "enter enter enter c / c a enter",
			[]string{"connected"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo", CAFile: "/ca"}, true},
		{"failed check, back to the namespace", nil, []Outcome{Failed}, "enter enter enter esc",
			[]string{"workflow namespace", "filter: _"}, "",
			Draft{Context: "kind-dev", Server: argoServer, Namespace: "argo"}, true},
		{"no Argo Server in the context", nil, nil, "enter",
			[]string{"no Argo Server service found in kind-dev", "labels and names the Argo manifests"}, "", Draft{}, true},
		{"esc from the server list", nil, []Outcome{Reachable}, "enter esc",
			[]string{"kube context", "kind-dev  (current)"}, "", Draft{}, true},
		{"esc from the first step", []Item{{Name: "dev"}}, []Outcome{Reachable}, "down enter esc",
			[]string{"choose a profile", "  dev", "+ new profile"}, "", Draft{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setup := &fakeSetup{checks: c.checks}
			if c.checks != nil {
				setup.servers = []Server{argoServer}
			}
			m := testModel(c.items...)
			m.SetSetup(setup)
			out := drive(m, m.Open("", ""), c.keys)
			got := body(m)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("dialog lacks %q:\n%s", w, got)
				}
			}
			var wantOut []tea.Msg
			if c.wantSave != "" {
				wantOut = []tea.Msg{SwitchMsg{Profile: c.wantSave}}
				if !reflect.DeepEqual(setup.saved, []string{c.wantSave}) {
					t.Errorf("saved %q, want %q", setup.saved, c.wantSave)
				}
			}
			if !reflect.DeepEqual(out, wantOut) {
				t.Errorf("emitted %#v, want %#v", out, wantOut)
			}
			if c.wantDraft != (Draft{}) {
				if len(setup.probed) == 0 {
					t.Fatal("nothing was probed")
				}
				if last := setup.probed[len(setup.probed)-1]; last != c.wantDraft {
					t.Errorf("probed %+v, want %+v", last, c.wantDraft)
				}
			}
			if m.InSetup() != c.inSetup {
				t.Errorf("in setup = %v, want %v", m.InSetup(), c.inSetup)
			}
		})
	}
}

// A reply to a request the reader stepped away from changes nothing.
func TestSetupDropsAbandonedReplies(t *testing.T) {
	setup := &fakeSetup{servers: []Server{argoServer}, checks: []Outcome{Reachable}}
	m := testModel()
	m.SetSetup(setup)
	drive(m, m.Open("", ""), "enter enter")
	probe := m.Update(namedKeys["enter"])
	m.Update(namedKeys["esc"])
	m.Update(probe())
	if got := body(m); !strings.Contains(got, "workflow namespace") || strings.Contains(got, "profile name") {
		t.Errorf("an abandoned probe moved the wizard on:\n%s", got)
	}
}
