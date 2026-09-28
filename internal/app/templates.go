package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/kindlist"
	"github.com/ficaa1/micko/internal/ui/templatelist"
)

// templates.go registers the two template kinds: WorkflowTemplates, listed in
// the session's namespace like every other kind, and ClusterWorkflowTemplates,
// which belong to no namespace.

// newTemplateKind builds the WorkflowTemplate kind's definition for m.
func (m *Root) newTemplateKind() *kindDef {
	m.tmplView = kindlist.New(templatelist.Spec(), m.theme)
	return &kindDef{
		pane: m.tmplView,
		noun: "workflow template",
		fetcher: func() (func(context.Context, string) (any, error), string) {
			lister := m.deps.templateLister
			if lister == nil {
				return nil, "this connection cannot list workflow templates"
			}
			return func(ctx context.Context, ns string) (any, error) {
				return lister.ListWorkflowTemplates(ctx, ns)
			}, ""
		},
		apply: func(items any, now time.Time) {
			ts, _ := items.([]core.WorkflowTemplate)
			m.tmplView.SetItems(ts, now)
		},
	}
}

// newClusterTemplateKind builds the ClusterWorkflowTemplate kind's definition
// for m. The namespace its fetch is given is always empty and not used.
func (m *Root) newClusterTemplateKind() *kindDef {
	m.ctmplView = kindlist.New(templatelist.ClusterSpec(), m.theme)
	return &kindDef{
		pane: m.ctmplView,
		noun: "cluster workflow template",
		fetcher: func() (func(context.Context, string) (any, error), string) {
			lister := m.deps.clusterTemplateLister
			if lister == nil {
				return nil, "this connection cannot list cluster workflow templates"
			}
			return func(ctx context.Context, _ string) (any, error) {
				return lister.ListClusterWorkflowTemplates(ctx)
			}, ""
		},
		apply: func(items any, now time.Time) {
			ts, _ := items.([]core.WorkflowTemplate)
			m.ctmplView.SetItems(ts, now)
		},
	}
}

// showTemplates is the workflow templates kind's show.
func (m *Root) showTemplates() tea.Cmd {
	return m.showKind(RouteTemplates, "workflow templates")
}

// showClusterTemplates is the cluster workflow templates kind's show.
func (m *Root) showClusterTemplates() tea.Cmd {
	return m.showKind(RouteClusterTemplates, "cluster workflow templates")
}

// clusterScopedRoute reports whether the active route lists a kind that
// belongs to no namespace. The namespace keys do not apply there, and the
// header says so instead of naming a namespace the list ignores.
func (m *Root) clusterScopedRoute() bool {
	def := m.kind(m.route)
	return def != nil && !def.pane.Namespaced()
}
