package main

import (
	"strings"
	"testing"
	"time"
)

// TestSmokeVersionString pins the build identity so the smoke slice has a
// real executable-shaped assertion.
func TestSmokeVersionString(t *testing.T) {
	if mainVersion == "" {
		t.Fatal("mainVersion is empty")
	}
	if strings.Count(mainVersion, ".") < 2 {
		t.Errorf("mainVersion = %q, want a major.minor.patch version", mainVersion)
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

// A frozen clock belongs to the demo alone, whose dataset is built from the
// same clock and whose sample ages have to stay put. A real run must read
// the wall clock, or every age on screen freezes at the moment of start.
func TestOnlyTheDemoGetsAFrozenClock(t *testing.T) {
	real, demoClock := newClock(false)
	if demoClock != nil {
		t.Fatal("a real run was given the demo's frozen clock")
	}
	first := real.Now()
	deadline := time.Now().Add(time.Second)
	for real.Now().Equal(first) {
		if time.Now().After(deadline) {
			t.Fatal("the clock wired into a real run does not advance")
		}
	}

	demo, frozen := newClock(true)
	if frozen == nil {
		t.Fatal("the demo needs its own clock to generate the dataset")
	}
	if !demo.Now().Equal(frozen.Now()) {
		t.Fatal("the demo dataset and the views read different clocks")
	}
	if at := demo.Now(); !demo.Now().Equal(at) {
		t.Fatal("the demo clock moved on its own")
	}
}
