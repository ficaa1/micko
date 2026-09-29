package app

import (
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/testkit"
)

// A profile's redactValues reaches every detail view, across switches.
func TestRedactValuesFollowsTheConnection(t *testing.T) {
	wf := workflowFixture("wf-1")
	m := newRoot(fixtureReader(wf), "demo", time.Second)
	shown := func() bool {
		m.detailView.SetWorkflow(wf, testkit.FixtureEpoch)
		m.detailView.SetSection("resource")
		body := strings.Join(m.detailView.BodyLines(), "\n")
		if strings.Contains(body, "values shown") == strings.Contains(body, "values redacted") {
			t.Fatalf("the resource status says neither or both:\n%s", body)
		}
		return strings.Contains(body, "values shown")
	}
	if !shown() {
		t.Fatal("values must be shown by default")
	}
	m.Adopt(&Connection{Reader: m.deps.reader, Namespace: "demo", Redact: true})
	if shown() {
		t.Fatal("a profile with redactValues must hide the values")
	}
	m.switchNamespace("demo-ml")
	if shown() {
		t.Fatal("a namespace switch must keep the profile's redaction")
	}
	m.Adopt(&Connection{Reader: m.deps.reader, Namespace: "demo"})
	if !shown() {
		t.Fatal("a profile without redactValues must show the values again")
	}
}
