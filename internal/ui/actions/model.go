package actions

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
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
	// StateFinal is delete's second confirmation. It follows StateConfirm
	// and only a capital D leaves it towards a request; every other key
	// cancels.
	StateFinal
)

// finalDeleteKey is the one key that confirms a delete on its final screen.
// It is a capital letter so that neither the key that opened the delete
// (d) nor the key that passed the first confirmation (y), pressed once more
// by a heavy or repeating finger, can finish it.
const finalDeleteKey = "D"

// ActionIntentMsg is the output of a single-workflow action. The root
// application owns transport and must decide how to execute it.
type ActionIntentMsg struct{ Request core.ActionRequest }

// BulkIntentMsg is the output of a confirmed bulk action: one request per
// target the action applies to, in the order they are to be sent. The root
// sends them one at a time and never resends one.
type BulkIntentMsg struct{ Requests []core.ActionRequest }

// BulkItem is one target's result in a bulk action. Reason says why a
// refused or unknown target ended where it did; it is rendered sanitized.
type BulkItem struct {
	Result core.ActionResult
	Reason string
}

// Model is a child Tea model for safe workflow actions. It has no API client
// and never starts a goroutine.
//
// It acts on one workflow, or, after SetTargets with more than one, on a
// set of marked workflows. Every rule of the single path holds per target
// in the bulk path: explicit confirmation, one request per workflow, and
// an outcome that says what is known and nothing more.
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
	// theme styles the pane. The zero theme draws plain text; every state
	// is carried by its words, so the styling only repeats them.
	theme shared.Theme

	// targets is what the action reaches: the marked workflows, or the one
	// workflow a single action was opened on. Empty means the caller gave
	// only a Ref, and nothing is known about its phase.
	targets []core.Summary
	// bulk is set when more than one workflow is targeted.
	bulk bool
	// applies is the subset of targets the chosen action applies to, in
	// target order. It is what the confirmation lists and what is sent.
	applies []core.Summary
	// bulkIntent is the one-shot bulk request list, emitted by Update.
	bulkIntent []core.ActionRequest
	// sent counts the bulk requests the root has finished.
	sent int
	// stopRequested asks the root to send nothing after the request in
	// flight. It never cancels that request: it is already on the wire.
	stopRequested bool
	// items is the bulk outcome, one per applicable target.
	items []BulkItem
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

// SetTargets records the workflows the action reaches, with the state the
// menu's availability is judged from. One target is a single action on that
// workflow; more than one is a bulk action.
func (m *Model) SetTargets(targets []core.Summary) {
	m.targets = append([]core.Summary(nil), targets...)
	m.bulk = len(m.targets) > 1
	if len(m.targets) == 1 {
		m.ref = m.targets[0].Ref
	}
}

// Bulk reports whether the action reaches more than one workflow.
func (m *Model) Bulk() bool { return m.bulk }

// Targets returns the workflows the action reaches.
func (m *Model) Targets() []core.Summary { return append([]core.Summary(nil), m.targets...) }

// Action is the chosen action, empty before one is chosen.
func (m *Model) Action() core.Action { return m.action }

// Availability is one menu entry: an action and the number of targets it
// applies to.
type Availability struct {
	Action  core.Action
	Applies int
}

// Available lists the actions that apply to at least one target, in menu
// order. With no known target state every action is listed: the phase is
// unknown, and hiding a verb on a guess would make it unreachable.
func (m *Model) Available() []Availability {
	var out []Availability
	for _, a := range core.Actions {
		if n := len(m.applicable(a)); n > 0 {
			out = append(out, Availability{Action: a, Applies: n})
		}
	}
	return out
}

// Unavailable lists the actions that apply to no target, in menu order.
func (m *Model) Unavailable() []core.Action {
	var out []core.Action
	for _, a := range core.Actions {
		if len(m.applicable(a)) == 0 {
			out = append(out, a)
		}
	}
	return out
}

