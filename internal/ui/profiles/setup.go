package profiles

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/ui/shared"
)

// Setup is what the new-profile wizard asks of the kubeconfig, the cluster
// and the config file. Every method blocks and runs off the update loop.
type Setup interface {
	KubeContexts(ctx context.Context) (names []string, current string, err error)
	ArgoServers(ctx context.Context, kubeContext string) ([]Server, error)
	Namespaces(ctx context.Context, kubeContext string) ([]string, error)
	// Probe forwards to the draft's server, makes one request and says what
	// the profile still needs.
	Probe(ctx context.Context, d Draft) Check
	// Save appends the profile to the config file and returns the new list.
	Save(name string, d Draft) ([]Item, error)
}

// Server is an Argo Server service found in a kube context.
type Server struct {
	Namespace, Name string
	Port            int
}

func (s Server) String() string { return fmt.Sprintf("%s/%s :%d", s.Namespace, s.Name, s.Port) }

// Draft is the profile the wizard is building.
type Draft struct {
	Context   string
	Server    Server
	Namespace string
	// HTTP is set when the server answers plain HTTP behind the forward.
	HTTP bool
	// ServerAuth is set when the server answered without a token.
	ServerAuth bool
	TokenEnv   string
	TokenFile  string
	CAFile     string
	Insecure   bool
}

// Outcome is what a probe found.
type Outcome int

const (
	Reachable Outcome = iota
	NeedsToken
	Untrusted
	Failed
)

// Check is a probe's result: the outcome, the draft as the probe left it,
// and a sanitized line saying why when it did not connect.
type Check struct {
	Outcome Outcome
	Draft   Draft
	Detail  string
}

// ReplyMsg carries a wizard request's result back to the picker.
type ReplyMsg struct {
	seq   int
	apply func(*Model) tea.Cmd
}

type step int

const (
	stepOff step = iota
	stepContext
	stepServer
	stepNamespace
	stepCheck
	stepToken
	stepTrust
	stepCA
	stepFailed
	stepName
)

// requestTimeout bounds one kubectl call or one probe, port-forward included.
const requestTimeout = time.Minute

var (
	envName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	nameJunk = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
)

// wizard is the new-profile flow: context, server, namespace, a probe that
// finds out what auth and TLS the server needs, then a name.
type wizard struct {
	step step
	// busy is a request in flight; seq drops the replies of abandoned ones.
	busy   bool
	seq    int
	cancel context.CancelFunc

	draft      Draft
	contexts   []string
	current    string
	servers    []Server
	namespaces []string

	// options are the current list step's rows, narrowed by input.
	options []string
	cursor  int
	// input is the list filter or the text being typed.
	input string
	// detail is the last failure, shown on the step it sent the reader back to.
	detail string
	// connected says how the probe got in, shown above the name prompt.
	connected string
}

// SetSetup enables the wizard. Without it the picker only lists profiles.
func (m *Model) SetSetup(s Setup) { m.setup = s }

// InSetup reports whether the wizard owns the dialog, so esc steps back
// instead of quitting.
func (m *Model) InSetup() bool { return m.open && m.w.step != stepOff }

func (m *Model) startSetup() tea.Cmd {
	m.w = wizard{step: stepContext}
	return m.request(func(ctx context.Context, s Setup) func(*Model) tea.Cmd {
		names, current, err := s.KubeContexts(ctx)
		return func(m *Model) tea.Cmd {
			m.w.contexts, m.w.current = names, current
			m.w.detail = errText(err)
			if err == nil && len(names) == 0 {
				m.w.detail = "the kubeconfig has no contexts"
			}
			m.showList(m.w.contexts, current)
			return nil
		}
	})
}

// request runs one Setup call off the update loop. Starting another, or
// leaving the step, abandons it.
func (m *Model) request(run func(context.Context, Setup) func(*Model) tea.Cmd) tea.Cmd {
	m.stopRequest()
	m.w.busy = true
	seq := m.w.seq
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	m.w.cancel = cancel
	s := m.setup
	return func() tea.Msg {
		defer cancel()
		return ReplyMsg{seq: seq, apply: run(ctx, s)}
	}
}

