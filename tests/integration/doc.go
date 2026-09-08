//go:build integration

// Package integration hosts E1's independent test harnesses
// (docs/test-environment.md §1, §5):
//
//   - ET-2: an in-process Argo Workflows v4.1.2 wire-contract fixture
//     server (fakeserver.go) plus a test-only wire client (wire.go) that
//     exercises real HTTP transport, real JSON decoding and the real
//     internal/app root model — never fake DTOs alone.
//   - ET-3: a PTY process harness (pty.go, linux) driving the real
//     argo-tui binary: spawn → scripted keys → captured output → exit and
//     terminal-state assertions.
//
// Build with: go test -tags=integration ./tests/integration/...
//
// Scope note (honest labeling, plan §8 E1 gate): the production REST
// adapter (internal/argo, A1) is a parallel worker. Until I1 merges it,
// these tests exercise the pinned wire contract and the real root model;
// adapter-level conformance against the same fixture server is executed
// by I1/Q1. See docs/testing.md for the current evidence state.
//
// Fixture policy: every payload in this package is SYNTHETIC and generated
// (plan §8 F1 slice 3) — nothing is a captured production response and
// nothing may be presented as one.
package integration