// applicable returns the targets action applies to.
func (m *Model) applicable(action core.Action) []core.Summary {
	if len(m.targets) == 0 {
		return []core.Summary{{Ref: m.ref, Phase: m.phase}}
	}
	var out []core.Summary
	for _, t := range m.targets {
		if action.AppliesTo(t) {
			out = append(out, t)
		}
	}
	return out
}

// Open begins the consequence-aware confirmation flow. A single terminate
// requires the typed-name gate and a bulk terminate the typed-count gate;
// delete requires an explicit yes and then its final screen; every other
// action requires an explicit yes.
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
	if m.bulk {
		m.applies = m.applicable(action)
	} else {
		m.applies = nil
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
	if m.bulk && m.items != nil {
		return strings.ToLower(string(m.action)) + ": " + m.bulkTotals()
	}
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
	case core.ActionRefused:
		return verb + " refused — nothing was sent"
	default:
		return verb + " outcome unknown"
	}
}

// Outcome returns the recorded result, or nil before one arrives.
func (m *Model) Outcome() *core.ActionResult { return m.outcome }

// BulkItems returns the bulk outcome, nil before it arrives.
func (m *Model) BulkItems() []BulkItem { return append([]BulkItem(nil), m.items...) }

func (m *Model) Cancel() {
	if m.state == StateMenu || m.state == StateConfirm || m.state == StateTypedName || m.state == StateFinal {
		m.state = StateIdle
		m.typedName = ""
	}
}

// OpenMenu presents the actions that apply to the targets.
func (m *Model) OpenMenu() {
	if !m.allowActions || m.readOnly || m.demo {
		m.Open(core.ActionResume)
		return
	}
	m.state = StateMenu
	m.reason = ""
}

// Confirm passes the yes step. For delete it leads to the final screen
// instead of sending anything.
func (m *Model) Confirm() bool {
	if m.state != StateConfirm {
		return false
	}
	if m.action == core.ActionDelete {
		m.state = StateFinal
		return false
	}
	return m.submitConfirmed("")
}

