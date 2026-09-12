// Package core defines the frozen, transport-independent data contracts for
// argo-tui (plan §4 "Frozen contract surface"). These types are consumed by
// UI workers (B1/C1/D1) and implemented by the transport adapter (A1, in
// package internal/argo) and the fake backend (internal/testkit).
//
// Freezing rules (plan §4):
//   - Exact field/type names are recorded in docs/development.md; changes go
//     through F with a plan/ADR amendment before dependent work resumes.
//   - DTOs carry only consumed fields; unknown server fields stay in
//     Workflow.Resource as raw JSON (docs/development.md).
//   - Resource versions and continuation tokens are opaque strings: compare
//     by equality only, never parse or construct.
package core

import (
	"context"
	"encoding/json"
	"time"
)

// Ref identifies a workflow by namespace, name and server-assigned UID.
// UID is the authoritative identity: same name with a new UID is a
// different workflow (plan §2; acceptance matrix LIST-12).
type Ref struct {
	Namespace string
	Name      string
	UID       string
}

// Query describes a server-side list request.
type Query struct {
	Namespace     string
	LabelSelector string
	// Continue is an opaque continuation token returned by a previous
	// Page. It is passed back verbatim, never parsed or constructed
	// (docs/development.md — v4.1.2 uses integer-string offsets, but the
	// client must not rely on that).
	Continue string
	// Limit is the page size; 0 means server default.
	Limit int64
}

// Summary is the list-level projection of a workflow.
type Summary struct {
	Ref             Ref
	ResourceVersion string
	Phase           string
	Message         string
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	Labels          map[string]string
	// Suspended reports that the workflow currently holds at least one
	// Suspend node in a Running phase, i.e. it waits for a human Resume.
	// It is derived from the node map the server sent with this summary;
	// when the server sends no node data it stays false and the list says
	// nothing rather than guessing (docs/development.md).
	Suspended bool
}

// Page is one result page plus list metadata.
type Page struct {
	Items []Summary
	// Continue is empty when no further page exists. Non-empty values are
	// opaque and must be passed back verbatim.
	Continue string
	// ResourceVersion is an opaque snapshot marker (compare by equality only).
	ResourceVersion string
}

// Node is one workflow node. Not every node is a pod; children and
// outboundNodes have distinct semantics (docs/development.md).
type Node struct {
	ID          string
	Name        string
	DisplayName string
	Type        string
	Phase       string
	Message     string
	BoundaryID  string
	// Children are child node IDs (template boundaries); they are NOT
	// dependency edges.
	Children []string
	// OutboundNodes are the last nodes before a template is considered
	// complete. They are NOT interchangeable with Children
	// (docs/development.md).
	OutboundNodes []string
	StartedAt     *time.Time
	FinishedAt    *time.Time
	// TemplateName is the node's own template name. TemplateRefTemplate is
	// the template named through a templateRef. Exactly one is normally
	// set; both feed the version-aware pod-name resolution below.
	TemplateName        string
	TemplateRefTemplate string
	// PodName is populated only from verified version-aware resolution
	// (docs/development.md; v0.1 policy: do not guess pod names).
	PodName string
}

// Workflow is the detail-level view of one workflow.
type Workflow struct {
	Summary Summary
	Nodes   map[string]Node
	// NodesAvailable reports whether the node map is usable. An empty
	// Nodes map with NodesAvailable=false must be rendered as
	// "node status unavailable", never as an empty workflow (plan §5).
	NodesAvailable         bool
	NodesUnavailableReason string
	// Resource is the raw server-returned workflow JSON, preserved
	// verbatim for the resource view (docs/development.md). It is
	// structured-parsed and redacted before rendering, never echoed raw.
	Resource json.RawMessage
	// PodNameVersion is the pod naming scheme the server recorded on this
	// workflow (annotation workflows.argoproj.io/pod-name-format). "v1"
	// means the node ID is the pod name; "v2" is the hashed form. An empty
	// value means the server said nothing, and no pod name is derived.
	PodNameVersion string
}

// LogRequest describes a log stream to open.
type LogRequest struct {
	Ref Ref
	// PodName empty means workflow-wide logs (all pods). Pod-scoped
	// streams require a verified pod name (user-entered in v0.1).
	PodName string
	// Container is always sent explicitly; the default "main" stays
	// visible/editable in the UI, never silently guessed (plan §2).
	Container  string
	Follow     bool
	Timestamps bool
	TailLines  int64
}

// LogRecord is one log line delivered by a stream. ReceivedAt is the local
// receipt time (fake clock or wall clock); source timestamps, when
// requested, are textual content — LogRecord never fabricates metadata
// (docs/development.md: LogEntry carries only content and podName).
type LogRecord struct {
	PodName    string
	Container  string
	Content    string
	ReceivedAt time.Time
}

// Reader is the frozen transport-independent read contract. Implemented by
// the REST adapter (A1), the testkit fake, and the demo backend; UI code
// never imports transport packages.
type Reader interface {
	List(context.Context, Query) (Page, error)
	Get(context.Context, Ref) (Workflow, error)
	// StreamLogs delivers records serially to the callback. The callback
	// runs on one goroutine, blocks only on a bounded cancelable queue,
	// and returns an error to stop the stream. StreamLogs returns nil for
	// a clean finite EOF; context cancellation is distinguishable from
	// network failure; the caller owns the reconnect policy (plan §4).
	StreamLogs(context.Context, LogRequest, func(LogRecord) error) error
}
