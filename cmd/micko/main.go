// Command micko is a keyboard-first TUI for Argo Workflows.
//
// Workflow actions are on unless --read-only turns them off, and each one
// is confirmation-gated; --demo remains offline and never performs writes.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/app"
	"github.com/ficaa1/micko/internal/buildinfo"
	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/journal"
	"github.com/ficaa1/micko/internal/session"
	"github.com/ficaa1/micko/internal/setup"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runIO(args, os.Stdin, os.Stdout, os.Stderr)
}

func runIO(args []string, input io.Reader, output, errorOutput io.Writer) int {
	fs := flag.NewFlagSet("micko", flag.ContinueOnError)
	fs.SetOutput(errorOutput)
	setupFlag := fs.Bool("setup", false, "create a named profile interactively (no connection; never enter token values)")
	check := fs.Bool("check", false, "check workflow read access and exit (requires --profile or --server)")
	checkTimeout := fs.Duration("check-timeout", 15*time.Second, "total connection check timeout")
	authMode := fs.String("auth-mode", "", "authentication: server (no bearer token) or token (token source required)")
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
	readOnly := fs.Bool("read-only", false, "turn off workflow actions (resume, suspend, retry, resubmit, stop, terminate, delete)")
	// Accepted so scripts that still pass --allow-actions keep working.
	_ = fs.Bool("allow-actions", false, "no effect: actions are on unless --read-only")
	debug := fs.Bool("debug", false, "enable sanitized lifecycle diagnostics")
	skin := fs.String("skin", "", "colour skin, overriding the config file: "+strings.Join(shared.SkinNames(), ", "))
	redactValues := fs.Bool("redact-values", false, "hide parameter and output values until v reveals them")
	var mascot mascotFlag
	fs.Var(&mascot, "mascot", "perch Mićko on the pane, or --mascot=floor to sit him in its bottom corner (terminals of 80x40 and larger)")
	if err := fs.Parse(args); err != nil {
		// The flag package already printed the usage error.
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *versionFlag {
		fmt.Fprintf(output, "micko %s (%s)\n", buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(errorOutput, "micko: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *setupFlag {
		invalid := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "setup" && f.Name != "config" {
				invalid = true
			}
		})
		if invalid {
			fmt.Fprintln(errorOutput, "micko: --setup accepts only --config")
			return 2
		}
		path := *configPath
		if path == "" {
			var err error
			path, err = config.DefaultConfigPath()
			if err != nil {
				fmt.Fprintln(errorOutput, "micko: cannot resolve config path")
				return 1
			}
		}
		if err := setup.Run(input, output, path); err != nil {
			fmt.Fprintln(errorOutput, "micko:", err)
			return 1
		}
		return 0
	}
	if *check && (*demo || !directConnect(*profile, *server) || *checkTimeout <= 0) {
		fmt.Fprintln(errorOutput, "micko: --check requires --profile or --server, a positive --check-timeout, and no --demo")
		return 2
	}

	// A misspelled skin is a usage error like a misspelled flag: it is
	// reported before anything connects, with every name that would work.
	if *skin != "" {
		if err := shared.CheckSkin(*skin); err != nil {
			fmt.Fprintln(errorOutput, "micko: --skin:", err)
			return 2
		}
	}

	clock, demoClock := newClock(*demo)
	opts := actionOptions(*readOnly, *demo)

	var root *app.Root
	if *demo {
		opts.Server, opts.Profile = "synthetic demo", "demo"
		root = app.NewRoot(testkit.DemoReader(demoClock), clock, "demo", config.DefaultRefreshInterval, opts)
		// The demo reads no config file, so the flag is its only skin.
		_, _ = root.ApplySkin(demoSkin(*skin))
		root.SetRedactValues(*redactValues)
		root.SetMascot(mascot.spot)
		root.SetNotify(config.DefaultNotify())
	} else {
		connector, err := session.NewConnector(session.Options{
			ConfigPath:            *configPath,
			Server:                *server,
			Namespace:             *namespace,
			AuthMode:              *authMode,
			TokenFile:             *tokenFile,
			CAFile:                *caFile,
			RefreshInterval:       *refresh,
			InsecureSkipTLSVerify: *insecure,
			Debug:                 *debug,
			Skin:                  *skin,
			Skins:                 shared.SkinNames(),
			RedactValues:          *redactValues,
			Mascot:                mascot.spot,
			Diagnostics:           errorOutput,
		})
		if err != nil {
			if *check {
				fmt.Fprintln(errorOutput, "check config: Could not read configuration; check YAML, permissions and skin names.")
			} else {
				fmt.Fprintln(errorOutput, "micko:", err)
			}
			return 1
		}
		// The connector owns the port-forward of whatever connection is live.
		// The model closes the one it replaces on a switch; this closes the
		// last one on every exit path, including a failed Run.
		defer connector.Close()
		if *check {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if *insecure {
				fmt.Fprintln(errorOutput, "warning: TLS certificate verification is disabled (--insecure-skip-tls-verify).")
			}
			result := connector.Check(ctx, *profile, *checkTimeout)
			fmt.Fprintf(output, "check %s: %s\n", result.Stage, result.Message)
			if !result.OK {
				return 1
			}
			return 0
		}

		root = app.NewRoot(nil, clock, "", config.DefaultRefreshInterval, opts)
		// The journal records write attempts, so only a session that can
		// write gets one. The demo, which never writes, has none at all.
		if !*readOnly && connector.JournalEnabled() {
			root.SetJournal(journal.Open())
		}
		root.SetConnector(connector)
		root.SetProfiles(connector.ProfileList())
		root.SetMascot(connector.Mascot())
		root.SetNotify(connector.Notify())
		// Before a profile is chosen the picker uses the flag or the file's
		// top-level skin. Init starts the auto skin's background query.
		if _, err := root.ApplySkin(connector.Skin()); err != nil {
			fmt.Fprintln(errorOutput, "micko:", err)
			return 1
		}

		// Naming a profile or a server is an instruction to connect to it, so
		// that path connects here and reports a failure on stderr with a
		// non-zero exit. Without either, the session starts in the picker: a
		// config file with several clusters has no right answer to guess.
		if directConnect(*profile, *server) {
			conn, err := connector.Connect(context.Background(), *profile)
			if err != nil {
				fmt.Fprintln(errorOutput, "micko:", err)
				return 1
			}
			root.Adopt(conn)
		}
	}
	root.SetVersion(buildinfo.Version + " @ " + buildinfo.Commit)

	if _, err := tea.NewProgram(root).Run(); err != nil {
		fmt.Fprintf(errorOutput, "micko: %v\n", err)
		return 1
	}
	return 0
}

// directConnect reports whether the command line names what to connect to.
// --namespace and the TLS or token flags alone still open the picker.
func directConnect(profile, server string) bool {
	return profile != "" || server != ""
}

// demoSkin is the skin the demo draws in: the flag, else the default.
func demoSkin(flag string) string {
	if flag != "" {
		return flag
	}
	return config.DefaultSkin
}

// newClock returns the wall clock for live sessions and the dataset clock for
// the demo. The second result is nil for live sessions.
func newClock(demo bool) (app.Clock, *testkit.FakeClock) {
	if demo {
		c := testkit.NewFakeClock(time.Now().UTC())
		return c, c
	}
	return app.SystemClock{}, nil
}

// mascotFlag is --mascot. Bare, it perches Mićko; --mascot=floor sits him
// on the floor of the pane, and --mascot=false leaves him to the config
// file.
type mascotFlag struct{ spot config.Mascot }

func (f *mascotFlag) String() string { return string(f.spot) }

func (f *mascotFlag) Set(s string) (err error) {
	f.spot, err = config.ParseMascot(s)
	return err
}

// IsBoolFlag lets --mascot stand alone, as a bool flag does.
func (f *mascotFlag) IsBoolFlag() bool { return true }

// actionOptions turns actions off for --read-only and for the demo.
func actionOptions(readOnly, demo bool) actions.Options {
	return actions.Options{
		AllowActions: !readOnly && !demo,
		ReadOnly:     readOnly || demo,
		Demo:         demo,
	}
}
