package detail

import (
	"testing"

	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

// Resource-redaction tests (DET-12 ⛨): parameter/output values are
// collapsed by default; reveal is explicit and session-only — the reveal
// state lives in the view model, never persisted anywhere.

// resourceFixture builds a workflow with sensitive-looking values.
func resourceFixture() core.Workflow {
	wf := testkit.SyntheticWorkflow("ns", "redact-wf", "Succeeded", testkit.FixtureEpoch)
	wf.Resource = []byte(`{
	  "metadata": {"name": "redact-wf", "namespace": "ns",
	    "annotations": {"workflows.argoproj.io/pod-name-format": "v2"}},
	  "spec": {
	    "arguments": {"parameters": [{"name": "api-key", "value": "sk-1234567890abcdef123456"}]},
	    "entrypoint": "main"
	  },
	  "status": {
	    "phase": "Succeeded",
	    "nodes": {"n1": {"outputs": {"parameters": [{"name": "password", "value": "hunter2-secret"}]}}},
	    "unknownFutureField": {"nested": [1, 2, {"deep": true}]}
	  }
	}`)
	return wf
}

// TestResourceViewRedactedByDefault pins DET-12: secret-shaped and
// parameter/output values are collapsed in the default view.
func TestResourceViewRedactedByDefault(t *testing.T) {
	wf := resourceFixture()
	out := RenderResource(wf, false)
	if out == "" {
		t.Fatal("empty resource view")
	}
	for _, secret := range []string{"sk-1234567890abcdef123456", "hunter2-secret"} {
		if containsSub(out, secret) {
			t.Errorf("default resource view leaks %q:\n%s", secret, out)
		}
	}
	if !containsSub(out, "[REDACTED]") {
		t.Errorf("default view must show redaction markers:\n%s", out)
	}
}

// TestResourceViewExplicitReveal pins the session-only reveal: only an
// explicit render with revealForSession=true shows values, and the call has
// no persistence side effects (pure function of its arguments).
func TestResourceViewExplicitReveal(t *testing.T) {
	wf := resourceFixture()
	// Default still redacted after an explicit reveal render (reveal is
	// per-call/session state, never sticky).
	_ = RenderResource(wf, true)
	out := RenderResource(wf, false)
	for _, secret := range []string{"sk-1234567890abcdef123456", "hunter2-secret"} {
		if containsSub(out, secret) {
			t.Errorf("reveal must be session-only; default render still leaks %q", secret)
		}
	}
	revealed := RenderResource(wf, true)
	for _, secret := range []string{"sk-1234567890abcdef123456", "hunter2-secret"} {
		if !containsSub(revealed, secret) {
			t.Errorf("explicit reveal must show values; missing %q:\n%s", secret, revealed)
		}
	}
}

// TestResourceViewUnknownFieldsPreserved pins DET-03: unknown server fields
// survive the YAML normalization verbatim in structure.
func TestResourceViewUnknownFieldsPreserved(t *testing.T) {
	wf := resourceFixture()
	out := RenderResource(wf, true)
	for _, want := range []string{"unknownFutureField", "deep", "pod-name-format", "entrypoint"} {
		if !containsSub(out, want) {
			t.Errorf("resource view lost field %q:\n%s", want, out)
		}
	}
}

// TestResourceViewSanitized pins DET-13 ⛨: terminal injection via resource
// metadata is impossible — control sequences are neutralized by the shared
// sanitizer before the view is returned.
func TestResourceViewSanitized(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "evil\x1b]0;pwned\x07-wf", "Running", testkit.FixtureEpoch)
	wf.Resource = []byte(`{"metadata":{"name":"evil\x1b]0;pwned\x07-wf","x":"a\u0007b"},"status":{"message":"\x1b[2Jclear"}}`)
	wf.Resource = []byte(`{"metadata":{"name":"n","x":"a\u0007b"},"status":{"message":"esc\u001b[2Jhere"}}`)
	out := RenderResource(wf, false)
	for _, bad := range []string{"\x1b", "\x07"} {
		if containsRune(out, bad[0]) {
			t.Errorf("resource view contains raw 0x%02x (terminal injection):\n%s", bad[0], out)
		}
	}
}

// TestResourceViewEmptyAndInvalidResource: nil/empty resource renders a
// placeholding message; invalid JSON must not panic and shows a protocol
// error line — never raw bytes echo.
func TestResourceViewEmptyAndInvalidResource(t *testing.T) {
	wf := testkit.SyntheticWorkflow("ns", "empty", "Running", testkit.FixtureEpoch)
	wf.Resource = nil
	out := RenderResource(wf, false)
	if out == "" {
		t.Fatal("nil resource must still render (explicit placeholder)")
	}

	wf2 := testkit.SyntheticWorkflow("ns", "bad", "Running", testkit.FixtureEpoch)
	wf2.Resource = []byte(`{not-json}`)
	out2 := RenderResource(wf2, false)
	if containsSub(out2, "{not-json}") {
		t.Errorf("invalid JSON echoed raw:\n%s", out2)
	}
	if !containsSub(out2, "unavailable") && !containsSub(out2, "protocol") && !containsSub(out2, "cannot") {
		t.Fatalf("invalid JSON must be explained, got:\n%s", out2)
	}
}

func containsSub(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOfSub(s, sub) >= 0
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsRune(s string, r byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == r {
			return true
		}
	}
	return false
}
