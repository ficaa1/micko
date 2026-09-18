// wire.go — JSON decoding of the pinned v4.1.2 wire shapes
// (docs/development.md). Unknown server fields are dropped from
// typed DTOs but preserved verbatim in Workflow.Resource (the raw bytes are
// captured before typed decoding).
package argo

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// listEnvelope mirrors GET /api/v1/workflows/{namespace}. metadata.continue
// and metadata.resourceVersion are opaque strings — decoded verbatim, never
// parsed (docs/development.md; LIST-02/03).
type listEnvelope struct {
	Metadata struct {
		Continue        string `json:"continue"`
		ResourceVersion string `json:"resourceVersion"`
		// RemainingItemCount present only with the opt-in label selector;
		// decoded but unused (docs/development.md).
		RemainingItemCount *int64 `json:"remainingItemCount"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

// workflowLongEnvelope captures the one-level wrapper sometimes produced by
// deployments that marshal through proto3 JSON: result wraps Workflow
// (docs/development.md).
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
		Annotations       map[string]string `json:"annotations"`
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

// suspendedEnvelope is the gate scan's answer: the identity of a workflow and
// only the two node fields that say whether it waits for a human.
type suspendedEnvelope struct {
	Items []struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
		Status struct {
			Nodes map[string]struct {
				Type  string `json:"type"`
				Phase string `json:"phase"`
			} `json:"nodes"`
		} `json:"status"`
	} `json:"items"`
}

// decodeSuspendedUIDs reports which of the scanned workflows hold a Suspend
// node in a Running phase, keyed by workflow UID.
//
// The list body it reads carries node maps, which are the bulk of a workflow
// object. The scan is therefore kept to the workflows that can still be
// waiting, and the rest of the list is fetched without node data at all.
func decodeSuspendedUIDs(body []byte) (map[string]bool, error) {
	var env suspendedEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode gate scan: %w", err)
	}
	out := make(map[string]bool, len(env.Items))
	for _, it := range env.Items {
		if it.Metadata.UID == "" {
			continue
		}
		for _, n := range it.Status.Nodes {
			if n.Type == "Suspend" && n.Phase == "Running" {
				out[it.Metadata.UID] = true
				break
			}
		}
	}
	return out, nil
}

// decodeWorkflowDetail decodes a detail response, preserving the entire raw
// body verbatim in Workflow.Resource (docs/development.md: raw bytes for
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
	// Absent timestamps stay nil (docs/development.md: absent ⇒ not yet
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
	wf.PodNameVersion = w.Metadata.Annotations[podNameFormatAnnotation]
	wf.Summary.Suspended = hasRunningSuspendNode(wf.Nodes)
	resolvePodNames(&wf)
	return wf, nil
}

// podNameFormatAnnotation is the annotation Argo writes on every workflow it
// creates to record which pod naming scheme that workflow uses. The server is
// the only authority on this; argo-tui never assumes a default.
const podNameFormatAnnotation = "workflows.argoproj.io/pod-name-format"

// hasRunningSuspendNode reports whether the workflow is parked on a manual
// approval gate. A Suspend node in phase Running is exactly the state a
// Resume clears, so the list can tell "running" apart from "waiting for me".
func hasRunningSuspendNode(nodes map[string]core.Node) bool {
	for _, n := range nodes {
		if n.Type == "Suspend" && n.Phase == "Running" {
			return true
		}
	}
	return false
}

// resolvePodNames fills Node.PodName for pod-backed nodes, using the naming
// scheme the SERVER recorded on the workflow. With no annotation nothing is
// derived: a guessed pod name would send log requests to a pod that may
// belong to another workflow, so the UI would rather offer no node-scoped
// logs at all (docs/development.md).
func resolvePodNames(wf *core.Workflow) {
	if wf.PodNameVersion == "" || len(wf.Nodes) == 0 {
		return
	}
	for id, n := range wf.Nodes {
		if !podBackedNodeType(n.Type) {
			continue
		}
		name := generatePodName(wf.Summary.Ref.Name, n.Name, nodeTemplateName(n), n.ID, wf.PodNameVersion)
		if name == "" {
			continue
		}
		n.PodName = name
		wf.Nodes[id] = n
	}
}

// podBackedNodeType mirrors the pinned log-capable node list
// (docs/development.md). Only these nodes ever own a pod.
func podBackedNodeType(t string) bool {
	switch t {
	case "Pod", "ContainerSet", "HTTP", "Plugin":
		return true
	default:
		return false
	}
}

func nodeTemplateName(n core.Node) string {
	if n.TemplateName != "" {
		return n.TemplateName
	}
	return n.TemplateRefTemplate
}

// generatePodName reproduces Argo's own pod naming (workflow/util/pod_name.go)
// for the scheme the workflow declares.
//
//   - "v1": the node ID IS the pod name.
//   - "v2": "<workflow>-<template>-<fnv32(nodeName)>", with the prefix cut so
//     the whole name fits the 253-character Kubernetes limit.
//
// Any other value is unknown to this build and yields no name.
func generatePodName(workflowName, nodeName, templateName, nodeID, version string) string {
	switch version {
	case "v1":
		return nodeID
	case "v2":
	default:
		return ""
	}
	if workflowName == "" || nodeName == "" {
		return ""
	}
	if workflowName == nodeName {
		return workflowName
	}
	prefix := workflowName
	if templateName != "" {
		prefix = workflowName + "-" + templateName
	}
	prefix = ensurePodNamePrefixLength(prefix)
	// FNV-1a, matching Argo exactly. FNV-1 differs by one step order and
	// produces a completely different name, so this is verified against a
	// real cluster in podname_test.go rather than assumed.
	h := fnv.New32a()
	_, _ = h.Write([]byte(nodeName))
	return prefix + "-" + strconv.FormatUint(uint64(h.Sum32()), 10)
}

// ensurePodNamePrefixLength keeps room for the hash suffix inside the
// 253-character Kubernetes resource-name limit, exactly as Argo does.
func ensurePodNamePrefixLength(prefix string) string {
	const maxK8sResourceNameLength = 253
	const k8sNamingHashLength = 10
	maxPrefixLength := maxK8sResourceNameLength - k8sNamingHashLength
	if len(prefix) > maxPrefixLength-1 {
		return prefix[:maxPrefixLength-1]
	}
	return prefix
}

func tsOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// nodeEnvelope projects NodeStatus fields (json names pinned in
// docs/development.md). Unknown node fields are dropped from the typed DTO
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
	TemplateName  string     `json:"templateName"`
	TemplateRef   *struct {
		Name     string `json:"name"`
		Template string `json:"template"`
	} `json:"templateRef"`
	// Pod-capable node types may carry a pod name in outputs/inputs, but
	// PodName on the DTO is only ever populated from verified resolution
	// (docs/development.md rule). The adapter intentionally never guesses
	// from node ID (v0.1 policy, docs/development.md).
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
		TemplateName:  rn.TemplateName,
		TemplateRefTemplate: func() string {
			if rn.TemplateRef == nil {
				return ""
			}
			return rn.TemplateRef.Template
		}(),
		// PodName: never populated here — see resolvePodNames, which only
		// derives a name when the server stated the pod-name format.
	}, nil
}
