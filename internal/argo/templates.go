package argo

import (
	"context"
	"encoding/json"

	"github.com/ficaa1/micko/internal/core"
)

var (
	_ core.TemplateLister        = (*Client)(nil)
	_ core.ClusterTemplateLister = (*Client)(nil)
)

// templatePathPrefix is the WorkflowTemplate list route,
// GET /api/v1/workflow-templates/{namespace}; an empty namespace keeps the
// trailing slash and lists every namespace. clusterTemplatePath is the
// ClusterWorkflowTemplate list, GET /api/v1/cluster-workflow-templates,
// which has no namespace segment at all. Neither takes a fields projection.
const (
	templatePathPrefix  = "/api/v1/workflow-templates/"
	clusterTemplatePath = "/api/v1/cluster-workflow-templates"
)

// descriptionAnnotation is the annotation the Argo UI reads a template's
// one-line description from.
const descriptionAnnotation = "workflows.argoproj.io/description"

// templateKinds are the fields that define a template, in the order the
// type is looked for. Exactly one is set on a valid template.
var templateKinds = []string{
	"container", "script", "dag", "steps", "suspend", "resource", "data",
	"http", "plugin", "containerSet",
}

// rawTemplate is the consumed projection of a (Cluster)WorkflowTemplate. The
// templates are kept as field maps: which defining field is present is the
// template's type, and nothing else of them is needed.
type rawTemplate struct {
	Metadata struct {
		rawObjectMeta
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Entrypoint          string                       `json:"entrypoint"`
		Arguments           rawSpecArguments             `json:"arguments"`
		ServiceAccountName  string                       `json:"serviceAccountName"`
		Templates           []map[string]json.RawMessage `json:"templates"`
		WorkflowTemplateRef *struct {
			Name         string `json:"name"`
			ClusterScope bool   `json:"clusterScope"`
		} `json:"workflowTemplateRef"`
	} `json:"spec"`
}

// ListWorkflowTemplates implements core.TemplateLister.
func (c *Client) ListWorkflowTemplates(ctx context.Context, namespace string) ([]core.WorkflowTemplate, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	if namespace == "" {
		if err := c.checkClusterScope(ctx); err != nil {
			return nil, err
		}
	}
	return c.listTemplates(ctx, kindPath(templatePathPrefix, namespace), "workflow template list")
}

// ListClusterWorkflowTemplates implements core.ClusterTemplateLister. A
// server started for one managed namespace may not serve cluster templates;
// its answer is shown as it comes.
func (c *Client) ListClusterWorkflowTemplates(ctx context.Context) ([]core.WorkflowTemplate, error) {
	ctx, cancel := c.withUnaryDeadline(ctx)
	defer cancel()
	return c.listTemplates(ctx, clusterTemplatePath, "cluster workflow template list")
}

func (c *Client) listTemplates(ctx context.Context, path, what string) ([]core.WorkflowTemplate, error) {
	var out []core.WorkflowTemplate
	err := c.listPages(ctx, path, what, func(body []byte) (string, error) {
		var env kindListEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return "", err
		}
		for _, raw := range env.Items {
			t, err := decodeTemplate(raw)
			if err != nil {
				return "", err
			}
			out = append(out, t)
		}
		return env.Metadata.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// decodeTemplate maps one template object, keeping its raw bytes.
func decodeTemplate(raw json.RawMessage) (core.WorkflowTemplate, error) {
	var w rawTemplate
	if err := json.Unmarshal(raw, &w); err != nil {
		return core.WorkflowTemplate{}, err
	}
	t := core.WorkflowTemplate{
		Namespace:      w.Metadata.Namespace,
		Name:           w.Metadata.Name,
		UID:            w.Metadata.UID,
		CreatedAt:      tsOrZero(w.Metadata.CreationTimestamp),
		Labels:         w.Metadata.Labels,
		Description:    w.Metadata.Annotations[descriptionAnnotation],
		Entrypoint:     w.Spec.Entrypoint,
		Arguments:      arguments(w.Spec.Arguments.Parameters),
		ServiceAccount: w.Spec.ServiceAccountName,
		Resource:       append(json.RawMessage(nil), raw...),
	}
	if ref := w.Spec.WorkflowTemplateRef; ref != nil && ref.Name != "" {
		t.WorkflowTemplateRef = ref.Name
		if ref.ClusterScope {
			t.WorkflowTemplateRef = "cluster/" + ref.Name
		}
	}
	for _, tmpl := range w.Spec.Templates {
		info := core.TemplateInfo{Type: "unknown"}
		if n, ok := anyString(tmpl["name"]); ok {
			info.Name = n
		}
		for _, k := range templateKinds {
			if v, ok := tmpl[k]; ok && string(v) != "null" {
				info.Type = k
				break
			}
		}
		t.Templates = append(t.Templates, info)
	}
	return t, nil
}
