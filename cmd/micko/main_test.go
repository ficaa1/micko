package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/ui/actions"
)

// Every command line that ends before the TUI starts exits with its code:
// 0 for a fast path, 2 for a usage error, 1 for a start that fails.
func TestRunExitCodes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oneProfile := write("one.yaml", "profiles:\n  dev:\n    server: https://argo.example.com\n")
	badSkin := write("skin.yaml", "profiles:\n  dev:\n    server: https://argo.example.com\n    skin: neon\n")
	missing := filepath.Join(dir, "none.yaml")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"version", []string{"--version"}, 0},
		{"allow-actions still parses", []string{"--allow-actions", "--version"}, 0},
		{"read-only", []string{"--read-only", "--version"}, 0},
		{"positional argument", []string{"stray-argument"}, 2},
		{"unknown flag", []string{"--nope"}, 2},
		{"unknown skin flag in the demo", []string{"--demo", "--skin", "neon"}, 2},
		{"unknown skin flag", []string{"--skin", "neon", "--config", missing}, 2},
		{"unknown mascot", []string{"--mascot=roof", "--version"}, 2},
		{"unknown skin in the file", []string{"--config", badSkin}, 1},
		{"profile not in the file", []string{"--config", oneProfile, "--profile", "missing"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := run(c.args); got != c.want {
				t.Errorf("run(%q) = %d, want %d", c.args, got, c.want)
			}
		})
	}
}

// Only the two flags that name a destination connect; the rest change how a
// profile is used, not which one, so they leave the choice to the picker.
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
		t.Run(c.name, func(t *testing.T) {
			if got := directConnect(c.profile, c.server); got != c.want {
				t.Errorf("directConnect(%q, %q) = %v, want %v", c.profile, c.server, got, c.want)
			}
		})
	}
}

// The demo reads no config file, so its skin is the flag or the default.
func TestTheDemoSkinIsTheFlag(t *testing.T) {
	for flag, want := range map[string]string{"": "default", "nord": "nord"} {
		if got := demoSkin(flag); got != want {
			t.Errorf("demoSkin(%q) = %q, want %q", flag, got, want)
		}
	}
}

// A frozen clock belongs to the demo alone, whose dataset is built from the
// same clock. A real run reads the wall clock, or every age on screen freezes.
func TestOnlyTheDemoGetsAFrozenClock(t *testing.T) {
	wall, demoClock := newClock(false)
	if demoClock != nil {
		t.Fatal("a real run was given the demo's frozen clock")
	}
	first := wall.Now()
	deadline := time.Now().Add(time.Second)
	for wall.Now().Equal(first) {
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

// Actions are on unless --read-only turns them off; the demo never writes.
func TestActionsAreOnUnlessReadOnly(t *testing.T) {
	cases := []struct {
		name           string
		readOnly, demo bool
		want           actions.Options
	}{
		{"default", false, false, actions.Options{AllowActions: true}},
		{"read-only", true, false, actions.Options{ReadOnly: true}},
		{"demo", false, true, actions.Options{ReadOnly: true, Demo: true}},
		{"read-only demo", true, true, actions.Options{ReadOnly: true, Demo: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := actionOptions(c.readOnly, c.demo); got != c.want {
				t.Errorf("actionOptions(%v, %v) = %+v, want %+v", c.readOnly, c.demo, got, c.want)
			}
		})
	}
}
