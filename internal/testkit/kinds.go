package testkit

import (
	"context"
	"fmt"
	"strings"

	"github.com/ficaa1/micko/internal/core"
)

// kinds.go is the fake's side of the resource kinds beside workflows: the
// list calls the views read them through, and the label selector the
// drill-down from a kind to its workflows sends.

var (
	_ core.CronLister            = (*FakeReader)(nil)
	_ core.TemplateLister        = (*FakeReader)(nil)
	_ core.ClusterTemplateLister = (*FakeReader)(nil)
	_ core.ArchiveReader         = (*FakeReader)(nil)
)

// ListArchivedWorkflows implements core.ArchiveReader over
// ArchivedWorkflows, filtered by namespace and label selector, in pages whose
// continuation token is an offset, as the server's is.
func (f *FakeReader) ListArchivedWorkflows(ctx context.Context, q core.ArchiveQuery) (core.ArchivePage, error) {
	if err := ctx.Err(); err != nil {
		return core.ArchivePage{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KindCalls++
	if f.ArchiveErr != nil {
		return core.ArchivePage{}, f.ArchiveErr
	}
	var all []core.Workflow
	for _, wf := range f.ArchivedWorkflows {
		if q.Namespace != "" && wf.Summary.Ref.Namespace != q.Namespace {
			continue
		}
		if !MatchLabels(q.LabelSelector, wf.Summary.Labels) {
			continue
		}
		all = append(all, wf)
	}
	offset := 0
	if q.Continue != "" {
		if _, err := fmt.Sscanf(q.Continue, "%d", &offset); err != nil || offset < 0 || offset > len(all) {
			return core.ArchivePage{}, core.ErrInvalidf("bad continue token %q", q.Continue)
		}
	}
	end := len(all)
	if q.Limit > 0 && offset+int(q.Limit) < end {
		end = offset + int(q.Limit)
	}
	page := core.ArchivePage{Items: append([]core.Workflow(nil), all[offset:end]...)}
	if end < len(all) {
		page.Continue = fmt.Sprint(end)
	}
	return page, nil
}

// GetArchivedWorkflow implements core.ArchiveReader.
func (f *FakeReader) GetArchivedWorkflow(ctx context.Context, uid string) (core.Workflow, error) {
	if err := ctx.Err(); err != nil {
		return core.Workflow{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.GetCalls++
	if f.ArchiveErr != nil {
		return core.Workflow{}, f.ArchiveErr
	}
	for _, wf := range f.ArchivedWorkflows {
		if wf.Summary.Ref.UID == uid {
			return wf, nil
		}
	}
	return core.Workflow{}, core.ErrNotFoundf("not found")
}

// ListWorkflowTemplates implements core.TemplateLister over
// WorkflowTemplates, filtered by namespace unless it is empty.
func (f *FakeReader) ListWorkflowTemplates(ctx context.Context, namespace string) ([]core.WorkflowTemplate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KindCalls++
	if f.TemplateErr != nil {
		return nil, f.TemplateErr
	}
	var out []core.WorkflowTemplate
	for _, t := range f.WorkflowTemplates {
		if namespace == "" || t.Namespace == namespace {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListClusterWorkflowTemplates implements core.ClusterTemplateLister.
func (f *FakeReader) ListClusterWorkflowTemplates(ctx context.Context) ([]core.WorkflowTemplate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.KindCalls++
	if f.ClusterTemplateErr != nil {
		return nil, f.ClusterTemplateErr
	}
	return append([]core.WorkflowTemplate(nil), f.ClusterWorkflowTemplates...), nil
}

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
