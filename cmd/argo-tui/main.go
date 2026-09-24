// Command argo-tui is a keyboard-first TUI for Argo Workflows.
//
// Mutations stay explicitly opt-in and confirmation-gated behind
// --allow-actions; --demo remains offline and never performs writes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/argo-tui/internal/app"
	"github.com/ficaa1/argo-tui/internal/buildinfo"
	"github.com/ficaa1/argo-tui/internal/config"
	"github.com/ficaa1/argo-tui/internal/journal"
	"github.com/ficaa1/argo-tui/internal/session"
	"github.com/ficaa1/argo-tui/internal/testkit"
	"github.com/ficaa1/argo-tui/internal/ui/actions"
)

// mainVersion and mainCommit expose the build identity to the smoke test
// without exporting a mutable API surface.
var mainVersion = buildinfo.Version
var mainCommit = buildinfo.Commit

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("argo-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	demo := fs.Bool("demo", false, "run against the built-in synthetic demo dataset (no network, no writes)")
	versionFlag := fs.Bool("version", false, "print version and exit")
	configPath := fs.String("config", "", "configuration file path")
	profile := fs.String("profile", "", "connect to this profile instead of opening the profile picker")
	server := fs.String("server", "", "Argo Server endpoint")
	namespace := fs.String("namespace", "", "workflow namespace")
	tokenFile := fs.String("token-file", "", "token file")
	caFile := fs.String("ca-file", "", "custom CA bundle")
	refresh := fs.Duration("refresh-interval", 0, "poll interval")
	insecure := fs.Bool("insecure-skip-tls-verify", false, "disable TLS verification (unsafe)")
	allowActions := fs.Bool("allow-actions", false, "enable explicitly confirmed workflow actions")
	debug := fs.Bool("debug", false, "enable sanitized lifecycle diagnostics")
	if err := fs.Parse(args); err != nil {
		// flag already printed usage/error to stderr
		return 2
	}
	if *versionFlag {
		fmt.Printf("argo-tui %s (%s)\n", mainVersion, mainCommit)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "argo-tui: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	clock, demoClock := newClock(*demo)
	opts := actions.Options{
		AllowActions: *allowActions && !*demo,
		ReadOnly:     !*allowActions || *demo,
		Demo:         *demo,
	}

	var root *app.Root
	if *demo {
		opts.Server, opts.Profile = "synthetic demo", "demo"
		root = app.NewRootWithOptions(testkit.DemoReader(demoClock), clock, "demo", config.DefaultRefreshInterval, opts)
	} else {
		connector, err := session.NewConnector(session.Options{
			ConfigPath:            *configPath,
			Server:                *server,
			Namespace:             *namespace,
			TokenFile:             *tokenFile,
			CAFile:                *caFile,
			RefreshInterval:       *refresh,
			InsecureSkipTLSVerify: *insecure,
			Debug:                 *debug,
			Diagnostics:           os.Stderr,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "argo-tui:", err)
			return 1
		}
		// The connector owns the port-forward of whatever connection is live.
		// The model closes the one it replaces on a switch; this closes the
		// last one on every exit path, including a failed Run.
		defer connector.Close()

		root = app.NewRootWithOptions(nil, clock, "", config.DefaultRefreshInterval, opts)
		// The journal records write attempts, so only a session that can
		// write gets one. The demo, which never writes, has none at all.
		if *allowActions && connector.JournalEnabled() {
			root.SetJournal(journal.Open())
		}
		root.SetConnector(connector)
		root.SetProfiles(connector.ProfileList())

		// Naming a profile or a server is an instruction to connect to it, so
		// that path connects here and reports a failure on stderr with a
		// non-zero exit. Without either, the session starts in the picker: a
		// config file with several clusters has no right answer to guess.
		if directConnect(*profile, *server) {
			conn, err := connector.Connect(context.Background(), *profile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "argo-tui:", err)
				return 1
			}
			root.Adopt(conn)
		}
	}
	root.SetVersion(mainVersion + " @ " + mainCommit)

	if _, err := tea.NewProgram(root).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "argo-tui: %v\n", err)
		return 1
	}
	return 0
}

// directConnect reports whether the command line already names what to connect
// to. --namespace and the TLS or token flags alone do not: they change how a
// profile is used, not which one, so they still open the picker.
func directConnect(profile, server string) bool {
	return profile != "" || server != ""
}

// newClock picks the clock for this run. Every relative time on screen is
// measured against it: a workflow's age, and how long the snapshot has been
// stale. A real run therefore reads the wall clock, or all of them freeze at
// the moment the program started.
//
// The demo is the one exception. Its dataset is generated from the same
// clock, so a frozen clock is what keeps its sample ages put. The second
// return value is that clock, and is nil for a real run.
func newClock(demo bool) (app.Clock, *testkit.FakeClock) {
	if demo {
		c := testkit.NewFakeClock(time.Now().UTC())
		return c, c
	}
	return app.SystemClock{}, nil
}
