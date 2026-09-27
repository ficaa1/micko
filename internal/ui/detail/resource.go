package detail

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/shared"
)

// RenderResource renders the workflow's raw server JSON as normalized YAML
// (DET-03), with parameter/output and secret-shaped values redacted by
// default (DET-12 ⛨) and every surface passed through the shared terminal
// sanitizer (DET-13 ⛨).
//
// revealForSession is the explicit, session-only reveal state: it is a pure
// function argument — the resource view keeps no persisted or sticky reveal
// state (plan §2: "explicit reveal is session-only"). The caller (view
// model) owns the per-session flag.
//
// The function never echoes invalid JSON raw: it reports an explicit
// protocol explanation instead.
func RenderResource(wf core.Workflow, revealForSession bool) string {
	return RenderManifest(wf.Resource, revealForSession)
}

// RenderManifest renders any Argo object's raw JSON — a cron workflow, a
// template — exactly as the resource tab renders a workflow: normalized
// YAML, the same redaction, the same sanitizing. The raw views of the other
// resource kinds call it so there is one redaction rule, not one per view.
func RenderManifest(raw []byte, revealForSession bool) string {
	if len(raw) == 0 {
		return "(no resource payload returned by the server)\n"
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "(resource payload is not valid JSON; nothing to display " +
			"— normalized rendering unavailable)\n"
	}

	redacted := redactValue(doc, revealForSession, false)
	yamlOut, err := jsonToYAML(redacted)
	if err != nil {
		return fmt.Sprintf("(resource normalization failed: %v)\n", err)
	}
	// DET-13 ⛨: the resource content may embed attacker-controlled strings
	// (labels, annotations, messages) — always sanitize before returning,
	// preserving safe newlines and tabs (shared.Sanitize keeps \n and \t).
	return shared.Sanitize(yamlOut)
}

// redactedMarker stands in for a value the reader has not revealed. The
// resource tab and the node info panel use the same marker, so a reader
// learns one sign for "hidden until v".
const redactedMarker = "[REDACTED]"

// sensitiveValueKeys are JSON keys whose string values are collapsed by
// default wherever they appear: workflow arguments/outputs use "value" and
// "result", and credential-shaped names are redacted unconditionally
// (plan §2: collapse parameter/output values by default).
var sensitiveValueKeys = map[string]bool{
	"value":    true,
	"result":   true,
	"secret":   true,
	"token":    true,
	"apikey":   true,
	"api_key":  true,
	"password": true,
}

// sensitiveContentKeys are keys under which deeper string values are
// considered sensitive context (e.g. outputs.parameters, arguments).
var sensitiveContextKeys = map[string]bool{
	"parameters": true,
	"outputs":    true,
	"arguments":  true,
}

// redactValue walks the decoded JSON and replaces sensitive values with
// redaction markers unless the session revealed them. Key names and
// parameters/outputs path context drive the decision; string values under a
// sensitive context get shell/credential-shape redaction via
// shared.RedactTokens as defense-in-depth.
func redactValue(v any, reveal bool, sensitiveCtx bool) any {
	switch tv := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(tv))
		for k, val := range tv {
			lk := strings.ToLower(k)
			ctx := sensitiveCtx || sensitiveContextKeys[lk]
			if sensitiveValueKeys[lk] && !reveal {
				out[k] = redactedMarker
				continue
			}
			out[k] = redactValue(val, reveal, ctx)
		}
		return out
	case []any:
		out := make([]any, len(tv))
		for i, e := range tv {
			out[i] = redactValue(e, reveal, sensitiveCtx)
		}
		return out
	case string:
		if !reveal && sensitiveCtx {
			return redactedMarker
		}
		if !reveal {
			return shared.RedactTokens(tv)
		}
		return tv
	default:
		return v
	}
}

// jsonToYAML renders decoded JSON as normalized YAML using the pinned
// yaml.v3 dependency (go.mod: gopkg.in/yaml.v3, introduced by F1 for
// config — no new module requirements).
func jsonToYAML(v any) (string, error) {
	return yamlMarshalJSONShaped(v)
}
