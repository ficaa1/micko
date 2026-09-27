package main

import (
	"os"
	"path/filepath"
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

// A named profile is an instruction to connect, so the entrypoint connects
// before it starts the TUI and reports a failure on stderr with exit 1. This
// also pins that a profile that is not in the file is refused by name rather
// than opening a session against nothing.
func TestRunRejectsAnUnknownProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles:\n  dev:\n    server: https://argo.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--config", path, "--profile", "missing"}); code != 1 {
		t.Errorf("run(--profile missing) exit = %d, want 1", code)
	}
}

// directConnect decides between connecting and opening the picker. Only the
// two flags that name a destination connect; the rest change how a profile is
// used, not which one, so they must leave the choice to the reader.
func TestOnlyADestinationFlagSkipsThePicker(t *testing.T) {
	cases := []struct {
		name            string
		profile, server string
		want            bool
	}{
		{name: "no flags", want: false},
		{name: "profile", profile: "dev", want: true},
		{name: "server", server: "https://argo.example.com", want: true},
		{name: "both", profile: "dev", server: "https://argo.example.com", want: true},
	}
	for _, c := range cases {
		if got := directConnect(c.profile, c.server); got != c.want {
			t.Errorf("%s: directConnect(%q, %q) = %v, want %v", c.name, c.profile, c.server, got, c.want)
		}
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

// A misspelled --skin is a usage error, reported before anything starts,
// in the demo as well as in a real run.
func TestRunRejectsAnUnknownSkinFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--demo", "--skin", "neon"},
		{"--skin", "neon", "--config", filepath.Join(t.TempDir(), "none.yaml")},
	} {
		if code := run(args); code != 2 {
			t.Errorf("run(%v) exit = %d, want 2", args, code)
		}
	}
}

// An unknown skin in the config file fails the start with exit 1, like any
// other invalid config, even when it is on a profile nobody chose.
func TestRunRejectsAnUnknownSkinInTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "profiles:\n  dev:\n    server: https://argo.example.com\n    skin: neon\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--config", path}); code != 1 {
		t.Errorf("run(config with skin neon) exit = %d, want 1", code)
	}
}

// The demo reads no config file, so its skin is the flag or the default.
func TestTheDemoSkinIsTheFlag(t *testing.T) {
	if got := demoSkin(""); got != "default" {
		t.Errorf("demoSkin(\"\") = %q, want default", got)
	}
	if got := demoSkin("nord"); got != "nord" {
		t.Errorf("demoSkin(nord) = %q", got)
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
