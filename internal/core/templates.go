package core

import (
	"context"
	"encoding/json"
	"time"
)

// WorkflowTemplate is the read projection of a WorkflowTemplate or
// ClusterWorkflowTemplate; a cluster template has no namespace. A template
// has no status: its runs are found by the label the controller puts on them.
type WorkflowTemplate struct {
	// Namespace is empty for a ClusterWorkflowTemplate.
	Namespace string
	Name      string
	UID       string
	CreatedAt time.Time
	Labels    map[string]string
	// Description is the workflows.argoproj.io/description annotation, the
	// one-line summary the Argo UI shows for a template.
	Description string

	Entrypoint string
	// Arguments are spec.arguments.parameters. Their values can hold
	// secrets, so views redact them unless the reader reveals them.
	Arguments []Argument
	// Templates are the spec's templates, by name and kind.
	Templates []TemplateInfo
	// ServiceAccount is spec.serviceAccountName.
	ServiceAccount string
	// WorkflowTemplateRef names the template this one runs, when its spec
	// refers to another instead of carrying its own templates.
	WorkflowTemplateRef string

	// Resource is the object as the server returned it, for the raw view. It
	// is parsed and redacted before rendering, never echoed raw.
	Resource json.RawMessage
}

// TemplateInfo is one entry of a spec's templates list.
type TemplateInfo struct {
	Name string
	// Type is the field that defines the template: container, script,
	// dag, steps, suspend, resource, data, http, plugin or containerSet.
	// "unknown" when none of them is set.
	Type string
}

// TemplateLister lists WorkflowTemplates. It is optional: a Reader that does
// not implement it has no template view, and the pane says so.
type TemplateLister interface {
	// ListWorkflowTemplates returns the templates in namespace, or in every
	// namespace the token may read when namespace is empty.
	ListWorkflowTemplates(ctx context.Context, namespace string) ([]WorkflowTemplate, error)
}

// ClusterTemplateLister lists ClusterWorkflowTemplates, which belong to no
// namespace. It is a separate interface from TemplateLister because a token
// is granted the two independently.
type ClusterTemplateLister interface {
	ListClusterWorkflowTemplates(ctx context.Context) ([]WorkflowTemplate, error)
}
