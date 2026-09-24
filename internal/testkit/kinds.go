package testkit

import (
	"context"
	"strings"

	"github.com/ficaa1/argo-tui/internal/core"
)

// kinds.go is the fake's side of the resource kinds beside workflows: the
// list calls the views read them through, and the label selector the
// drill-down from a kind to its workflows sends.

var _ core.CronLister = (*FakeReader)(nil)

// ListCronWorkflows implements core.CronLister over CronWorkflows. An empty
// namespace answers every namespace, as the server does.
func (f *FakeReader) ListCronWorkflows(ctx context.Context, namespace string) ([]core.CronWorkflow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KindCalls++
	if f.CronErr != nil {
		return nil, f.CronErr
	}
	var out []core.CronWorkflow
	for _, cw := range f.CronWorkflows {
		if namespace == "" || cw.Namespace == namespace {
			out = append(out, cw)
		}
	}
	return out, nil
}

// MatchLabels reports whether labels satisfy a Kubernetes equality-based
// label selector: comma-separated requirements, each `k=v`, `k==v`, `k!=v`,
// `k` (present) or `!k` (absent), all of which must hold. An empty selector
// matches everything. A set-based requirement (`k in (a,b)`) is not
// understood and matches nothing, so a test that sends one sees an empty
// list instead of a silently unfiltered one.
func MatchLabels(selector string, labels map[string]string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return true
	}
	for _, req := range strings.Split(selector, ",") {
		req = strings.TrimSpace(req)
		switch {
		case strings.Contains(req, " in ") || strings.Contains(req, " notin ") || strings.Contains(req, "("):
			return false
		case strings.Contains(req, "!="):
			k, v, _ := strings.Cut(req, "!=")
			if labels[strings.TrimSpace(k)] == strings.TrimSpace(v) {
				return false
			}
		case strings.Contains(req, "=="):
			k, v, _ := strings.Cut(req, "==")
			if got, ok := labels[strings.TrimSpace(k)]; !ok || got != strings.TrimSpace(v) {
				return false
			}
		case strings.Contains(req, "="):
			k, v, _ := strings.Cut(req, "=")
			if got, ok := labels[strings.TrimSpace(k)]; !ok || got != strings.TrimSpace(v) {
				return false
			}
		case strings.HasPrefix(req, "!"):
			if _, ok := labels[strings.TrimPrefix(req, "!")]; ok {
				return false
			}
		default:
			if _, ok := labels[req]; !ok {
				return false
			}
		}
	}
	return true
}
