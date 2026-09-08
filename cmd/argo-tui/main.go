// Command argo-tui is a read-only TUI for Argo Workflows.
//
// F1 scope: a compiling baseline with an explicit --demo mode backed by the
// in-memory fake Reader — no network fallback, no writes (plan §8 F1 slice
// 6). Real connections (config + REST adapter) arrive with A1/I1; the stub
// UI is not a finished alpha.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/testkit"
)

// version identifies the F1 baseline build; I1 replaces it with real
// version stamping (--version is part of the frozen CLI set, plan §3).
const version = "0.0.0-f1"

// mainVersion exposes the build version to the smoke test (plan §8 F1
// slice 1: compile/smoke test) without exporting a mutable API surface.
var mainVersion = version

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("argo-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	demo := fs.Bool("demo", false, "run against the built-in synthetic demo dataset (no network, no writes)")
	versionFlag := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		// flag already printed usage/error to stderr
		return 2
	}
	if *versionFlag {
		fmt.Println("argo-tui " + mainVersion)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "argo-tui: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	if !*demo {
		// Plan §3: validation and real connection setup happen before the
		// alternate screen starts. F1 has no REST adapter yet; requiring
		// --demo keeps the demo path explicit and prevents any accidental
		// network fallback (plan: "no network fallback").
		fmt.Fprintln(os.Stderr, "argo-tui: real connections are not wired yet (A1/I1); use --demo for the synthetic fake backend")
		return 1
	}

	// --demo: explicit fake Reader only. The fake never performs I/O and
	// never constructs HTTP clients (ADR 0001); there is no network
	// fallback and no write path anywhere in F1.
	clock := testkit.NewFakeClock(time.Now().UTC())
	fake := testkit.DemoReader(clock)
	root := app.NewRoot(fake, clock, "demo", 5*time.Second) // 5s = config.DefaultRefreshInterval; real config wiring is I1 scope
	p := tea.NewProgram(root)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "argo-tui: %v\n", err)
		return 1
	}
	return 0
}
