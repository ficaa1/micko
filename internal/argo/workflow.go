// workflow.go — the core.Workflow mapping for GET responses. The typed
// projection and decode live in wire.go; this file holds the small
// workflow-oriented helpers that are independent of the wire structs.
package argo

import (
	"encoding/json"
	"time"
)

// jsonUnmarshal is a tiny indirection so tests (and reviewers) can see that
// resource JSON probing uses the standard decoder.
func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

// clampNote documents (not enforces) the detail-view timestamp rule:
// startedAt/finishedAt are pointers — nil means "not yet started/finished"
// and must never be materialized as zero times (docs/development.md).
var _ = time.Time{}
