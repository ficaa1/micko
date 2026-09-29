package app

import (
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/testkit"
)

// A profile's redactValues reaches every detail view, across switches.
func TestRedactValuesFollowsTheConnection(t *testing.T) {
	m := newRoot(testkit.DemoReader(testkit.NewFakeClock(testkit.FixtureEpoch)), "demo", time.Second)
	if !m.detailView.Reveal() {
		t.Fatal("values must be shown by default")
	}
	m.Adopt(&Connection{Reader: m.deps.reader, Namespace: "demo", Redact: true})
	if m.detailView.Reveal() {
		t.Fatal("a profile with redactValues must hide the values")
	}
	m.switchNamespace("demo-ml")
	if m.detailView.Reveal() {
		t.Fatal("a namespace switch must keep the profile's redaction")
	}
	m.Adopt(&Connection{Reader: m.deps.reader, Namespace: "demo"})
	if !m.detailView.Reveal() {
		t.Fatal("a profile without redactValues must show the values again")
	}
}
