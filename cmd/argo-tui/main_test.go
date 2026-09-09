package main

import (
	"strings"
	"testing"
)

// TestSmokeVersionString pins the local beta build identity so the smoke
// slice has a real executable-shaped assertion.
func TestSmokeVersionString(t *testing.T) {
	if mainVersion == "" {
		t.Fatal("mainVersion is empty")
	}
	if !strings.Contains(mainVersion, "beta") {
		t.Errorf("mainVersion = %q, want the beta identifier", mainVersion)
	}
}

// TestSmokeRunRejectsNonDemo pins the slice-6 contract at the executable
// level: without an explicit flag, the entrypoint never falls back to
// network construction; it refuses to start the TUI (exit 1).
func TestSmokeRunRejectsNonDemo(t *testing.T) {
	if code := run([]string{}); code != 1 {
		t.Errorf("run() exit = %d, want 1 (no implicit connection without --demo)", code)
	}
}

// TestSmokeRunVersionFlag pins the --version fast path (exit 0, no TUI).
func TestSmokeRunVersionFlag(t *testing.T) {
	if code := run([]string{"--version"}); code != 0 {
		t.Errorf("run(--version) exit = %d, want 0", code)
	}
}

// TestSmokeRunRejectsPositionalArgs guards the frozen CLI surface: unknown
// positional arguments are rejected with exit 2 instead of being ignored.
func TestSmokeRunRejectsPositionalArgs(t *testing.T) {
	if code := run([]string{"stray-argument"}); code != 2 {
		t.Errorf("run(stray) exit = %d, want 2", code)
	}
}
