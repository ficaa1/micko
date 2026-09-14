package actions

import (
	"fmt"
	"strings"
	"unicode"

	"argo-tui/internal/core"
	"argo-tui/internal/ui/shared"
	tea "charm.land/bubbletea/v2"
)

const maxTypedNameLength = 256

// State is the explicit UI lifecycle for a guarded action.
type State uint8

const (
	StateIdle State = iota
	StateUnavailable
	StateMenu
	StateConfirm
	StateTypedName
	StateSubmitting
	StateOutcome
)

// ActionIntentMsg is the only output of this package. The root application
// owns transport and must decide how to execute it.
type ActionIntentMsg struct{ Request core.ActionRequest }

// Model is a child Tea model for safe workflow actions. It has no API client
// and never starts a goroutine.
type Model struct {
	ref                          core.Ref
	allowActions, readOnly, demo bool
	state                        State
	action                       core.Action
	typedName                    string
	intent                       *core.ActionRequest
	intentCount                  int
	outcome                      *core.ActionResult
	reason                       string
	server, profile, phase       string
	width, height                int
}

var _ tea.Model = (*Model)(nil)

// New creates an action model. flags are allowActions, readOnly, demo;
// missing flags retain their safe false values.
func New(ref core.Ref, flags ...bool) *Model {
	m := &Model{ref: ref}
	if len(flags) > 0 {
		m.allowActions = flags[0]
	}
	if len(flags) > 1 {
		m.readOnly = flags[1]
	}
	if len(flags) > 2 {
		m.demo = flags[2]
	}
	return m
}

// NewModel is an explicit alias for callers that use the other UI package
// naming convention.
func NewModel(ref core.Ref, flags ...bool) *Model { return New(ref, flags...) }

func (m *Model) Init() tea.Cmd    { return nil }
func (m *Model) State() State     { return m.state }
func (m *Model) Ref() core.Ref    { return m.ref }
func (m *Model) IntentCount() int { return m.intentCount }
func (m *Model) LastIntent() core.ActionRequest {
	if m.intent == nil {
		return core.ActionRequest{}
	}
	return *m.intent
}

// Open begins the consequence-aware confirmation flow. Termination always
// requires the typed-name gate; all other actions require an explicit yes.
func (m *Model) Open(action core.Action) {
	m.action = action
	m.outcome = nil
	m.typedName = ""
	if !m.allowActions || m.readOnly || m.demo {
		m.state = StateUnavailable
		if !m.allowActions {
			m.reason = "actions disabled; launch with --allow-actions"
		}
		if m.readOnly {
			m.reason = "read-only mode: actions unavailable"
		}
		if m.demo {
			m.reason = "demo mode: actions unavailable"
		}
		return
	}
	if action == core.ActionTerminate {
		m.state = StateTypedName
	} else {
		m.state = StateConfirm
	}
}

// OpenUnavailable shows the action pane in its blocked state with an explicit
// reason. A key that silently does nothing reads as a broken key, and the
// operator needs to know the data is stale rather than the action unsupported.
func (m *Model) OpenUnavailable(reason string) {
	m.action = ""
	m.outcome = nil
	m.typedName = ""
	m.state = StateUnavailable
	m.reason = reason
}

// Close returns the pane to idle from any reporting state. The root calls it
// when it has taken the outcome over: a pane that only closes on a key press
// leaves the reader one press away from the workflow they just changed.
func (m *Model) Close() {
	m.state = StateIdle
	m.typedName = ""
}

// OutcomeLine is the one-line report of the finished action, for the footer
// of the route the root returns to. Empty when there is no outcome yet.
func (m *Model) OutcomeLine() string {
	if m.outcome == nil {
		return ""
	}
	verb := strings.ToLower(string(m.outcome.Action))
	switch m.outcome.Outcome {
	case core.ActionConfirmed:
		s := verb + " confirmed"
		if wf := m.outcome.Workflow; wf != nil && wf.Summary.Phase != "" {
			s += " — phase " + shared.Sanitize(wf.Summary.Phase)
		}
		if ref := m.outcome.Affected; ref != nil && ref.Name != "" && ref.Name != m.ref.Name {
			s += " — new workflow " + shared.Sanitize(ref.Name)
		}
		return s
	case core.ActionAccepted:
		s := verb + " accepted — the server applied it, it has not finished"
		if wf := m.outcome.Workflow; wf != nil && wf.Summary.Phase != "" {
			s += " (phase " + shared.Sanitize(wf.Summary.Phase) + ")"
		}
		return s
	default:
		return verb + " outcome unknown"
	}
}

