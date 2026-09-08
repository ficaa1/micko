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
	"strings"
	"time"

	"charm.land/bubbletea/v2"

	"argo-tui/internal/app"
	"argo-tui/internal/argo"
	"argo-tui/internal/config"
	"argo-tui/internal/core"
	"argo-tui/internal/testkit"
)

const version = "0.1.0-alpha"

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
	configPath := fs.String("config", "", "configuration file path")
	profile := fs.String("profile", "", "configured profile")
	server := fs.String("server", "", "Argo Server endpoint")
	namespace := fs.String("namespace", "", "workflow namespace")
	tokenFile := fs.String("token-file", "", "token file")
	caFile := fs.String("ca-file", "", "custom CA bundle")
	refresh := fs.Duration("refresh-interval", 0, "poll interval")
	insecure := fs.Bool("insecure-skip-tls-verify", false, "disable TLS verification (unsafe)")
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

	clock := testkit.NewFakeClock(time.Now().UTC())
	var reader core.Reader
	activeNS := ""
	interval := config.DefaultRefreshInterval
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
		cfg, err := config.Load(data, config.Options{ConfigPath: path, Profile: *profile, Server: *server, Namespace: *namespace, TokenFile: *tokenFile, CAFile: *caFile, RefreshInterval: *refresh, InsecureSkipTLSVerify: *insecure})
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
		reader, err = argo.NewClient(argo.Options{Server: cfg.Server, TokenFn: tokenFn, TokenSource: "configured credential source", CAFile: cfg.CAFile, InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify})
		if err != nil {
			fmt.Fprintln(os.Stderr, "argo-tui:", err)
			return 1
		}
		activeNS, interval = cfg.Namespace, cfg.RefreshInterval
	}
	root := app.NewRoot(reader, clock, activeNS, interval)
	p := tea.NewProgram(root)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "argo-tui: %v\n", err)
		return 1
	}
	return 0
}