// ConfirmFinal passes delete's final screen. It is the only way a delete
// request is ever built.
func (m *Model) ConfirmFinal() bool {
	if m.state != StateFinal || m.action != core.ActionDelete {
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

// typedGate is what the terminate gate expects to be typed: the workflow's
// name when it reaches one workflow, the number of workflows otherwise.
func (m *Model) typedGate() string {
	if m.bulk {
		if len(m.applies) == 1 {
			return m.applies[0].Ref.Name
		}
		return strconv.Itoa(len(m.applies))
	}
	return m.ref.Name
}

func (m *Model) Submit() bool {
	if m.state != StateTypedName || m.typedName != m.typedGate() {
		return false
	}
	return m.submitConfirmed(m.typedName)
}

func (m *Model) submitConfirmed(typed string) bool {
	if m.state == StateSubmitting || m.intent != nil || m.bulkIntent != nil {
		return false
	}
	final := m.action == core.ActionDelete
	if m.bulk {
		if len(m.applies) == 0 {
			return false
		}
		reqs := make([]core.ActionRequest, 0, len(m.applies))
		for _, t := range m.applies {
			c := core.Confirmation{Confirmed: true, Final: final}
			if m.action == core.ActionTerminate {
				if len(m.applies) == 1 {
					c.TypedName = typed
				} else {
					c.BulkSize, c.TypedCount = len(m.applies), typed
				}
			}
			reqs = append(reqs, core.ActionRequest{Ref: t.Ref, Action: m.action, Confirmation: c})
		}
		m.bulkIntent = reqs
		m.intentCount++
		m.sent = 0
		m.stopRequested = false
		m.items = nil
		m.state = StateSubmitting
		return true
	}
	req := core.ActionRequest{Ref: m.ref, Action: m.action, Confirmation: core.Confirmation{Confirmed: true, TypedName: typed, Final: final}}
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

// SetBulkProgress records how many bulk requests have finished.
func (m *Model) SetBulkProgress(done int) {
	if m.state == StateSubmitting {
		m.sent = done
	}
}

// StopRequested reports whether the reader asked to send nothing more.
func (m *Model) StopRequested() bool { return m.stopRequested }

// SetBulkOutcome records the per-target results of a finished bulk action.
func (m *Model) SetBulkOutcome(items []BulkItem) {
	if m.state != StateSubmitting || !m.bulk {
		return
	}
	m.items = append([]BulkItem{}, items...)
	m.state = StateOutcome
}

// Hints are the key hints for the footer while the pane is open.
func (m *Model) Hints() string {
	switch m.state {
	case StateMenu:
		return "letter choose  esc cancel"
	case StateConfirm:
		return "y confirm  n/enter/esc cancel"
	case StateTypedName:
		return "type to confirm  enter submit  esc cancel"
	case StateFinal:
		return "D delete  any other key cancels"
	case StateSubmitting:
		if m.bulk {
			return "esc stop after the request in flight"
		}
		return "waiting for the server"
	default:
		return "enter/esc close"
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch m.state {
		case StateTypedName:
			m.updateTypedName(key)
		case StateFinal:
			m.updateFinal(key)
		default:
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
	if m.state == StateSubmitting && m.bulkIntent != nil {
		reqs := m.bulkIntent
		m.bulkIntent = nil // one-shot, like the single intent
		return m, func() tea.Msg { return BulkIntentMsg{Requests: reqs} }
	}
	return m, nil
}

// updateFinal handles delete's final screen: capital D confirms and every
// other key cancels. The key is compared as reported, never lowercased,
// because lowercasing it is exactly what would let the d that opened the
// delete finish it.
func (m *Model) updateFinal(key tea.KeyPressMsg) {
	if key.String() == finalDeleteKey {
		m.ConfirmFinal()
		return
	}
	m.Cancel()
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
	k := strings.ToLower(key.String())
	if m.state == StateMenu {
		if action, ok := menuKeys[k]; ok {
			// A verb that applies to no target is not offered, and its key
			// does nothing: opening it would only lead to a confirmation of
			// nothing, or to a request Argo is certain to reject.
			if len(m.applicable(action)) > 0 {
				m.Open(action)
			}
			return
		}
	}
	switch k {
	case "esc":
		switch m.state {
		case StateOutcome, StateUnavailable:
			m.state = StateIdle
		case StateSubmitting:
			if m.bulk {
				m.stopRequested = true
			}
		default:
			m.Cancel()
		}
	case "enter":
		if m.state == StateOutcome || m.state == StateUnavailable || m.state == StateConfirm {
			m.state = StateIdle
		}
	case "n":
		if m.state == StateConfirm {
			m.Cancel()
		}
	case "y":
		if m.state == StateConfirm {
			m.Confirm()
		}
	}
}

// menuKeys maps each menu letter to its action. The letters are the same on
// every route: u resume, z suspend, r retry, b resubmit ("submit again"),
// s stop, t terminate, d delete. Retry needs its own key because resume
// already has u, and sharing one would leave the restart unreachable.
var menuKeys = map[string]core.Action{
	"u": core.ActionResume,
	"z": core.ActionSuspend,
	"r": core.ActionRetry,
	"b": core.ActionResubmit,
	"s": core.ActionStop,
	"t": core.ActionTerminate,
	"d": core.ActionDelete,
}

// menuKey is the letter for action, the inverse of menuKeys.
func menuKey(action core.Action) string {
	for k, a := range menuKeys {
		if a == action {
			return k
		}
	}
	return "?"
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

// SetTheme replaces the style set the pane is drawn in.
func (m *Model) SetTheme(t shared.Theme) { m.theme = t }

func actionLabel(a core.Action) string { return strings.ToUpper(string(a)) }
func target(ref core.Ref) string {
	return fmt.Sprintf("%s/%s (UID: %s)", shared.Sanitize(ref.Namespace), shared.Sanitize(ref.Name), shared.Sanitize(ref.UID))
}