// Outcome returns the recorded result, or nil before one arrives.
func (m *Model) Outcome() *core.ActionResult { return m.outcome }

func (m *Model) Cancel() {
	if m.state == StateMenu || m.state == StateConfirm || m.state == StateTypedName {
		m.state = StateIdle
		m.typedName = ""
	}
}

// OpenMenu presents the two milestone-supported workflow actions.
func (m *Model) OpenMenu() {
	if !m.allowActions || m.readOnly || m.demo {
		m.Open(core.ActionResume)
		return
	}
	m.state = StateMenu
	m.reason = ""
}

func (m *Model) Confirm() bool {
	if m.state != StateConfirm {
		return false
	}
	return m.submitConfirmed("")
}
func (m *Model) SetTypedName(name string) {
	if m.state == StateTypedName {
		m.typedName = appendInput("", name)
	}
}
func (m *Model) TypedName() string { return m.typedName }
func (m *Model) SetContext(server, profile, phase string) {
	m.server, m.profile, m.phase = server, profile, phase
}
func (m *Model) Submit() bool {
	if m.state != StateTypedName || m.typedName != m.ref.Name {
		return false
	}
	return m.submitConfirmed(m.typedName)
}

func (m *Model) submitConfirmed(typed string) bool {
	if m.state == StateSubmitting || m.intent != nil {
		return false
	}
	req := core.ActionRequest{Ref: m.ref, Action: m.action, Confirmation: core.Confirmation{Confirmed: true, TypedName: typed}}
	m.intent = &req
	m.intentCount++
	m.state = StateSubmitting
	return true
}

// SetOutcome is called by the root after it has performed read-back. Unknown
// is intentionally rendered as possibly applied; it is never success.
func (m *Model) SetOutcome(result core.ActionResult) {
	if m.state != StateSubmitting {
		return
	}
	m.outcome = &result
	m.state = StateOutcome
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.state == StateTypedName {
			m.updateTypedName(key)
		} else {
			m.updateNonInput(key)
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}
	if m.state == StateSubmitting && m.intent != nil {
		req := *m.intent
		m.intent = nil // one-shot command; state remains submitting to suppress duplicates
		return m, func() tea.Msg { return ActionIntentMsg{Request: req} }
	}
	return m, nil
}

func (m *Model) updateTypedName(key tea.KeyPressMsg) {
	switch key.Code {
	case tea.KeyEscape:
		m.Cancel()
	case tea.KeyBackspace:
		if m.typedName != "" {
			r := []rune(m.typedName)
			m.typedName = string(r[:len(r)-1])
		}
	case tea.KeyEnter:
		m.Submit()
	default:
		m.typedName = appendInput(m.typedName, key.Text)
	}
}

func (m *Model) updateNonInput(key tea.KeyPressMsg) {
	switch strings.ToLower(key.String()) {
	case "esc":
		if m.state == StateOutcome || m.state == StateUnavailable {
			m.state = StateIdle
		} else {
			m.Cancel()
		}
	case "enter":
		if m.state == StateOutcome || m.state == StateUnavailable || m.state == StateConfirm {
			m.state = StateIdle
		}
	case "y":
		if m.state == StateConfirm {
			m.Confirm()
		}
	case "u":
		if m.state == StateMenu {
			m.Open(core.ActionResume)
		}
	case "r":
		// retry restarts this workflow from its failed nodes. It needs its
		// own key: resume already has u, and sharing one would leave the
		// restart unreachable.
		if m.state == StateMenu {
			m.Open(core.ActionRetry)
		}
	case "b":
		if m.state == StateMenu {
			m.Open(core.ActionResubmit)
		}
	case "s":
		if m.state == StateMenu {
			m.Open(core.ActionStop)
		}

	}
}

func appendInput(current, input string) string {
	runes := []rune(current)
	for _, r := range input {
		if unicode.IsControl(r) || len(runes) >= maxTypedNameLength {
			continue
		}
		runes = append(runes, r)
	}
	return string(runes)
}

func (m *Model) View() tea.View {
	return tea.NewView(render(m))
}

func actionLabel(a core.Action) string { return strings.ToUpper(string(a)) }
func target(ref core.Ref) string {
	return fmt.Sprintf("%s/%s (UID: %s)", shared.Sanitize(ref.Namespace), shared.Sanitize(ref.Name), shared.Sanitize(ref.UID))
}
