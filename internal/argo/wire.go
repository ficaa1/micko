// wire.go — JSON decoding of the pinned v4.1.2 wire shapes
// (docs/protocol.md §2/§4/§5/§8). Unknown server fields are dropped from
// typed DTOs but preserved verbatim in Workflow.Resource (the raw bytes are
// captured before typed decoding).
package argo

import (
	"encoding/json"
	"fmt"
	"time"

	"argo-tui/internal/core"
)

// listEnvelope mirrors GET /api/v1/workflows/{namespace}. metadata.continue
// and metadata.resourceVersion are opaque strings — decoded verbatim, never
// parsed (docs/contracts.md §1; LIST-02/03).
type listEnvelope struct {
	Metadata struct {
		Continue        string `json:"continue"`
		ResourceVersion string `json:"resourceVersion"`
		// RemainingItemCount present only with the opt-in label selector;
		// decoded but unused (docs/protocol.md §4).
		RemainingItemCount *int64 `json:"remainingItemCount"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

// workflowLongEnvelope captures the one-level wrapper sometimes produced by
// deployments that marshal through proto3 JSON: result wraps Workflow
// (docs/protocol.md §2).
type workflowLongEnvelope struct {
	Result *rawWorkflow `json:"result"`
}

// rawWorkflow carries the raw bytes of one workflow object plus typed
// projection of the consumed fields. Unknown fields stay in Raw.
type rawWorkflow struct {
	Raw      json.RawMessage `json:"-"`
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		ResourceVersion   string            `json:"resourceVersion"`
		Labels            map[string]string `json:"labels"`
		CreationTimestamp *time.Time        `json:"creationTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase                    string                     `json:"phase"`
		Message                  string                     `json:"message"`
		StartedAt                *time.Time                 `json:"startedAt"`
		FinishedAt               *time.Time                 `json:"finishedAt"`
		Nodes                    map[string]json.RawMessage `json:"nodes"`
		CompressedNodes          string                     `json:"compressedNodes"`
		OffloadNodeStatusVersion string                     `json:"offloadNodeStatusVersion"`
	} `json:"status"`
}

// decodeListPage decodes one list page from a 200 response body.
func decodeListPage(body []byte) (core.Page, error) {
	var env listEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return core.Page{}, fmt.Errorf("decode list page: %w", err)
	}
	page := core.Page{
		Continue:        env.Metadata.Continue,
		ResourceVersion: env.Metadata.ResourceVersion,
		Items:           make([]core.Summary, 0, len(env.Items)),
	}
	for _, raw := range env.Items {
		wf, err := decodeRawWorkflow(raw)
		if err != nil {
			return core.Page{}, fmt.Errorf("decode list item: %w", err)
		}
		page.Items = append(page.Items, wf.Summary)
	}
	return page, nil
}

// decodeWorkflowDetail decodes a detail response, preserving the entire raw
// body verbatim in Workflow.Resource (docs/protocol.md §2: raw bytes for
// the resource view, never typed-drop).
func decodeWorkflowDetail(body []byte) (core.Workflow, error) {
	wf, err := decodeWorkflowBody(body, body)
	if err != nil {
		return core.Workflow{}, err
	}
	return wf, nil
}

// decodeWorkflowBody decodes either a bare workflow object or (tolerated)
// one wrapped inside a result field.
func decodeWorkflowBody(body, rawFor []byte) (core.Workflow, error) {
	var wrapped workflowLongEnvelope
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Result != nil {
		return wfFromRaw(wrapped.Result.Raw, rawFor)
	}
	wf, err := decodeRawWorkflow(body)
	if err != nil {
		return core.Workflow{}, err
	}
	wf.Resource = json.RawMessage(append([]byte(nil), rawFor...))
	return wf, nil
}

// decodeRawWorkflow unmarshals one workflow object's raw bytes into the
// typed projection and summary.
func decodeRawWorkflow(raw json.RawMessage) (core.Workflow, error) {
	wf, err := wfFromRaw(raw, nil)
	if err != nil {
		return core.Workflow{}, err
	}
	return wf, nil
}

