//go:build integration

// Package integration tests the fixture-server wire contract, root-model
// journeys and real-binary terminal lifecycle. The HTTP harness uses a
// test-only WireClient; production adapter tests live in internal/argo.
// Run with: go test -tags=integration ./tests/integration/...
// See docs/development.md for prerequisites and coverage limits.
package integration
