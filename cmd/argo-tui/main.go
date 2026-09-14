// Command argo-tui is a keyboard-first TUI for Argo Workflows.
//
// The local beta keeps mutations explicitly opt-in and confirmation-gated;
// --demo remains offline and never performs writes.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/argo"
	"argo-tui/internal/buildinfo"
	"argo-tui/internal/config"
	"argo-tui/internal/core"
	"argo-tui/internal/diagnostics"
	"argo-tui/internal/portforward"
	"argo-tui/internal/testkit"
	"argo-tui/internal/ui/actions"
)

// mainVersion and mainCommit expose the build identity to the smoke test
// without exporting a mutable API surface.
var mainVersion = buildinfo.Version
var mainCommit = buildinfo.Commit

// forwardEndpoint combines the announced loopback address of the owned
// port-forward with the scheme and path prefix of the configured server. The
// forward moves the transport to a new host and port; it must not silently
// downgrade a configured https endpoint to plain http, and it must not
// discard a configured base path.
func forwardEndpoint(configured, announced string) string {
	if announced == "" {
		return ""
	}
	a, err := url.Parse(announced)
	if err != nil || a.Host == "" {
		return ""
	}
	c, err := url.Parse(configured)
	if err != nil || c.Scheme == "" {
		return announced
	}
	out := url.URL{Scheme: c.Scheme, Host: a.Host, Path: strings.TrimSuffix(c.Path, "/")}
	return out.String()
}

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

	var reader core.Reader
	var connStates chan app.ConnectionStateMsg
	activeNS := ""
	interval := config.DefaultRefreshInterval
	activeServer, activeProfile := "synthetic demo", "demo"
	activeWebURL := ""
	activePipe := ""
	var activeNamespaces []string
	clock, demoClock := newClock(*demo)
	if *demo {
		reader = testkit.DemoReader(demoClock)
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
		var stateCh chan app.ConnectionStateMsg
		if cfg.Target.Service != "" {
			// LocalPort stays empty so kubectl binds an ephemeral loopback
			// port. Nothing is ever sent to a guessed or reused port: the
			// endpoint comes only from the readiness line of the process we
			// own, and it is re-read per request because every recovery
			// binds a new port.
			forwarder, err = portforward.New(portforward.Target{Context: cfg.Target.Context, Namespace: cfg.Target.Namespace, Service: cfg.Target.Service, RemotePort: strconv.Itoa(cfg.Target.RemotePort), LocalPort: ""})
			if err != nil {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward:", err)
				return 1
			}
			forwarder.Start(context.Background())
			defer forwarder.Close()

			// Drain lifecycle events from the moment forwarding starts, before
			// waiting for readiness: a stalled consumer must never be able to
			// hold back the recovery loop. stateCh buffers the connection
			// transitions until the Tea program exists to receive them.
			stateCh = make(chan app.ConnectionStateMsg, 32)
			sink := diagnostics.New(os.Stderr)
			go func() {
				defer close(stateCh)
				for e := range forwarder.Events() {
					if *debug {
						sink.Emit(diagnostics.StageForward, string(e.State), e.Attempt-1, e.State == portforward.StateLost, 0, "")
					}
					msg := app.ConnectionStateMsg{Ready: e.State == portforward.StateReady}
					if msg.Ready {
						msg.Target = forwardEndpoint(cfg.Server, forwarder.Endpoint())
					}
					switch e.State {
					case portforward.StateReady, portforward.StateLost, portforward.StateFailed, portforward.StateStopped:
					default:
						continue
					}
					select {
					case stateCh <- msg:
					default: // never block forwarding on the UI
					}
				}
			}()

			readyCtx, cancelReady := context.WithTimeout(context.Background(), 30*time.Second)
			err = forwarder.Ready(readyCtx)
			cancelReady()
			if err != nil {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward readiness:", err)
				return 1
			}
			serverURL = forwardEndpoint(cfg.Server, forwarder.Endpoint())
			if serverURL == "" {
				fmt.Fprintln(os.Stderr, "argo-tui: port-forward announced readiness without an endpoint")
				return 1
			}
		}
		// resolveServer keeps the transport pointed at the currently owned
		// local port. The configured scheme, path prefix and TLS material are
		// fixed at construction and are not affected by recovery.
		var resolveServer func() string
		if forwarder != nil {
			resolveServer = func() string { return forwardEndpoint(cfg.Server, forwarder.Endpoint()) }
		}
		reader, err = argo.NewClient(argo.Options{Server: serverURL, TokenFn: tokenFn, TokenSource: "configured credential source", CAFile: cfg.CAFile, InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify, ResolveServer: resolveServer, UserAgent: buildinfo.UserAgent()})
		if err != nil {
			fmt.Fprintln(os.Stderr, "argo-tui:", err)
			return 1
		}
		activeNS, interval = cfg.Namespace, cfg.RefreshInterval
		activeServer, activeProfile = cfg.Server, cfg.ProfileName
		activeWebURL = cfg.WebURL
		activePipe = cfg.PipeCommand
		activeNamespaces = cfg.Namespaces
		connStates = stateCh
	}
	root := app.NewRootWithOptions(reader, clock, activeNS, interval, actions.Options{
		AllowActions: *allowActions && !*demo,
		ReadOnly:     !*allowActions || *demo,
		Demo:         *demo,
		Server:       activeServer,
		Profile:      activeProfile,
	})
	root.SetVersion(mainVersion + " @ " + mainCommit)
	root.SetWebURL(activeWebURL)
	root.SetPipeCommand(activePipe)
	root.SetNamespaceSeed(activeNamespaces)
	p := tea.NewProgram(root)
	if connStates != nil {
		// Bridge transport lifecycle into the update loop. Losing the forward
		// keeps the last-good data on screen but disables actions until a
		// fresh snapshot is accepted; the model owns that policy.
		go func() {
			for msg := range connStates {
				p.Send(msg)
			}
		}()
	}
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "argo-tui: %v\n", err)
		return 1
	}
	return 0
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