func (m *Model) stopRequest() {
	if m.w.cancel != nil {
		m.w.cancel()
		m.w.cancel = nil
	}
	m.w.seq++
	m.w.busy = false
}

func (m *Model) reply(msg ReplyMsg) tea.Cmd {
	if !m.InSetup() || msg.seq != m.w.seq {
		return nil
	}
	m.w.busy, m.w.cancel = false, nil
	return msg.apply(m)
}

// showList fills a list step and puts the cursor on want.
func (m *Model) showList(options []string, want string) {
	m.w.input = ""
	m.w.options = options
	m.w.cursor = max(0, slices.Index(options, want))
}

func (m *Model) filtered() []string {
	q := strings.ToLower(strings.TrimSpace(m.w.input))
	var rows []string
	for _, o := range m.w.options {
		if q == "" || strings.Contains(strings.ToLower(o), q) {
			rows = append(rows, o)
		}
	}
	return rows
}

func (m *Model) updateSetup(msg tea.Msg) tea.Cmd {
	if paste, ok := msg.(tea.PasteMsg); ok {
		if !m.w.busy {
			m.w.input += strings.TrimSpace(paste.Content)
			m.w.cursor = 0
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	s := key.String()
	if s == "esc" {
		return m.back()
	}
	if m.w.busy {
		return nil
	}
	switch m.w.step {
	case stepTrust:
		switch s {
		case "t":
			m.w.draft.Insecure = true
			return m.probe()
		case "c":
			m.w.step, m.w.input = stepCA, m.w.draft.CAFile
		}
		return nil
	case stepFailed:
		if s == "enter" {
			return m.probe()
		}
		return nil
	}
	list := m.w.step == stepContext || m.w.step == stepServer || m.w.step == stepNamespace
	switch s {
	case "enter":
		return m.submit()
	case "up", "ctrl+p", "down", "ctrl+n":
		if n := len(m.filtered()); list && n > 0 {
			delta := 1
			if s == "up" || s == "ctrl+p" {
				delta = n - 1
			}
			m.w.cursor = (m.w.cursor + delta) % n
		}
	case "backspace":
		if r := []rune(m.w.input); len(r) > 0 {
			m.w.input = string(r[:len(r)-1])
			m.w.cursor = 0
		}
	default:
		if key.Text != "" {
			m.w.input += key.Text
			m.w.cursor = 0
		}
	}
	return nil
}

// back leaves the current step for the one before it, abandoning any
// request in flight. From the first step it returns to the profile list.
func (m *Model) back() tea.Cmd {
	m.stopRequest()
	m.w.detail = ""
	switch m.w.step {
	case stepContext:
		m.w = wizard{seq: m.w.seq}
		m.applyFilter()
	case stepServer:
		m.w.step = stepContext
		m.showList(m.w.contexts, m.w.draft.Context)
	case stepCA:
		m.w.step, m.w.input = stepTrust, ""
	default:
		m.w.step = stepNamespace
		m.showList(m.w.namespaces, m.w.draft.Namespace)
		if m.w.draft.Namespace != "" && !slices.Contains(m.w.namespaces, m.w.draft.Namespace) {
			m.w.input = m.w.draft.Namespace
		}
	}
	return nil
}

func (m *Model) submit() tea.Cmd {
	rows := m.filtered()
	chosen := ""
	if m.w.cursor < len(rows) {
		chosen = rows[m.w.cursor]
	}
	text := strings.TrimSpace(m.w.input)
	m.w.detail = ""
	switch m.w.step {
	case stepContext:
		if chosen == "" {
			return nil
		}
		m.w.draft = Draft{Context: chosen}
		m.w.step = stepServer
		m.w.options = nil
		return m.request(func(ctx context.Context, s Setup) func(*Model) tea.Cmd {
			servers, err := s.ArgoServers(ctx, chosen)
			return func(m *Model) tea.Cmd {
				m.w.servers = servers
				m.w.detail = errText(err)
				labels := make([]string, len(servers))
				for i, sv := range servers {
					labels[i] = sv.String()
				}
				m.showList(labels, "")
				return nil
			}
		})
	case stepServer:
		i := slices.IndexFunc(m.w.servers, func(s Server) bool { return s.String() == chosen })
		if i < 0 {
			return nil
		}
		m.w.draft.Server = m.w.servers[i]
		m.w.step = stepNamespace
		m.w.options = nil
		kubeContext, serverNS := m.w.draft.Context, m.w.draft.Server.Namespace
		return m.request(func(ctx context.Context, s Setup) func(*Model) tea.Cmd {
			names, err := s.Namespaces(ctx, kubeContext)
			return func(m *Model) tea.Cmd {
				// Reading namespaces may be forbidden; the server's own is
				// still a choice, and any typed name is accepted.
				if err != nil || !slices.Contains(names, serverNS) {
					names = append(names, serverNS)
					slices.Sort(names)
				}
				m.w.namespaces = names
				m.showList(names, serverNS)
				return nil
			}
		})
	case stepNamespace:
		ns := chosen
		if ns == "" {
			ns = text
		}
		if ns == "" {
			return nil
		}
		m.w.draft.Namespace = ns
		m.w.draft.ServerAuth, m.w.draft.TokenEnv, m.w.draft.TokenFile = false, "", ""
		m.w.draft.HTTP, m.w.draft.Insecure, m.w.draft.CAFile = false, false, ""
		return m.probe()
	case stepToken:
		switch {
		case strings.HasPrefix(text, "/"):
			m.w.draft.TokenEnv, m.w.draft.TokenFile = "", text
		case envName.MatchString(strings.TrimPrefix(text, "$")):
			m.w.draft.TokenEnv, m.w.draft.TokenFile = strings.TrimPrefix(text, "$"), ""
		default:
			m.w.detail = "give an environment variable name or an absolute file path"
			return nil
		}
		return m.probe()
	case stepCA:
		if !strings.HasPrefix(text, "/") {
			m.w.detail = "give an absolute path to a PEM CA file"
			return nil
		}
		m.w.draft.CAFile, m.w.draft.Insecure = text, false
		return m.probe()
	case stepName:
		if text == "" {
			return nil
		}
		d := m.w.draft
		return m.request(func(_ context.Context, s Setup) func(*Model) tea.Cmd {
			items, err := s.Save(text, d)
			return func(m *Model) tea.Cmd {
				if err != nil {
					m.w.detail = errText(err)
					return nil
				}
				m.w = wizard{seq: m.w.seq}
				m.filter = ""
				m.SetItems(items, m.configPath)
				return func() tea.Msg { return SwitchMsg{Profile: text} }
			}
		})
	}
	return nil
}

// probe checks the draft against the server and moves to the step its
// outcome calls for.
func (m *Model) probe() tea.Cmd {
	m.w.step = stepCheck
	d := m.w.draft
	return m.request(func(ctx context.Context, s Setup) func(*Model) tea.Cmd {
		c := s.Probe(ctx, d)
		return func(m *Model) tea.Cmd {
			m.w.draft = c.Draft
			m.w.detail = shared.Sanitize(c.Detail)
			m.w.input = ""
			switch c.Outcome {
			case Reachable:
				m.w.step = stepName
				m.w.input = m.suggestName(c.Draft.Context)
				m.w.connected = "connected, no token needed"
				if !c.Draft.ServerAuth {
					m.w.connected = "connected with the token from " + tokenSource(c.Draft)
				}
			case NeedsToken:
				m.w.step = stepToken
				m.w.input = c.Draft.TokenEnv + c.Draft.TokenFile
			case Untrusted:
				m.w.step = stepTrust
			default:
				m.w.step = stepFailed
			}
			return nil
		}
	})
}

// suggestName turns a kube context into a free profile name: EKS ARNs and
// other punctuation become hyphens, and a taken name gets a number.
func (m *Model) suggestName(kubeContext string) string {
	if i := strings.LastIndex(kubeContext, "/"); i >= 0 {
		kubeContext = kubeContext[i+1:]
	}
	name := strings.Trim(nameJunk.ReplaceAllString(kubeContext, "-"), "-._")
	if name == "" {
		name = "argo"
	}
	taken := func(n string) bool {
		return slices.ContainsFunc(m.items, func(it Item) bool { return it.Name == n })
	}
	for i, base := 2, name; taken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func tokenSource(d Draft) string {
	if d.TokenFile != "" {
		return d.TokenFile
	}
	return "$" + d.TokenEnv
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return shared.Sanitize(err.Error())
}

func (m *Model) setupHints() string {
	if m.w.busy {
		return "esc back"
	}
	switch m.w.step {
	case stepContext, stepServer:
		return "type to filter  ↑↓ move  enter choose  esc back"
	case stepNamespace:
		return "type to filter or name one  ↑↓ move  enter choose  esc back"
	case stepTrust:
		return "t trust it for this profile  c give a CA file  esc back"
	case stepFailed:
		return "enter retry  esc back"
	case stepName:
		return "enter save and connect  esc back"
	}
	return "enter check  esc back"
}

func (m *Model) setupLines() []string {
	th := m.theme
	d := m.w.draft
	lines := []string{th.Title.Render("new profile")}
	field := func(label, value string) {
		if value != "" {
			lines = append(lines, th.Muted.Render(fmt.Sprintf("%-10s ", label))+shared.Sanitize(value))
		}
	}
	field("context", d.Context)
	if m.w.step > stepServer {
		field("server", d.Server.String())
	}
	if m.w.step > stepNamespace {
		field("namespace", d.Namespace)
	}
	lines = append(lines, "")
	warn := func() {
		if m.w.detail != "" {
			lines = append(lines, th.Warning.Render(m.w.detail))
		}
	}
	input := func(label string) {
		lines = append(lines, label+": "+shared.Sanitize(m.w.input)+"_")
	}

	switch m.w.step {
	case stepContext, stepServer, stepNamespace:
		prompt := map[step]string{
			stepContext:   "kube context",
			stepServer:    "Argo Server in " + d.Context,
			stepNamespace: "workflow namespace",
		}[m.w.step]
		lines = append(lines, prompt)
		if m.w.busy {
			wait := map[step]string{
				stepContext:   "reading the kubeconfig…",
				stepServer:    "looking for Argo Server services…",
				stepNamespace: "listing namespaces…",
			}[m.w.step]
			return append(lines, th.Dim.Render(wait))
		}
		warn()
		if m.w.step == stepServer && m.w.detail == "" && len(m.w.options) == 0 {
			return append(lines, "no Argo Server service found in "+shared.Sanitize(d.Context),
				th.Dim.Render("micko looks for the labels and names the Argo manifests and Helm chart use"))
		}
		input("filter")
		rows := m.filtered()
		for i, r := range rows {
			row := "  " + shared.Sanitize(r)
			if m.w.step == stepContext && r == m.w.current {
				row += th.Muted.Render("  (current)")
			}
			if i == m.w.cursor {
				row = th.SelectRow(row, m.width)
			}
			lines = append(lines, row)
		}
		if typed := strings.TrimSpace(m.w.input); len(rows) == 0 && typed != "" && m.w.step == stepNamespace {
			lines = append(lines, th.Dim.Render("enter uses "+shared.Sanitize(typed)))
		}
	case stepCheck:
		lines = append(lines, "forwarding to "+shared.Sanitize(d.Server.String())+" and checking the server…",
			th.Dim.Render("a port-forward can take a few seconds"))
	case stepToken:
		lines = append(lines, "the server wants a token")
		warn()
		lines = append(lines, th.Dim.Render("name the environment variable that holds it, or an absolute path to a file"))
		input("token from")
	case stepTrust:
		lines = append(lines, "the server's certificate is not trusted")
		warn()
		lines = append(lines, th.Dim.Render("trusting it is safe here: the traffic runs through the kubectl tunnel to loopback"))
	case stepCA:
		lines = append(lines, "CA file that signed the server's certificate")
		warn()
		input("CA file")
	case stepFailed:
		lines = append(lines, "the check failed")
		warn()
	case stepName:
		lines = append(lines, th.Accent.Render(m.w.connected))
		warn()
		input("profile name")
		if m.configPath != "" {
			lines = append(lines, "", th.Dim.Render("saved to "+shared.Sanitize(m.configPath)))
		}
	}
	return lines
}
