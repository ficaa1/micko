package detail

import (
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/core"
)

// Model is the detail route's child Tea model (bubbletea v2 Model surface
// frozen in docs/contracts.md §7). It renders the composed detail view and
// emits intents only — the root alone converts them to network effects
// (plan §4). It never starts goroutines and holds no Reader.
type Model struct {
	// state is the loaded workflow detail (or zero value while loading).
	state DetailViewState
	// rawResource keeps the workflow's raw payload for session-only
	// reveal re-renders (nil until a workflow is applied).
	rawResource []byte
	// loaded marks whether a workflow detail has been applied.
	loaded bool
	// loading renders the loading state (root-driven).
	loading bool
	// lastErr renders the error state (root-driven; sanitized at render).
	lastErr string
	// notFound renders the not-found state (root-driven).
	notFound bool

	// tab is the active detail tab: "summary" | "nodes" | "resource".
	tab string
	// revealResource is the session-only explicit reveal for the resource
	// tab (DET-12 ⛨). It resets when the workflow (by UID) changes and is
	// never persisted anywhere.
	revealResource bool

	width, height int
}

// New builds the detail child model.
func New() *Model {
	return &Model{tab: "summary"}
}

// Compile-time interface check against the frozen v2 surface.
var _ tea.Model = (*Model)(nil)

// SetWorkflow applies a freshly loaded detail.
func (m *Model) SetWorkflow(wf core.Workflow, now time.Time) {
	var sameWorkflow bool
	if m.loaded {
		sameWorkflow = m.state.Summary.Ref.UID == wf.Summary.Ref.UID
	}
	m.state = DetailViewState{
		Summary:  wf.Summary,
		Outline:  BuildNodeOutline(wf, OutlineOptions{}),
		Resource: RenderResource(wf, false),
		Message:  wf.Summary.Message,
	}
	m.rawResource = append([]byte(nil), wf.Resource...)
	m.loaded = true
	m.loading = false
	m.lastErr = ""
	m.notFound = false
	if !sameWorkflow {
		// Reveal is session-only per workflow: a different UID resets it.
		m.revealResource = false
	}
}

// SetLoading marks the loading state (root-driven, before data arrives).
func (m *Model) SetLoading() {
	m.loading = true
}

// SetError renders an error state (sanitized by the view at render time).
func (m *Model) SetError(msg string) {
	m.loading = false
	m.notFound = false
	m.lastErr = msg
}

// SetNotFound renders the "workflow no longer available" state.
func (m *Model) SetNotFound() {
	m.loading = false
	m.lastErr = ""
	m.notFound = true
}

// SetSize implements the child view sizing contract (contracts §7).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
}

// Reveal is the session-only reveal state accessor (for tests/root).
func (m *Model) Reveal() bool { return m.revealResource }

// Update implements tea.Model. Tab switching (Tab), Esc back intent,
// session-only resource reveal (`v`) live here; intents bubble to the
// root as messages — the model never talks to a Reader (plan §4).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			m.tab = nextTab(m.tab)
			return m, nil
		case "esc":
			return m, backCmd()
		case "v":
			// Explicit, session-only reveal of redacted resource values.
			m.revealResource = !m.revealResource
			return m, nil
		default:
			return m, nil
		}

	default:
		return m, nil
	}
}

// backCmd emits the back intent (the root converts it to routing).
// BackMsg matches the root app's frozen back intent (app.BackMsg shape;
// detail defines its own message struct so the child package stays
// independent of internal/app — the root routes by type-agnostic handling
// or app maps it; contracts §4 pins the intent names).
type BackMsg struct{}

func backCmd() tea.Cmd { return func() tea.Msg { return BackMsg{} } }

// Init implements tea.Model — detail children have no autonomous effects.
func (m *Model) Init() tea.Cmd { return nil }

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.loading {
		return tea.NewView("detail: loading...")
	}
	switch {
	case m.notFound:
		return tea.NewView("workflow no longer available")
	case m.lastErr != "":
		return tea.NewView("detail error: " + m.lastErr)
	case !m.loaded:
		return tea.NewView("detail: (no workflow loaded)")
	}
	state := m.state
	// The resource pane reflects the session reveal state: re-render the
	// resource with values revealed when toggled this session.
	if m.revealResource {
		wf := core.Workflow{
			Summary:  state.Summary,
			Resource: m.rawResource,
		}
		state.Resource = RenderResource(wf, true)
	}
	return tea.NewView(RenderDetail(state, m.tab))
}

// nextTab cycles summary → nodes → resource → summary.
func nextTab(cur string) string {
	switch cur {
	case "summary":
		return "nodes"
	case "nodes":
		return "resource"
	default:
		return "summary"
	}
}
