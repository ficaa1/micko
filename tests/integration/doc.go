//go:build integration

// Package integration runs the root model over the production Argo client
// against an in-process Argo Server, and drives the built binary in a
// pseudo-terminal. Run with: go test -tags=integration ./tests/integration/...
package integration
