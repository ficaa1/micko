package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/core"
	"github.com/ficaa1/argo-tui/internal/ui/cronlist"
	"github.com/ficaa1/argo-tui/internal/ui/kindlist"
	"github.com/ficaa1/argo-tui/internal/ui/shared"
)

// cron.go registers the cron workflow kind: its view, and the call that
// lists it through the connection's core.CronLister.

// newCronKind builds the cron kind's definition for m.
func (m *Root) newCronKind() *kindDef {
	m.cronView = kindlist.New(cronlist.Spec(), shared.NewTheme(false))
	return &kindDef{
		pane: m.cronView,
		noun: "cron workflow",
		fetcher: func() (func(context.Context, string) (any, error), string) {
			lister := m.deps.cronLister
			if lister == nil {
				return nil, "this connection cannot list cron workflows"
			}
			return func(ctx context.Context, ns string) (any, error) {
				return lister.ListCronWorkflows(ctx, ns)
			}, ""
		},
		apply: func(items any, now time.Time) {
			cws, _ := items.([]core.CronWorkflow)
			m.cronView.SetItems(cws, now)
		},
	}
}

// showCron is the cron workflows kind's show.
func (m *Root) showCron() tea.Cmd { return m.showKind(RouteCron, "cron workflows") }