func wfFromRaw(raw json.RawMessage, rawForDetail []byte) (core.Workflow, error) {
	var w rawWorkflow
	if err := json.Unmarshal(raw, &w); err != nil {
		return core.Workflow{}, fmt.Errorf("decode workflow: %w", err)
	}
	startedAt := w.Status.StartedAt
	finishedAt := w.Status.FinishedAt
	// Absent timestamps stay nil (docs/protocol.md §2: absent ⇒ not yet
	// started, never fabricated zero-times).
	wf := core.Workflow{
		Summary: core.Summary{
			Ref: core.Ref{
				Namespace: w.Metadata.Namespace,
				Name:      w.Metadata.Name,
				UID:       w.Metadata.UID,
			},
			ResourceVersion: w.Metadata.ResourceVersion,
			Phase:           w.Status.Phase,
			Message:         w.Status.Message,
			CreatedAt:       tsOrZero(w.Metadata.CreationTimestamp),
			StartedAt:       startedAt,
			FinishedAt:      finishedAt,
			Labels:          w.Metadata.Labels,
		},
	}
	if rawForDetail != nil {
		wf.Resource = json.RawMessage(append([]byte(nil), rawForDetail...))
	}
	// Node-data availability: offload/compressed markers prove node data
	// exists but is not hydrated — represent explicitly (DET-04), never as
	// an empty workflow.
	switch {
	case w.Status.OffloadNodeStatusVersion != "":
		wf.NodesAvailable = false
		wf.NodesUnavailableReason = fmt.Sprintf(
			"node status is offloaded (offloadNodeStatusVersion %q) and the server did not hydrate it in this response",
			w.Status.OffloadNodeStatusVersion)
	case w.Status.CompressedNodes != "":
		wf.NodesAvailable = false
		wf.NodesUnavailableReason = "node status is compressed (legacy compressedNodes) and the server did not hydrate it in this response"
	case w.Status.Nodes == nil:
		// Absent node map entirely: for a not-yet-started workflow this is
		// normal; represent as available-but-empty for the outline, since
		// no offload marker contradicts it. The UI distinguishes empty via
		// node count.
		wf.NodesAvailable = true
	default:
		wf.NodesAvailable = true
		wf.Nodes = make(map[string]core.Node, len(w.Status.Nodes))
		for id, nodeRaw := range w.Status.Nodes {
			node, err := decodeNode(id, nodeRaw)
			if err != nil {
				return core.Workflow{}, fmt.Errorf("decode node %q: %w", id, err)
			}
			wf.Nodes[id] = node
		}
	}
	return wf, nil
}

func tsOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// nodeEnvelope projects NodeStatus fields (json names pinned in
// docs/protocol.md §8). Unknown node fields are dropped from the typed DTO
// but the workflow detail's Resource retains them.
type rawNode struct {
	Name          string     `json:"name"`
	DisplayName   string     `json:"displayName"`
	Type          string     `json:"type"`
	Phase         string     `json:"phase"`
	Message       string     `json:"message"`
	BoundaryID    string     `json:"boundaryID"`
	Children      []string   `json:"children"`
	OutboundNodes []string   `json:"outboundNodes"`
	StartedAt     *time.Time `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	// Pod-capable node types may carry a pod name in outputs/inputs, but
	// PodName on the DTO is only ever populated from verified resolution
	// (docs/contracts.md rule). The adapter intentionally never guesses
	// from node ID (v0.1 policy, docs/protocol.md §8).
}

func decodeNode(id string, raw json.RawMessage) (core.Node, error) {
	var rn rawNode
	if err := json.Unmarshal(raw, &rn); err != nil {
		return core.Node{}, err
	}
	return core.Node{
		ID:            id,
		Name:          rn.Name,
		DisplayName:   rn.DisplayName,
		Type:          rn.Type,
		Phase:         rn.Phase,
		Message:       rn.Message,
		BoundaryID:    rn.BoundaryID,
		Children:      rn.Children,
		OutboundNodes: rn.OutboundNodes,
		StartedAt:     rn.StartedAt,
		FinishedAt:    rn.FinishedAt,
		// PodName: never populated by A1 — verified resolution only.
	}, nil
}
