// Command argo-tui is a keyboard-first TUI for Argo Workflows.
//
// The local beta keeps mutations explicitly opt-in and confirmation-gated;
// --demo remains offline and never performs writes.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/argo"
	"argo-tui/internal/config"
	"argo-tui/internal/core"
	"argo-tui/internal/diagnostics"
	"argo-tui/internal/portforward"
	"argo-tui/internal/testkit"
	"argo-tui/internal/ui/actions"
)

const version = "0.2.0-beta.1"

// mainVersion exposes the build version to the smoke test without exporting
// a mutable API surface.
var mainVersion = version
var mainCommit = "unknown"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("argo-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	demo := fs.Bool("demo", false, "run against the built-in synthetic demo dataset (no network, no writes)")
	versionFlag := fs.Bool("version", false, "print version and exit")
	configPath := fs.String("config", "", "configuration file path")
	profile := fs.String("profile", "", "configured profile")
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

	clock := testkit.NewFakeClock(time.Now().UTC())
	var reader core.Reader
	activeNS := ""
	interval := config.DefaultRefreshInterval
	activeServer, activeProfile := "synthetic demo", "demo"
	if *demo {
		reader = testkit.DemoReader(clock)
		activeNS = "demo"
	} else {
		path := *configPath
		if path == "" {
			path, _ = config.DefaultConfigPath()
		}
		var data []byte
		if path != "" {
			var err error
			data, err = os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "argo-tui: read config: %v\n", err)
				return 1
			}
		}
		cfg, err := config.Load(data, config.Options{ConfigPath: path, Profile: *profile, Server: *server, Namespace: *namespace, TokenFile: *tokenFile, CAFile: *caFile, RefreshInterval: *refresh, InsecureSkipTLSVerify: *insecure, Debug: *debug})
		if err != nil {
			fmt.Fprintln(os.Stderr, "argo-tui:", err)
			return 1
		}
		tokenFn := func() (string, error) {
			if cfg.TokenFile != "" {
				b, err := os.ReadFile(cfg.TokenFile)
				return strings.TrimSuffix(string(b), "\n"), err
			}
			return os.Getenv(cfg.TokenEnv), nil
		}
		serverURL := cfg.Server
		var forwarder *portforward.Manager
		if cfg.Target.Service != "" {
			localPort := ""
			forwarder, err = portforward.New(portforward.Target{Context: cfg.Target.Context, Namespace: cfg.Target.Namespace, Service: cfg.Target.Service, RemotePort: strconv.Itoa(cfg.Target.RemotePort), LocalPort: localPort})
			if err != nil {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward:", err)
				return 1
			}
			forwarder.Start(context.Background())
			defer forwarder.Close()
			// Do not construct a client against a guessed/reused port. Readiness
			// and the actual owned endpoint are prerequisites for any request.
			readyCtx, cancelReady := context.WithTimeout(context.Background(), 15*time.Second)
			err = forwarder.Ready(readyCtx)
			cancelReady()
			if err != nil {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward readiness:", err)
				return 1
			}
			serverURL = forwarder.Endpoint()
			if serverURL == "" {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward announced readiness without an endpoint")
				return 1
			}
			sink := diagnostics.New(os.Stderr)
			go func() {
				for e := range forwarder.Events() {
					if *debug {
						sink.Emit(diagnostics.StageForward, string(e.State), e.Attempt-1, e.State == portforward.StateLost, 0, "")
					}
				}
			}()
		}
		reader, err = argo.NewClient(argo.Options{Server: serverURL, TokenFn: tokenFn, TokenSource: "configured credential source", CAFile: cfg.CAFile, InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify})
		if err != nil {
			fmt.Fprintln(os.Stderr, "argo-tui:", err)
			return 1
		}
		activeNS, interval = cfg.Namespace, cfg.RefreshInterval
		activeServer, activeProfile = cfg.Server, cfg.ProfileName
	}
	root := app.NewRootWithOptions(reader, clock, activeNS, interval, actions.Options{
		AllowActions: *allowActions && !*demo,
		ReadOnly:     !*allowActions || *demo,
		Demo:         *demo,
		Server:       activeServer,
		Profile:      activeProfile,
	})
	root.SetVersion(mainVersion + " @ " + mainCommit)
	p := tea.NewProgram(root)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "argo-tui: %v\n", err)
		return 1
	}
	return 0
}
